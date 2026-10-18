package packagemanager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

const PM_YARN = "yarn"

type Yarn struct{}

func (Yarn) name() string                 { return PM_YARN }
func (Yarn) testedVersions() VersionRange { return VersionRange{MinimumMajor: 4, MaximumMajor: 4} }
func (Yarn) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate}
}
func (Yarn) updateImpact(Scope) MutationImpact        { return MutationImpact{MayChangeManifests: true} }
func (Yarn) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Yarn) projectFiles() []string                   { return []string{"package.json", "yarn.lock"} }
func (Yarn) updateArgs(packages []Package) []string {
	if len(packages) == 0 {
		return nil
	}
	args := []string{"up"}
	for _, pkg := range packages {
		if !yarnPackageName.MatchString(pkg.Name) {
			return nil
		}
		args = append(args, pkg.Name)
	}
	return args
}
func (Yarn) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	output, err := runOutputDir(ctx, scope.Path, executable, "info", "--json")
	if err != nil {
		return nil, err
	}
	return parseYarnInstalled(output)
}
func (y Yarn) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	if err := rejectYarnWorkspace(scope.Path); err != nil {
		return err
	}
	installed, err := y.listInstalled(ctx, executable, scope)
	if err != nil {
		return err
	}
	outdated := make(Packages)
	for _, pkg := range installed {
		output, err := runOutputDir(ctx, scope.Path, executable, "npm", "info", pkg.Name, "--json", "--fields", "name,dist-tags")
		if err != nil {
			return err
		}
		var info struct {
			Name string `json:"name"`
			Tags struct {
				Latest string `json:"latest"`
			} `json:"dist-tags"`
		}
		if err := json.Unmarshal(output, &info); err != nil {
			return parserError(fmt.Errorf("decode yarn npm info: %w", err))
		}
		if info.Name != pkg.Name || !semver.IsValid("v"+info.Tags.Latest) {
			return parserError(fmt.Errorf("decode yarn npm info for %q: missing or invalid package fields", pkg.Name))
		}
		if semver.Compare("v"+info.Tags.Latest, "v"+pkg.Version) > 0 {
			outdated[pkg.Name] = append(outdated[pkg.Name], recordTool{Package: Package{Manager: PM_YARN, Name: pkg.Name, Current: pkg.Version, Latest: info.Tags.Latest, Requested: info.Tags.Latest, Executable: executable}})
		}
	}
	MergePackages(packages, outdated)
	return nil
}

func rejectYarnWorkspace(path string) error {
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read Yarn project manifest: %w", err)
		}
		if err == nil {
			var manifest struct {
				Workspaces     json.RawMessage `json:"workspaces"`
				PackageManager string          `json:"packageManager"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return parserError(fmt.Errorf("decode Yarn project manifest: %w", err))
			}
			if len(manifest.Workspaces) != 0 || (dir != root && strings.HasPrefix(manifest.PackageManager, "yarn@")) {
				return fmt.Errorf("Yarn outdated/update planning is unsupported for workspace projects: yarn up may modify workspaces outside scope %q", path)
			}
		}
		if dir != root {
			for _, name := range []string{"yarn.lock", ".yarnrc.yml"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return fmt.Errorf("Yarn outdated/update planning is unsupported for workspace projects: ancestor Yarn project at %q", dir)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

var yarnPackageName = regexp.MustCompile(`^(@[a-zA-Z0-9_][a-zA-Z0-9._-]*/)?[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

type yarnInfo struct {
	Value    string `json:"value"`
	Children struct {
		Version string `json:"Version"`
	} `json:"children"`
}

func parseYarnInstalled(output []byte) ([]InstalledPackage, error) {
	var packages []InstalledPackage
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var info yarnInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
			return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
		}
		separator := strings.Index(info.Value, "@")
		if strings.HasPrefix(info.Value, "@") {
			separator = strings.Index(info.Value[1:], "@") + 1
		}
		if separator <= 0 || !yarnPackageName.MatchString(info.Value[:separator]) {
			return nil, parserError(fmt.Errorf("decode yarn inventory: missing package fields"))
		}
		name, reference := info.Value[:separator], info.Value[separator+1:]
		if strings.HasPrefix(reference, "virtual:") {
			_, reference, _ = strings.Cut(reference, "#")
		}
		if !strings.HasPrefix(reference, "npm:") {
			if !strings.Contains(reference, ":") {
				return nil, parserError(fmt.Errorf("decode yarn inventory: invalid locator"))
			}
			continue
		}
		if !semver.IsValid("v" + info.Children.Version) {
			return nil, parserError(fmt.Errorf("decode yarn inventory: invalid version"))
		}
		if strings.TrimPrefix(reference, "npm:") != info.Children.Version {
			// ponytail: aliases are skipped; add descriptor-aware lookup before supporting them.
			continue
		}
		packages = append(packages, InstalledPackage{Manager: PM_YARN, Name: name, Version: info.Children.Version, Role: RoleLibrary})
	}
	if err := scanner.Err(); err != nil {
		return nil, parserError(fmt.Errorf("decode yarn inventory: %w", err))
	}
	return packages, nil
}
