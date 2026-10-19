package packagemanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/semver"
)

const cargoProjectRegistry = "registry+https://github.com/rust-lang/crates.io-index"

var (
	cargoProjectName    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	cargoProjectVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	cargoProjectChange  = regexp.MustCompile(`^(Adding|Removing|Updating|Downgrading) ([A-Za-z0-9_][A-Za-z0-9_-]*) v(\S+)(?: -> v(\S+))?(?: \((available: v[^()]+|requires Rust [^()]+)\))?$`)
	cargoProjectLocking = regexp.MustCompile(`^Locking [0-9]+ packages? to (?:latest|highest)(?: Rust [0-9.]+)? compatible versions?$`)
)

func cargoProjectState(scopePath string) (string, error) {
	root, err := filepath.Abs(scopePath)
	if err != nil || scopePath == "" {
		return "", fmt.Errorf("cargo project requires an absolute root")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "root:%q\n", root)
	add := func(path string, required bool) error {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && !required {
			_, _ = fmt.Fprintf(hash, "absent:%q\n", path)
			return nil
		}
		if err != nil {
			return err
		}
		if required && !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular, non-symlink file", path)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if filepath.Base(path) != "Cargo.lock" {
			var document map[string]any
			if err := toml.Unmarshal(data, &document); err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			if filepath.Base(path) == "Cargo.toml" {
				if _, exists := document["workspace"]; exists {
					return fmt.Errorf("cargo workspace projects are not supported")
				}
				if err := cargoProjectStandalone(document); err != nil {
					return err
				}
			} else {
				for _, key := range []string{"include", "paths"} {
					if _, exists := document[key]; exists {
						return fmt.Errorf("cargo configuration %s is not supported", key)
					}
				}
				if err := cargoProjectStandalone(document); err != nil {
					return err
				}
			}
		}
		_, _ = fmt.Fprintf(hash, "file:%q:%q:%v:%x\n", path, canonical, info.Mode(), data)
		return nil
	}
	for _, name := range []string{"Cargo.toml", "Cargo.lock"} {
		if err := add(filepath.Join(root, name), true); err != nil {
			return "", err
		}
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		if dir != root {
			if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
				return "", fmt.Errorf("cargo ancestor manifests are not supported")
			} else if !os.IsNotExist(err) {
				return "", err
			}
		}
		for _, name := range []string{"config", "config.toml"} {
			if err := add(filepath.Join(dir, ".cargo", name), false); err != nil {
				return "", err
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	home := os.Getenv("CARGO_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".cargo")
	} else if !filepath.IsAbs(home) {
		home = filepath.Join(root, home)
	}
	for _, name := range []string{"config", "config.toml"} {
		if err := add(filepath.Join(home, name), false); err != nil {
			return "", err
		}
	}
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CARGO_") || strings.HasPrefix(name, "RUST") {
			environment = append(environment, entry)
		}
	}
	sort.Strings(environment)
	for _, entry := range environment {
		_, _ = fmt.Fprintf(hash, "env:%q\n", entry)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func cargoProjectStandalone(document map[string]any) error {
	// ponytail: standalone projects only; bind workspace and local dependency manifests before supporting them.
	var check func(map[string]any, bool) error
	check = func(table map[string]any, dependency bool) error {
		for key, value := range table {
			if key == "workspace" || dependency && key == "path" {
				return fmt.Errorf("cargo workspace and path dependencies are not supported")
			}
			if child, ok := value.(map[string]any); ok {
				if err := check(child, dependency || strings.HasSuffix(key, "dependencies") || key == "patch" || key == "replace"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return check(document, false)
}

func cargoValidateProjectPlan(ctx context.Context, plan UpdatePlan) error {
	if plan.Scope != "project" && plan.Scope != "workspace" {
		return parserError(fmt.Errorf("cargo project update requires a project scope"))
	}
	if _, err := cargoProjectState(plan.Directory); err != nil {
		return parserError(err)
	}
	root, err := runOutputDir(ctx, plan.Directory, plan.Executable, "locate-project", "--workspace", "--message-format", "plain")
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(strings.TrimSpace(string(root)))
	if err != nil {
		return parserError(err)
	}
	expected, err := filepath.EvalSymlinks(filepath.Join(plan.Directory, "Cargo.toml"))
	if err != nil || actual != expected {
		return parserError(fmt.Errorf("cargo scope must be standalone project root"))
	}
	return nil
}

func cargoProjectValidVersion(version string) bool {
	return cargoProjectVersion.MatchString(version) && semver.IsValid("v"+version)
}

func cargoProjectUpdateArgs(packages []Package) []string {
	if len(packages) != 1 {
		return nil
	}
	pkg := packages[0]
	if !cargoProjectName.MatchString(pkg.Name) || !cargoProjectValidVersion(pkg.Current) || !cargoProjectValidVersion(pkg.Latest) || pkg.Current == pkg.Latest || (pkg.Requested != "" && pkg.Requested != pkg.Latest) {
		return nil
	}
	return []string{"update", "--color", "never", pkg.Name + "@" + pkg.Current, "--precise", pkg.Latest}
}

func cargoProjectOutdated(ctx context.Context, executable string, scope Scope, packages Packages) error {
	if (scope.Kind != "project" && scope.Kind != "workspace") || scope.Path == "" {
		return parserError(fmt.Errorf("cargo project update requires a workspace root scope"))
	}
	if err := cargoValidateProjectPlan(ctx, UpdatePlan{Scope: scope.Kind, Directory: scope.Path, Executable: executable}); err != nil {
		return err
	}
	manifest := filepath.Join(scope.Path, "Cargo.toml")
	lockpath := filepath.Join(scope.Path, "Cargo.lock")
	info, err := os.Lstat(lockpath)
	if err != nil {
		return fmt.Errorf("read Cargo.lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return parserError(fmt.Errorf("cargo lockfile must be a regular, non-symlink file"))
	}
	lockbytes, err := os.ReadFile(lockpath)
	if err != nil {
		return err
	}
	var lock struct {
		Packages []struct {
			Name    string `toml:"name"`
			Version string `toml:"version"`
			Source  string `toml:"source"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(lockbytes, &lock); err != nil {
		return parserError(fmt.Errorf("decode Cargo.lock: %w", err))
	}
	counts := make(map[string]int)
	for _, pkg := range lock.Packages {
		counts[pkg.Name+"@"+pkg.Version]++
	}
	var found []Tool
	for _, pkg := range lock.Packages {
		// ponytail: crates.io lockfile updates only; source-qualified records
		// and source-aware verification are needed before other sources are safe.
		if pkg.Source != cargoProjectRegistry {
			continue
		}
		record := Package{Manager: PM_CARGO, Name: pkg.Name, Current: pkg.Version, Role: RoleLibrary,
			DependencyClass: "transitive", Manifest: manifest, Executable: executable, Scope: scope.Kind, Path: scope.Path}
		if !cargoProjectName.MatchString(pkg.Name) || !cargoProjectValidVersion(pkg.Version) || counts[pkg.Name+"@"+pkg.Version] != 1 {
			return parserError(fmt.Errorf("invalid or source-ambiguous cargo package %s@%s", pkg.Name, pkg.Version))
		}
		args := []string{"update", "--color", "never", pkg.Name + "@" + pkg.Version, "--dry-run"}
		output, err := cargoProjectDryRun(ctx, executable, scope.Path, args)
		if err != nil {
			return err
		}
		after, err := os.ReadFile(lockpath)
		if err != nil || !bytes.Equal(lockbytes, after) {
			return parserError(fmt.Errorf("cargo dry run unexpectedly changed Cargo.lock"))
		}
		target, err := cargoProjectTarget(output, pkg.Name, pkg.Version)
		if err != nil {
			return err
		}
		if target != "" {
			if counts[pkg.Name+"@"+target] == 0 {
				record.Config = cargoProjectTargetAbsent
			}
			record.Latest, record.Requested = target, target
			found = append(found, recordTool{record})
		}
	}
	packages[PM_CARGO] = append(packages[PM_CARGO], found...)
	return nil
}

func cargoProjectDryRun(ctx context.Context, executable, dir string, args []string) ([]byte, error) {
	cmd := commandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env = dir, managerEnvironment(dir)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return nil, classifyOperationError(ctx, fmt.Errorf("cargo update dry run: %w: %s", err, output.String()), output.String())
	}
	return output.Bytes(), nil
}

func cargoProjectTarget(output []byte, name, current string) (string, error) {
	var target string
	var dryRun, locking, removed bool
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case line == "warning: not updating lockfile due to dry run":
			dryRun = true
			continue
		case cargoProjectLocking.MatchString(line):
			locking = true
			continue
		case line == "Updating crates.io index", strings.HasPrefix(line, "Blocking waiting for file lock on package cache"),
			strings.HasPrefix(line, "note: pass `--verbose` to see ") && strings.HasSuffix(line, " unchanged dependencies behind latest"):
			continue
		}
		change := cargoProjectChange.FindStringSubmatch(line)
		if change == nil || !cargoProjectValidVersion(change[3]) || (change[4] != "" && !cargoProjectValidVersion(change[4])) {
			return "", parserError(fmt.Errorf("unrecognized cargo dry-run output %q", line))
		}
		if (change[1] == "Updating" || change[1] == "Downgrading") != (change[4] != "") {
			return "", parserError(fmt.Errorf("invalid cargo version change %q", line))
		}
		if change[2] != name {
			continue
		}
		if change[1] == "Removing" && change[3] == current && target == "" && !removed {
			removed = true
			continue
		}
		if removed || change[1] != "Updating" || change[3] != current || change[4] == current || target != "" {
			return "", parserError(fmt.Errorf("ambiguous cargo target change %q", line))
		}
		target = change[4]
	}
	if !dryRun || !locking {
		return "", parserError(fmt.Errorf("incomplete cargo dry-run output"))
	}
	return target, nil
}
