package packagemanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	goModule "golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

type Go struct{}

func (Go) name() string                 { return PM_GO }
func (Go) testedVersions() VersionRange { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Go) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate, CapabilityProjectUpdate}
}
func (Go) updateImpact(scope Scope) MutationImpact {
	return MutationImpact{MayChangeManifests: scope.Kind == "project" || scope.Kind == "workspace"}
}
func (Go) recoveryArgs(scope Scope, packages []Package) [][]string {
	if scope.Kind == "project" || scope.Kind == "workspace" {
		return nil
	}
	args := make([][]string, 0, len(packages))
	for _, pkg := range packages {
		args = append(args, []string{"install", pkg.Name + "@" + pkg.Current})
	}
	return args
}
func (Go) projectFiles() []string { return []string{"go.mod", "go.sum"} }
func (Go) updateArgs(packages []Package) []string {
	if len(packages) == 0 {
		return nil
	}
	project := packages[0].Scope == "project" || packages[0].Scope == "workspace"
	args := []string{"install"}
	if project {
		args[0] = "get"
	} else if len(packages) != 1 {
		return nil
	}
	for _, pkg := range packages {
		if goModule.CheckPath(pkg.Name) != nil || !semver.IsValid(pkg.Latest) || pkg.Scope != packages[0].Scope || pkg.Path != packages[0].Path {
			return nil
		}
		args = append(args, pkg.Name+"@"+pkg.Latest)
	}
	return args
}
func (Go) listOutdatedScope(ctx context.Context, executable string, scope Scope, _ ManagerOptions, packages Packages) error {
	if scope.Kind == "project" || scope.Kind == "workspace" {
		modules, err := goProjectModules(ctx, executable, scope, true)
		if err != nil {
			return err
		}
		manifest := filepath.Join(scope.Path, "go.mod")
		data, err := os.ReadFile(manifest)
		if err != nil {
			return parserError(fmt.Errorf("read Go project manifest: %w", err))
		}
		file, err := modfile.Parse(manifest, data, nil)
		if err != nil {
			return parserError(fmt.Errorf("parse Go project manifest: %w", err))
		}
		toolModules := make(map[string]bool)
		for _, tool := range file.Tool {
			var path string
			for _, module := range modules {
				if (tool.Path == module.Path || strings.HasPrefix(tool.Path, module.Path+"/")) && len(module.Path) > len(path) {
					path = module.Path
				}
			}
			if path != "" {
				toolModules[path] = true
			}
		}
		for _, module := range modules {
			if module.Main || module.Path == "go" || module.Path == "toolchain" || module.Replace != nil || module.Update == nil || (module.Indirect && !toolModules[module.Path]) {
				continue
			}
			if semver.Compare(module.Update.Version, module.Version) > 0 {
				class := "direct"
				if module.Indirect {
					class = "indirect"
				}
				packages[module.Path] = append(packages[module.Path], scopedTool{Tool: goTool{module: module.Path, current: module.Version, latest: module.Update.Version, executable: executable}, scope: scope, dependencyClass: class})
			}
		}
		return nil
	}
	path, err := GoToolJournalPath()
	if err != nil {
		return err
	}
	journal, err := ReadGoToolJournal(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return parserError(fmt.Errorf("read Go tool journal: %w", err))
	}
	for _, imported := range journal.Imports {
		output, err := runOutput(ctx, executable, "list", "-m", "-json", imported.Module+"@latest")
		if err != nil {
			return err
		}
		var module struct {
			Path    string
			Version string
		}
		if err := json.Unmarshal(output, &module); err != nil {
			return parserError(fmt.Errorf("decode Go module freshness: %w", err))
		}
		if module.Path != imported.Module || !semver.IsValid(module.Version) {
			return parserError(fmt.Errorf("decode Go module freshness for %q", imported.Module))
		}
		if module.Version != imported.Version {
			packages[imported.Module] = append(packages[imported.Module], goTool{module: imported.Module, current: imported.Version, latest: module.Version, executable: executable})
		}
	}
	return nil
}
func (Go) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	if scope.Kind == "project" || scope.Kind == "workspace" {
		modules, err := goProjectModules(ctx, executable, scope, false)
		if err != nil {
			return nil, err
		}
		classes, scanScope, loadErr := goDependencyClasses(ctx, executable, scope)
		var packages []InstalledPackage
		for _, module := range modules {
			if module.Main || module.Path == "go" || module.Path == "toolchain" {
				continue
			}
			identity := module
			if module.Replace != nil {
				identity = *module.Replace
				// Local replacements have no published version; omit from native OSV inventory.
				if identity.Version == "" {
					continue
				}
			}
			class := classes[module.Path]
			if class == "" {
				class = "graph-only"
			}
			packages = append(packages, InstalledPackage{Manager: PM_GO, Name: identity.Path, Version: identity.Version, Role: RoleLibrary, Manifest: filepath.Join(scope.Path, "go.mod"), DependencyClass: class, ScanScope: scanScope, Transitive: module.Indirect, OSVEcosystem: "Go", OSVName: identity.Path})
		}
		return packages, loadErr
	}
	path, err := GoToolJournalPath()
	if err != nil {
		return nil, err
	}
	journal, err := ReadGoToolJournal(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, parserError(fmt.Errorf("read Go tool journal: %w", err))
	}
	packages := make([]InstalledPackage, 0, len(journal.Imports))
	for _, imported := range journal.Imports {
		packages = append(packages, InstalledPackage{Manager: PM_GO, Name: imported.Module, Version: imported.Version, Role: RoleDevelopmentTool})
	}
	return packages, nil
}

// goDependencyClasses uses package membership, not go.mod's indirect marker.
// Application wins over test, which wins over tool when a module serves several roles.
func goDependencyClasses(ctx context.Context, executable string, scope Scope) (map[string]string, string, error) {
	classes := make(map[string]string)
	scanScope := "Go host " + runtime.GOOS + "/" + runtime.GOARCH + "; selected Go current build environment (may override host): GOOS/GOARCH, CGO_ENABLED and GOFLAGS/build tags; ./... application and tests, declared tools; module/version matches, not call reachability; local replacements excluded"
	var loadErrors []error
	for _, load := range []struct {
		class string
		args  []string
	}{
		{"application", []string{"./..."}},
		{"test", []string{"-test", "./..."}},
		{"tool", []string{"tool"}},
	} {
		args := append([]string{"list", "-deps", "-e", "-json", "-mod=readonly"}, load.args...)
		cmd := commandContext(ctx, executable, args...)
		cmd.Dir = scope.Path
		cmd.Env = append(managerEnvironment(scope.Path), "GOPROXY=off", "GONOPROXY=none", "GOSUMDB=off", "GOTOOLCHAIN=local")
		output, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			var stderr string
			if errors.As(err, &exitErr) {
				stderr = string(exitErr.Stderr)
			}
			loadErrors = append(loadErrors, fmt.Errorf("Go %s package loading incomplete: %w", load.class, classifyOperationError(ctx, err, stderr)))
		}
		decoder := json.NewDecoder(bytes.NewReader(output))
		for {
			var pkg struct {
				Module     *goProjectModule
				Incomplete bool
				Error      *struct{ Err string }
				DepsErrors []struct{ Err string }
			}
			if err := decoder.Decode(&pkg); err != nil {
				if !errors.Is(err, io.EOF) {
					loadErrors = append(loadErrors, parserError(fmt.Errorf("decode Go %s packages: %w", load.class, err)))
				}
				break
			}
			if pkg.Incomplete || pkg.Error != nil || len(pkg.DepsErrors) > 0 {
				reason := "missing dependency metadata"
				if pkg.Error != nil {
					reason = pkg.Error.Err
				} else if len(pkg.DepsErrors) > 0 {
					reason = pkg.DepsErrors[0].Err
				}
				loadErrors = append(loadErrors, fmt.Errorf("Go %s package loading incomplete: %s", load.class, reason))
			}
			if pkg.Module != nil && classes[pkg.Module.Path] == "" {
				classes[pkg.Module.Path] = load.class
			}
		}
	}
	if len(loadErrors) > 0 {
		scanScope += "; INCOMPLETE package loading: graph-only means usage unconfirmed"
	}
	return classes, scanScope, errors.Join(loadErrors...)
}

type goProjectModule struct {
	Path     string
	Version  string
	Main     bool
	Indirect bool
	Replace  *goProjectModule
	Update   *goProjectModule
	Error    *struct{ Err string }
}

func (module goProjectModule) validate(local bool) error {
	if module.Error != nil {
		return fmt.Errorf("module %q: %s", module.Path, module.Error.Err)
	}
	if module.Path == "" {
		return fmt.Errorf("missing Go module path")
	}
	if module.Path == "go" || module.Path == "toolchain" {
		return nil
	}
	if !module.Main && (!local || module.Version != "") && goModule.CheckPath(module.Path) != nil {
		return fmt.Errorf("invalid Go module path: %q", module.Path)
	}
	if !module.Main && (!local || module.Version != "") && !semver.IsValid(module.Version) {
		return fmt.Errorf("invalid Go module version for %q: %q", module.Path, module.Version)
	}
	if module.Replace != nil {
		if err := module.Replace.validate(true); err != nil {
			return err
		}
	}
	if module.Update != nil {
		if err := module.Update.validate(false); err != nil {
			return err
		}
		if module.Update.Path != module.Path {
			return fmt.Errorf("Go module update path mismatch for %q", module.Path)
		}
	}
	return nil
}

// resolveGoOperations identifies compilers in each scope, where shims may select
// different installations. Keep discovery path identity when self-identification fails.
func resolveGoOperations[T any](ctx context.Context, operations []readOperation[T], statuses []ManagerStatus) []readOperation[T] {
	type key struct {
		executable string
		scope      Scope
	}
	resolved := make(map[key]string)
	seen := make(map[key]bool)
	paths := make(map[int][]string)
	scopedPaths := make(map[Scope]map[string]bool)
	result := operations[:0]
	for _, operation := range operations {
		if operation.manager != PM_GO {
			result = append(result, operation)
			continue
		}
		lookup := key{operation.executable, operation.scope}
		identity, cached := resolved[lookup]
		if !cached {
			identity = operation.executable
			probeContext, cancel := context.WithTimeout(ctx, operationTimeout)
			output, err := runOutputDir(probeContext, operation.scope.Path, operation.executable, "env", "GOROOT")
			cancel()
			root := strings.TrimSpace(string(output))
			if err == nil && filepath.IsAbs(root) {
				binary := "go"
				if runtime.GOOS == "windows" {
					binary += ".exe"
				}
				canonical, err := filepath.EvalSymlinks(filepath.Join(root, "bin", binary))
				if err == nil && isExecutable(canonical) {
					identity = canonical
				}
			}
			resolved[lookup] = identity
		}
		operationKey := key{identity, operation.scope}
		if seen[operationKey] {
			continue
		}
		seen[operationKey] = true
		// The run closure retains the selected PATH entry; expose that same entry
		// in records and plans rather than switching the command after inspection.
		result = append(result, operation)
		paths[operation.managerIndex] = append(paths[operation.managerIndex], operation.executable)
		if scopedPaths[operation.scope] == nil {
			scopedPaths[operation.scope] = make(map[string]bool)
		}
		scopedPaths[operation.scope][identity] = true
	}
	for i, executables := range paths {
		status := &statuses[i]
		status.Executables = nil
		for _, executable := range executables {
			if !slices.Contains(status.Executables, executable) {
				status.Executables = append(status.Executables, executable)
			}
		}
		status.Warnings = slices.DeleteFunc(status.Warnings, func(warning string) bool {
			return strings.HasPrefix(warning, "multiple executables found on PATH: ")
		})
		for _, identities := range scopedPaths {
			if len(identities) > 1 {
				status.Warnings = append(status.Warnings, "multiple executables found on PATH: "+strings.Join(status.Executables, ", "))
				break
			}
		}
	}
	return result
}

func goProjectModules(ctx context.Context, executable string, scope Scope, outdated bool) ([]goProjectModule, error) {
	args := []string{"list", "-m", "-json", "-mod=readonly"}
	if outdated {
		args = append(args, "-u")
	}
	args = append(args, "all")
	cmd := commandContext(ctx, executable, args...)
	cmd.Dir = scope.Path
	cmd.Env = managerEnvironment(scope.Path)
	if !outdated {
		// Inventory uses cached metadata only, never freshness or automatic toolchain downloads.
		cmd.Env = append(cmd.Env, "GOPROXY=off", "GONOPROXY=none", "GOSUMDB=off", "GOTOOLCHAIN=local")
	}
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		var stderr string
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		return nil, classifyOperationError(ctx, err, stderr)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var modules []goProjectModule
	for {
		var module goProjectModule
		if err := decoder.Decode(&module); err != nil {
			if errors.Is(err, io.EOF) && len(modules) > 0 {
				return modules, nil
			}
			return nil, parserError(fmt.Errorf("decode Go project modules: %w", err))
		}
		if err := module.validate(false); err != nil {
			return nil, parserError(err)
		}
		modules = append(modules, module)
	}
}

type goTool struct {
	module     string
	current    string
	latest     string
	executable string
}

func (tool goTool) GetPackageManager() string   { return PM_GO }
func (tool goTool) GetName() string             { return tool.module }
func (tool goTool) GetCurrentVersion() string   { return tool.current }
func (tool goTool) GetLatestVersion() string    { return tool.latest }
func (tool goTool) GetRequestedVersion() string { return "" }
func (tool goTool) GetExecutable() string       { return tool.executable }

func GoToolJournalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory for Go tool journal: %w", err)
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "conficius", "go-tools.json"), nil
}
