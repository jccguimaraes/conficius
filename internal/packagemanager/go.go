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
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

type Go struct{}

func (Go) name() string                 { return PM_GO }
func (Go) testedVersions() VersionRange { return VersionRange{MinimumMajor: 1, MaximumMajor: 1} }
func (Go) capabilities() []Capability {
	return []Capability{CapabilityGlobalInventory, CapabilityProjectInventory, CapabilityGlobalOutdated, CapabilityProjectOutdated, CapabilityGlobalUpdate}
}
func (Go) updateImpact(Scope) MutationImpact { return MutationImpact{} }
func (Go) recoveryArgs(_ Scope, packages []Package) [][]string {
	args := make([][]string, 0, len(packages))
	for _, pkg := range packages {
		args = append(args, []string{"install", pkg.Name + "@" + pkg.Current})
	}
	return args
}
func (Go) projectFiles() []string { return []string{"go.mod"} }
func (Go) updateArgs(packages []Package) []string {
	if len(packages) != 1 {
		return nil
	}
	return []string{"install", packages[0].Name + "@" + packages[0].Latest}
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
			class := "direct"
			if module.Indirect {
				class = "indirect"
			}
			packages = append(packages, InstalledPackage{Manager: PM_GO, Name: identity.Path, Version: identity.Version, Role: RoleLibrary, Manifest: filepath.Join(scope.Path, "go.mod"), DependencyClass: class, Transitive: module.Indirect, OSVEcosystem: "Go", OSVName: identity.Path})
		}
		return packages, nil
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
