package packagemanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/jccguimaraes/conficius/internal/config"
)

var RegisteredPackageManagers = registeredManagerNames()

func registeredManagerNames() []string {
	names := make([]string, len(adapters))
	for i, adapter := range adapters {
		names[i] = adapter.name()
	}
	return names
}

const (
	StatusAvailable   = "available"
	StatusUnavailable = "unavailable"
	StatusUnsupported = "unsupported"
	StatusDisabled    = "disabled"
	StatusPartial     = "partial"
	StatusError       = "error"
)

type OperationStatus struct {
	Operation  string        `json:"operation"`
	Executable string        `json:"executable"`
	Scope      string        `json:"scope"`
	Path       string        `json:"path,omitempty"`
	Status     string        `json:"status"`
	Category   ErrorCategory `json:"category,omitempty"`
	Error      string        `json:"error,omitempty"`
}

type Tool interface {
	GetPackageManager() string
	GetName() string
	GetCurrentVersion() string
	GetLatestVersion() string
	GetRequestedVersion() string
	GetExecutable() string
}

type Packages map[string][]Tool

type scopedTool struct {
	Tool
	scope           Scope
	dependencyClass string
}

type ManagerEvidence struct {
	Executable   bool `json:"executable"`
	GlobalConfig bool `json:"global_config"`
}

type ManagerStatus struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	Evidence       ManagerEvidence   `json:"evidence"`
	AdapterVersion string            `json:"adapter_version"`
	ManagerVersion string            `json:"manager_version,omitempty"`
	TestedVersions VersionRange      `json:"tested_versions"`
	VersionTested  bool              `json:"version_tested"`
	Capabilities   []Capability      `json:"capabilities,omitempty"`
	Executables    []string          `json:"executables,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
	Operations     []OperationStatus `json:"operations,omitempty"`
	Error          string            `json:"error,omitempty"`
}

type ManagerIntegrity struct {
	Manager string `json:"manager"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func CheckManagerIntegrity(pms []string, options QueryOptions) ([]ManagerIntegrity, error) {
	statuses, err := inspectWithOptions(pms, options)
	if err != nil {
		return nil, err
	}
	checks := make([]ManagerIntegrity, len(statuses))
	for i, status := range statuses {
		check := ManagerIntegrity{Manager: status.Name, Status: status.Status, Message: status.Error}
		if status.Status == StatusAvailable {
			check.Status = "healthy"
			check.Message = "executable and manager version available"
			if status.ManagerVersion != "" && !status.VersionTested {
				check.Status = "warning"
				check.Message = fmt.Sprintf("manager version %s is outside tested range", status.ManagerVersion)
			}
		}
		checks[i] = check
	}
	return checks, nil
}

type ManagerOptions struct {
	IncludeBumpCandidates bool
	IncludeGreedy         bool
	ExpectedGlobalPrefix  string
}

type QueryOptions struct {
	Managers          map[string]ManagerOptions
	Executables       map[string]string
	GlobalConfig      map[string]bool
	Disabled          map[string]bool
	Required          map[string]bool
	DependencyClasses map[string]map[string]bool
	IncludeTransitive bool
}

func QueryOptionsFromProfile(profile config.Profile) QueryOptions {
	options := QueryOptions{
		GlobalConfig: make(map[string]bool),
		Disabled:     make(map[string]bool),
		Required:     make(map[string]bool),
	}
	for manager, settings := range profile.Managers {
		options.GlobalConfig[manager] = true
		if settings.Enabled != nil {
			if *settings.Enabled {
				options.Required[manager] = true
			} else {
				options.Disabled[manager] = true
			}
		}
	}
	return options
}

type QueryResult struct {
	Managers []ManagerStatus
	Packages Packages
}

type Package struct {
	Manager         string      `json:"manager"`
	Name            string      `json:"name"`
	Current         string      `json:"current"`
	Latest          string      `json:"latest"`
	Requested       string      `json:"requested,omitempty"`
	Role            PackageRole `json:"role"`
	DependencyClass string      `json:"dependency_class,omitempty"`
	Manifest        string      `json:"manifest,omitempty"`
	Config          string      `json:"config,omitempty"`
	Executable      string      `json:"executable"`
	Scope           string      `json:"scope,omitempty"`
	Path            string      `json:"path,omitempty"`
}

type PlanConfidence string

const (
	PlanConfidenceExact     PlanConfidence = "exact"
	PlanConfidenceEstimated PlanConfidence = "estimated"
	PlanConfidenceUnknown   PlanConfidence = "unknown"
)

type RecoveryCommand struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

type UpdatePlan struct {
	Manager    string            `json:"manager"`
	Executable string            `json:"executable"`
	Directory  string            `json:"directory,omitempty"`
	Scope      string            `json:"scope"`
	Confidence PlanConfidence    `json:"confidence"`
	StateHash  string            `json:"state_hash"`
	Impact     MutationImpact    `json:"impact"`
	Args       []string          `json:"args"`
	Recovery   []RecoveryCommand `json:"recovery,omitempty"`
	Packages   []Package         `json:"packages"`
}

var commandContext = exec.CommandContext

func Supported(pms []string) error {
	list := make(map[string]struct{}, len(RegisteredPackageManagers))
	for _, pm := range RegisteredPackageManagers {
		list[pm] = struct{}{}
	}

	var invalid []string
	for _, pm := range pms {
		if _, ok := list[pm]; !ok {
			invalid = append(invalid, pm)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("unsupported package managers: %s", strings.Join(invalid, ", "))
	}

	return nil
}

func Inspect(pms []string) ([]ManagerStatus, error) {
	if len(pms) == 0 {
		pms = RegisteredPackageManagers
	}
	if err := Supported(pms); err != nil {
		return nil, err
	}

	statuses := make([]ManagerStatus, 0, len(pms))
	for _, pm := range pms {
		adapter, err := adapterFor(pm)
		if err != nil {
			return nil, err
		}
		executables, err := LookPathAll(pm)
		if errors.Is(err, exec.ErrNotFound) {
			statuses = append(statuses, ManagerStatus{
				Name:           pm,
				Status:         StatusUnavailable,
				AdapterVersion: adapterVersion,
				TestedVersions: adapter.testedVersions(),
				Capabilities:   adapter.capabilities(),
				Error:          "executable not found",
			})
			continue
		}
		if err != nil {
			statuses = append(statuses, ManagerStatus{Name: pm, Status: StatusError, AdapterVersion: adapterVersion, TestedVersions: adapter.testedVersions(), Capabilities: adapter.capabilities(), Error: redactSensitive(err.Error())})
			continue
		}

		status := ManagerStatus{Name: pm, Status: StatusAvailable, Evidence: ManagerEvidence{Executable: true}, AdapterVersion: adapterVersion, TestedVersions: adapter.testedVersions(), Capabilities: adapter.capabilities(), Executables: executables}
		if len(executables) > 1 {
			status.Warnings = append(status.Warnings, "multiple executables found on PATH: "+strings.Join(executables, ", "))
		}
		output, versionErr := runOutput(context.Background(), executables[0], managerVersionArgs(pm)...)
		if versionErr != nil {
			status.Warnings = append(status.Warnings, "read manager version: "+redactSensitive(versionErr.Error()))
		} else {
			status.ManagerVersion = firstVersion(string(output))
			if status.ManagerVersion == "" {
				status.Warnings = append(status.Warnings, "read manager version: no version in output")
			} else {
				status.VersionTested = versionInRange(status.ManagerVersion, status.TestedVersions)
				if !status.VersionTested {
					status.Warnings = append(status.Warnings, fmt.Sprintf("manager version %s is outside tested major range %d-%d", status.ManagerVersion, status.TestedVersions.MinimumMajor, status.TestedVersions.MaximumMajor))
				}
			}
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func QueryOutdated(ctx context.Context, pms []string) (QueryResult, error) {
	return QueryOutdatedScopes(ctx, pms, []Scope{{Kind: "global"}})
}

func QueryOutdatedScopes(ctx context.Context, pms []string, scopes []Scope) (QueryResult, error) {
	return QueryOutdatedScopesWithOptions(ctx, pms, scopes, QueryOptions{})
}

func QueryOutdatedScopesWithOptions(ctx context.Context, pms []string, scopes []Scope, options QueryOptions) (QueryResult, error) {
	var err error
	scopes, err = normalizeScopes(scopes)
	if err != nil {
		return QueryResult{}, err
	}
	options, err = requireManifestManagers(options, scopes)
	if err != nil {
		return QueryResult{}, err
	}
	statuses, err := inspectWithOptions(pms, options)
	if err != nil {
		return QueryResult{}, err
	}

	result := QueryResult{Managers: statuses, Packages: make(Packages)}
	var operations []readOperation[Packages]
	for i := range result.Managers {
		status := &result.Managers[i]
		if status.Status != StatusAvailable {
			continue
		}
		adapter, adapterErr := adapterFor(status.Name)
		if adapterErr != nil {
			return QueryResult{}, adapterErr
		}
		for _, executable := range status.Executables {
			if status.Name == PM_NPM && options.Managers[PM_NPM].ExpectedGlobalPrefix != "" {
				status.Warnings = append(status.Warnings, npmPrefixWarnings(ctx, executable, options.Managers[PM_NPM].ExpectedGlobalPrefix)...)
			}
			for _, scope := range scopes {
				if !hasProjectManagerManifest(adapter, scope) {
					continue
				}
				if !hasCapability(adapter, scopeCapability(scope, CapabilityGlobalOutdated, CapabilityProjectOutdated)) {
					status.Operations = append(status.Operations, unsupportedOperationStatus("outdated", executable, scope))
					continue
				}
				manager, executable, scope := status.Name, executable, scope
				operations = append(operations, readOperation[Packages]{managerIndex: i, manager: manager, executable: executable, scope: scope, run: func(ctx context.Context) (Packages, error) {
					packages := make(Packages)
					err := listOutdatedScope(ctx, manager, executable, scope, options.Managers[manager], packages)
					return packages, err
				}})
			}
		}
	}
	queryErrors := requiredManagerErrors(result.Managers)
	for _, operationResult := range runReadOperations(ctx, operations) {
		status := &result.Managers[operationResult.operation.managerIndex]
		operationStatus := newOperationStatus("outdated", operationResult.operation, operationResult.err)
		status.Operations = append(status.Operations, operationStatus)
		if operationResult.err != nil {
			status.Error = redactSensitive(operationResult.err.Error())
			queryErrors = append(queryErrors, fmt.Errorf("%s (%s, %s): %w", operationResult.operation.manager, operationResult.operation.executable, operationResult.operation.scope.Kind, operationResult.err))
			continue
		}
		classes := options.DependencyClasses[operationResult.operation.scope.Path]
		dependencyClasses, classErr := npmDependencyClasses(operationResult.operation.scope)
		if operationResult.operation.manager == PM_NPM && len(classes) > 0 && classErr != nil {
			status.Error = redactSensitive(classErr.Error())
			queryErrors = append(queryErrors, classErr)
			continue
		}
		for name, tools := range operationResult.value {
			dependencyClass := dependencyClasses[name]
			if operationResult.operation.manager == PM_NPM && dependencyClass == "" && (operationResult.operation.scope.Kind == "project" || operationResult.operation.scope.Kind == "workspace") {
				dependencyClass = "extraneous"
			}
			if operationResult.operation.manager == PM_NPM && len(classes) > 0 && !classes[dependencyClass] {
				continue
			}
			for _, tool := range tools {
				result.Packages[name] = append(result.Packages[name], scopedTool{Tool: tool, scope: operationResult.operation.scope, dependencyClass: dependencyClass})
			}
		}
	}
	finalizeManagerStatuses(result.Managers)

	return result, errors.Join(queryErrors...)
}

func normalizeScopes(scopes []Scope) ([]Scope, error) {
	var errs []error
	result := make([]Scope, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	localRoots := make(map[string]int, len(scopes))
	for _, scope := range scopes {
		switch scope.Kind {
		case "system", "user", "global-config", "global":
			if scope.Path != "" {
				errs = append(errs, fmt.Errorf("%s scope must not have a path", scope.Kind))
			}
		case "project", "workspace":
			if scope.Path == "" {
				errs = append(errs, fmt.Errorf("%s scope requires a path", scope.Kind))
				continue
			}
			absolute, err := filepath.Abs(filepath.Clean(scope.Path))
			if err != nil {
				errs = append(errs, fmt.Errorf("resolve %s scope path %q: %w", scope.Kind, scope.Path, err))
				continue
			}
			resolved, err := filepath.EvalSymlinks(absolute)
			if err != nil {
				errs = append(errs, fmt.Errorf("resolve %s scope root %q: %w", scope.Kind, scope.Path, err))
				continue
			}
			scope.Path = absolute
			if index, exists := localRoots[resolved]; exists {
				if result[index].Kind == "project" && scope.Kind == "workspace" {
					result[index] = scope
				}
				continue
			}
			localRoots[resolved] = len(result)
			result = append(result, scope)
			continue
		default:
			errs = append(errs, fmt.Errorf("unsupported scope %q", scope.Kind))
			continue
		}
		key := scope.Kind + "\x00" + scope.Path
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, scope)
	}
	return result, errors.Join(errs...)
}

func hasProjectManagerManifest(adapter managerAdapter, scope Scope) bool {
	if scope.Kind != "project" && scope.Kind != "workspace" {
		return true
	}
	manifest := ""
	switch adapter.name() {
	case PM_NPM:
		manifest = "package.json"
	case PM_CARGO:
		manifest = "Cargo.lock"
	default:
		return true
	}
	info, err := os.Stat(filepath.Join(scope.Path, manifest))
	return err == nil && !info.IsDir()
}

func requireManifestManagers(options QueryOptions, scopes []Scope) (QueryOptions, error) {
	required := make(map[string]bool, len(options.Required))
	for manager, enabled := range options.Required {
		required[manager] = enabled
	}
	options.Required = required
	manifests := []struct {
		name    string
		manager string
	}{{".mise.toml", PM_MISE}, {"Brewfile", PM_BREW}, {"mise.toml", PM_MISE}, {"package.json", PM_NPM}}
	var conflicts []error
	seen := make(map[string]struct{})
	for _, scope := range scopes {
		if scope.Path == "" {
			continue
		}
		root, err := filepath.Abs(filepath.Clean(scope.Path))
		if err != nil {
			conflicts = append(conflicts, fmt.Errorf("resolve manager manifest root %s: %w", scope.Path, err))
			continue
		}
		for _, manifest := range manifests {
			path := filepath.Join(root, manifest.name)
			if _, exists := seen[path]; exists {
				continue
			}
			seen[path] = struct{}{}
			info, err := os.Lstat(path)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					conflicts = append(conflicts, fmt.Errorf("inspect manager manifest %s: %w", path, err))
				}
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(path)
				if err != nil {
					conflicts = append(conflicts, fmt.Errorf("inspect manager manifest %s: %w", path, err))
					continue
				}
			}
			if info.IsDir() {
				continue
			}
			if options.Disabled[manifest.manager] {
				conflicts = append(conflicts, fmt.Errorf("manager %s is disabled but required by %s", manifest.manager, path))
				continue
			}
			options.Required[manifest.manager] = true
		}
	}
	return options, errors.Join(conflicts...)
}

func requiredManagerErrors(statuses []ManagerStatus) []error {
	var errs []error
	for _, status := range statuses {
		if status.Status == StatusError && status.Error == "required manager is unavailable" {
			errs = append(errs, fmt.Errorf("%s: %s", status.Name, status.Error))
		}
	}
	return errs
}

func unsupportedOperationStatus(operation, executable string, scope Scope) OperationStatus {
	return OperationStatus{Operation: operation, Executable: executable, Scope: scope.Kind, Path: scope.Path, Status: StatusUnsupported}
}

func newOperationStatus[T any](operation string, result readOperation[T], err error) OperationStatus {
	status := OperationStatus{Operation: operation, Executable: result.executable, Scope: result.scope.Kind, Path: result.scope.Path, Status: StatusAvailable}
	if err == nil {
		return status
	}
	status.Status = StatusError
	status.Error = redactSensitive(err.Error())
	var operationErr *OperationError
	if errors.As(err, &operationErr) {
		status.Category = operationErr.Category
	}
	return status
}

func finalizeManagerStatuses(statuses []ManagerStatus) {
	for i := range statuses {
		status := &statuses[i]
		if len(status.Operations) == 0 {
			continue
		}
		successes := 0
		failures := 0
		for _, operation := range status.Operations {
			switch operation.Status {
			case StatusAvailable:
				successes++
			case StatusError:
				failures++
			}
		}
		switch {
		case failures == 0:
			status.Status = StatusAvailable
			status.Error = ""
		case successes > 0:
			status.Status = StatusPartial
		default:
			status.Status = StatusError
		}
	}
}

func inspectWithOptions(pms []string, options QueryOptions) ([]ManagerStatus, error) {
	if len(pms) == 0 {
		pms = RegisteredPackageManagers
	}
	if err := Supported(pms); err != nil {
		return nil, err
	}
	if len(pms) > 0 {
		selected := make(map[string]struct{}, len(pms))
		for _, manager := range pms {
			selected[manager] = struct{}{}
		}
		for manager := range options.Required {
			if _, exists := selected[manager]; !exists {
				return nil, fmt.Errorf("required manager %s is excluded by manager filter", manager)
			}
		}
	}
	statuses, err := Inspect(pms)
	if err != nil {
		return nil, err
	}
	for i := range statuses {
		statuses[i].Evidence.GlobalConfig = options.GlobalConfig[statuses[i].Name]
		if statuses[i].Evidence.GlobalConfig && statuses[i].Status == StatusUnavailable && statuses[i].Error == "executable not found" {
			statuses[i].Error = "executable not found; global config detected"
		}
		if options.Disabled[statuses[i].Name] {
			statuses[i].Status = StatusDisabled
			statuses[i].Error = ""
			continue
		}
		if executable := options.Executables[statuses[i].Name]; executable != "" {
			adapter, adapterErr := adapterFor(statuses[i].Name)
			if adapterErr != nil {
				return nil, adapterErr
			}
			statuses[i] = inspectExecutable(statuses[i].Name, executable, adapter)
			statuses[i].Evidence.GlobalConfig = options.GlobalConfig[statuses[i].Name]
		}
		if options.Required[statuses[i].Name] && statuses[i].Status != StatusAvailable {
			statuses[i].Status = StatusError
			statuses[i].Error = "required manager is unavailable"
		}
	}
	return statuses, nil
}

func managerVersionArgs(manager string) []string {
	if manager == PM_GO {
		return []string{"version"}
	}
	return []string{"--version"}
}

func inspectExecutable(manager, executable string, adapter managerAdapter) ManagerStatus {
	status := ManagerStatus{Name: manager, Status: StatusAvailable, Evidence: ManagerEvidence{Executable: true}, AdapterVersion: adapterVersion, TestedVersions: adapter.testedVersions(), Capabilities: adapter.capabilities(), Executables: []string{executable}}
	info, err := os.Stat(executable)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		status.Status = StatusUnavailable
		status.Evidence.Executable = false
		status.Error = "configured executable is unavailable"
		return status
	}
	output, err := runOutput(context.Background(), executable, managerVersionArgs(manager)...)
	if err != nil {
		status.Warnings = append(status.Warnings, "read manager version: "+redactSensitive(err.Error()))
		return status
	}
	status.ManagerVersion = firstVersion(string(output))
	if status.ManagerVersion == "" {
		status.Warnings = append(status.Warnings, "read manager version: no version in output")
		return status
	}
	status.VersionTested = versionInRange(status.ManagerVersion, status.TestedVersions)
	if !status.VersionTested {
		status.Warnings = append(status.Warnings, fmt.Sprintf("manager version %s is outside tested major range %d-%d", status.ManagerVersion, status.TestedVersions.MinimumMajor, status.TestedVersions.MaximumMajor))
	}
	return status
}

func listOutdatedScope(ctx context.Context, pm, executable string, scope Scope, options ManagerOptions, packages Packages) error {
	adapter, err := adapterFor(pm)
	if err != nil {
		return err
	}
	return adapter.listOutdatedScope(ctx, executable, scope, options, packages)
}

func toolScope(tool Tool) Scope {
	if scoped, ok := tool.(scopedTool); ok {
		return scoped.scope
	}
	return Scope{}
}

func toolDependencyClass(tool Tool) string {
	if scoped, ok := tool.(scopedTool); ok {
		return scoped.dependencyClass
	}
	return ""
}

func Filter(packages Packages, keep func(Package) bool) Packages {
	filtered := make(Packages)
	for name, tools := range packages {
		for _, tool := range tools {
			scope := toolScope(tool)
			class := toolDependencyClass(tool)
			record := Package{Manager: tool.GetPackageManager(), Name: tool.GetName(), Current: tool.GetCurrentVersion(), Latest: tool.GetLatestVersion(), Requested: tool.GetRequestedVersion(), Role: packageRole(tool.GetPackageManager(), class), DependencyClass: class, Manifest: packageManifest(tool.GetPackageManager(), scope), Config: packageConfig(tool.GetPackageManager(), scope), Executable: tool.GetExecutable(), Scope: scope.Kind, Path: scope.Path}
			if keep(record) {
				filtered[name] = append(filtered[name], tool)
			}
		}
	}
	return filtered
}

func packageManifest(manager string, scope Scope) string {
	if manager == PM_NPM && (scope.Kind == "project" || scope.Kind == "workspace") {
		return filepath.Join(scope.Path, "package.json")
	}
	return ""
}

func packageConfig(manager string, scope Scope) string {
	if manager == PM_MISE && (scope.Kind == "project" || scope.Kind == "workspace") {
		return miseConfigPath(scope.Path)
	}
	return ""
}

func packageRole(manager, dependencyClass string) PackageRole {
	if manager == PM_NPM && dependencyClass == "development" {
		return RoleDevelopmentTool
	}
	switch manager {
	case PM_MISE:
		return RoleRuntime
	case PM_NPM:
		return RoleLibrary
	case PM_BREW:
		return RoleLibrary
	default:
		return RoleLibrary
	}
}

func MergePackages(target, source Packages) {
	for name, tools := range source {
		target[name] = append(target[name], tools...)
	}
}

func Records(packages Packages) []Package {
	records := make([]Package, 0)
	for _, tools := range packages {
		for _, tool := range tools {
			records = append(records, Package{
				Manager:         tool.GetPackageManager(),
				Name:            tool.GetName(),
				Current:         tool.GetCurrentVersion(),
				Latest:          tool.GetLatestVersion(),
				Requested:       tool.GetRequestedVersion(),
				Role:            packageRole(tool.GetPackageManager(), toolDependencyClass(tool)),
				DependencyClass: toolDependencyClass(tool),
				Manifest:        packageManifest(tool.GetPackageManager(), toolScope(tool)),
				Config:          packageConfig(tool.GetPackageManager(), toolScope(tool)),
				Executable:      tool.GetExecutable(),
				Scope:           toolScope(tool).Kind,
				Path:            toolScope(tool).Path,
			})
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Manager != records[j].Manager {
			return managerIndex(records[i].Manager) < managerIndex(records[j].Manager)
		}
		if records[i].Name != records[j].Name {
			return records[i].Name < records[j].Name
		}
		return records[i].Executable < records[j].Executable
	})
	return records
}

func PlanUpdates(result QueryResult, include, exclude []string) ([]UpdatePlan, error) {
	records := Records(result.Packages)
	included, err := resolveFilters(records, include)
	if err != nil {
		return nil, fmt.Errorf("include: %w", err)
	}
	excluded, err := resolveFilters(records, exclude)
	if err != nil {
		return nil, fmt.Errorf("exclude: %w", err)
	}

	selected := make([]Package, 0, len(records))
	for _, record := range records {
		key := record.Manager + ":" + record.Name
		if len(include) > 0 {
			if _, ok := included[key]; !ok {
				continue
			}
		}
		if _, ok := excluded[key]; ok {
			continue
		}
		selected = append(selected, record)
	}

	type planKey struct{ manager, executable, scope, directory, packageName string }
	plansByKey := make(map[planKey]*UpdatePlan)
	var keys []planKey
	for _, record := range selected {
		key := planKey{manager: record.Manager, executable: record.Executable, scope: record.Scope, directory: record.Path}
		if record.Manager == PM_GO {
			key.packageName = record.Name
		}
		plan := plansByKey[key]
		if plan == nil {
			plan = &UpdatePlan{Manager: record.Manager, Executable: record.Executable, Scope: record.Scope, Directory: record.Path, Confidence: PlanConfidenceEstimated}
			plansByKey[key] = plan
			keys = append(keys, key)
		}
		plan.Packages = append(plan.Packages, record)
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].manager != keys[j].manager {
			return managerIndex(keys[i].manager) < managerIndex(keys[j].manager)
		}
		return keys[i].executable < keys[j].executable
	})

	plans := make([]UpdatePlan, 0, len(keys))
	for _, key := range keys {
		plan := plansByKey[key]
		sort.Slice(plan.Packages, func(i, j int) bool { return plan.Packages[i].Name < plan.Packages[j].Name })
		plan.Args = updateArgs(plan.Manager, plan.Packages)
		adapter, err := adapterFor(plan.Manager)
		if err != nil {
			return nil, err
		}
		scope := Scope{Kind: plan.Scope, Path: plan.Directory}
		plan.Impact = adapter.updateImpact(scope)
		for _, args := range adapter.recoveryArgs(scope, plan.Packages) {
			plan.Recovery = append(plan.Recovery, RecoveryCommand{Executable: plan.Executable, Args: args})
		}
		plan.StateHash = updatePlanHash(*plan)
		plans = append(plans, *plan)
	}
	return plans, nil
}

func updatePlanHash(plan UpdatePlan) string {
	hash := sha256.New()
	writeHashValue := func(value string) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, value := range []string{plan.Manager, plan.Executable, plan.Directory, plan.Scope, string(plan.Confidence),
		strconv.FormatBool(plan.Impact.RequiresAdmin), strconv.FormatBool(plan.Impact.MayRestartServices), strconv.FormatBool(plan.Impact.MayReboot), strconv.FormatBool(plan.Impact.Interactive)} {
		writeHashValue(value)
	}
	for _, arg := range plan.Args {
		writeHashValue(arg)
	}
	for _, command := range plan.Recovery {
		writeHashValue(command.Executable)
		for _, arg := range command.Args {
			writeHashValue(arg)
		}
	}
	for _, pkg := range plan.Packages {
		for _, value := range []string{pkg.Manager, pkg.Name, pkg.Current, pkg.Latest, pkg.Requested, pkg.Executable, pkg.Scope, pkg.Path} {
			writeHashValue(value)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func resolveFilters(records []Package, filters []string) (map[string]struct{}, error) {
	resolved := make(map[string]struct{}, len(filters))
	for _, filter := range filters {
		if filter == "" {
			return nil, errors.New("empty package filter")
		}

		if manager, name, ok := strings.Cut(filter, ":"); ok {
			if err := Supported([]string{manager}); err != nil {
				return nil, err
			}
			if name == "" || !hasPackage(records, manager, name) {
				return nil, fmt.Errorf("package %q is not outdated", filter)
			}
			resolved[manager+":"+name] = struct{}{}
			continue
		}

		managers := make(map[string]struct{})
		for _, record := range records {
			if record.Name == filter {
				managers[record.Manager] = struct{}{}
			}
		}
		if len(managers) == 0 {
			return nil, fmt.Errorf("package %q is not outdated", filter)
		}
		if len(managers) > 1 {
			var names []string
			for manager := range managers {
				names = append(names, manager+":"+filter)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("package %q is ambiguous; use %s", filter, strings.Join(names, " or "))
		}
		for manager := range managers {
			resolved[manager+":"+filter] = struct{}{}
		}
	}
	return resolved, nil
}

func hasPackage(records []Package, manager, name string) bool {
	for _, record := range records {
		if record.Manager == manager && record.Name == name {
			return true
		}
	}
	return false
}

func updateArgs(manager string, packages []Package) []string {
	adapter, err := adapterFor(manager)
	if err != nil {
		return nil
	}
	return adapter.updateArgs(packages)
}

type UpdateOutcome struct {
	StateHash    string   `json:"state_hash"`
	Status       string   `json:"status"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// ValidateProjectMutations checks manifest permissions and Git safety before project updates.
func ValidateProjectMutations(plans []UpdatePlan, profile config.Profile, allowDirty, allowUnversioned bool) error {
	checked := map[string]struct{}{}
	for _, plan := range plans {
		if plan.Scope != "project" || plan.Directory == "" {
			continue
		}
		if _, exists := checked[plan.Directory]; exists {
			continue
		}
		checked[plan.Directory] = struct{}{}
		manifestChangesAllowed := false
		for _, project := range profile.Projects {
			if (project.ResolvedPath == plan.Directory || project.Path == plan.Directory) && project.AllowManifestChanges {
				manifestChangesAllowed = true
				break
			}
		}
		if !manifestChangesAllowed {
			return fmt.Errorf("project %q does not allow manifest changes", plan.Directory)
		}
		command := exec.Command("git", "-C", plan.Directory, "rev-parse", "--show-toplevel")
		root, err := command.Output()
		if err != nil {
			if allowUnversioned {
				continue
			}
			return fmt.Errorf("project %q is not in a Git repository; use --allow-unversioned", plan.Directory)
		}
		status, err := exec.Command("git", "-C", strings.TrimSpace(string(root)), "status", "--porcelain").Output()
		if err != nil {
			return fmt.Errorf("inspect Git status for %q: %w", plan.Directory, err)
		}
		if len(status) > 0 && !allowDirty {
			return fmt.Errorf("project %q has uncommitted changes; use --allow-dirty", plan.Directory)
		}
	}
	return nil
}

func ApplyUpdates(ctx context.Context, plans []UpdatePlan, stdin io.Reader, stdout, stderr io.Writer) error {
	_, err := ApplyUpdatesWithOutcomes(ctx, plans, false, stdin, stdout, stderr)
	return err
}

func ApplyUpdatesWithOutcomes(ctx context.Context, plans []UpdatePlan, bestEffort bool, stdin io.Reader, stdout, stderr io.Writer) ([]UpdateOutcome, error) {
	outcomes := make([]UpdateOutcome, 0, len(plans))
	failedManagers := make(map[string]error)
	var updateErrors []error
	for i, plan := range plans {
		if previousErr := failedManagers[plan.Manager]; previousErr != nil {
			outcomes = append(outcomes, UpdateOutcome{StateHash: plan.StateHash, Status: "skipped", Error: previousErr.Error()})
			continue
		}
		if err := ctx.Err(); err != nil {
			classified := classifyOperationError(ctx, err, "")
			updateErrors = append(updateErrors, classified)
			for _, skipped := range plans[i:] {
				outcomes = append(outcomes, UpdateOutcome{StateHash: skipped.StateHash, Status: "skipped", Error: classified.Error()})
			}
			break
		}
		beforeFiles := snapshotProjectFiles(plan)
		planContext, cancel := context.WithTimeout(ctx, mutationTimeout)
		cmd := commandContext(planContext, plan.Executable, plan.Args...)
		cmd.Dir = plan.Directory
		cmd.Env = managerEnvironment(plan.Directory)
		cmd.Stdin = stdin
		var standardOutput, errorOutput bytes.Buffer
		cmd.Stdout = &standardOutput
		if stdout != nil {
			cmd.Stdout = io.MultiWriter(stdout, &standardOutput)
		}
		cmd.Stderr = &errorOutput
		if stderr != nil {
			cmd.Stderr = io.MultiWriter(stderr, &errorOutput)
		}
		err := cmd.Run()
		var classified error
		if err != nil {
			message := strings.TrimSpace(errorOutput.String() + "\n" + standardOutput.String())
			if plan.Manager == PM_HOMEBREW && strings.Contains(message, "It seems there is already an App at '") {
				// ponytail: explain this conflict only. Repair needs an explicit, safe backup plan.
				err = fmt.Errorf("Homebrew app conflict.\nInspect both app copies before repair.\nConficius did not attempt automatic repair.\nHomebrew may have changed files before the failure.\n%s\n%w", strings.TrimSpace(message), err)
			} else if message != "" {
				err = fmt.Errorf("%s\n%w", message, err)
			}
			classified = classifyOperationError(planContext, err, message)
		}
		cancel()
		if err != nil {
			wrapped := fmt.Errorf("%s (%s): %w", plan.Manager, plan.Executable, classified)
			updateErrors = append(updateErrors, wrapped)
			failedManagers[plan.Manager] = wrapped
			outcomes = append(outcomes, UpdateOutcome{StateHash: plan.StateHash, Status: "failed", Error: redactSensitive(wrapped.Error())})
			if ctx.Err() != nil || !bestEffort {
				for _, skipped := range plans[i+1:] {
					outcomes = append(outcomes, UpdateOutcome{StateHash: skipped.StateHash, Status: "skipped", Error: classified.Error()})
				}
				break
			}
			continue
		}
		outcomes = append(outcomes, UpdateOutcome{StateHash: plan.StateHash, Status: "completed", ChangedFiles: changedProjectFiles(plan, beforeFiles)})
	}
	return outcomes, errors.Join(updateErrors...)
}

func snapshotProjectFiles(plan UpdatePlan) map[string][]byte {
	if plan.Scope != "project" && plan.Scope != "workspace" {
		return nil
	}
	adapter, err := adapterFor(plan.Manager)
	if err != nil {
		return nil
	}
	files := make(map[string][]byte)
	for _, name := range adapter.projectFiles() {
		contents, err := os.ReadFile(filepath.Join(plan.Directory, name))
		if err == nil {
			files[name] = contents
		}
	}
	return files
}

func changedProjectFiles(plan UpdatePlan, before map[string][]byte) []string {
	if before == nil {
		return nil
	}
	adapter, err := adapterFor(plan.Manager)
	if err != nil {
		return nil
	}
	var changed []string
	for _, name := range adapter.projectFiles() {
		contents, err := os.ReadFile(filepath.Join(plan.Directory, name))
		previous, existed := before[name]
		if err == nil && (!existed || !bytes.Equal(previous, contents)) {
			changed = append(changed, name)
		} else if err != nil && existed {
			changed = append(changed, name)
		}
	}
	return changed
}

func VerifyUpdateOutcomes(ctx context.Context, plans []UpdatePlan, outcomes []UpdateOutcome, options QueryOptions) ([]UpdateOutcome, error) {
	plansByHash := make(map[string]UpdatePlan, len(plans))
	for _, plan := range plans {
		plansByHash[plan.StateHash] = plan
	}
	var verificationErrors []error
	for i := range outcomes {
		outcome := &outcomes[i]
		if outcome.Status != "completed" {
			continue
		}
		plan, ok := plansByHash[outcome.StateHash]
		if !ok {
			continue
		}
		var err error
		if plan.Manager == PM_GO {
			versions := make(map[string]string, len(plan.Packages))
			for _, pkg := range plan.Packages {
				versions[pkg.Name] = pkg.Latest
			}
			path, pathErr := GoToolJournalPath()
			if pathErr != nil {
				err = pathErr
			} else {
				err = UpdateGoToolJournalVersions(path, versions)
			}
		}
		remaining := make(Packages)
		if err == nil {
			err = listOutdatedScope(ctx, plan.Manager, plan.Executable, Scope{Kind: plan.Scope, Path: plan.Directory}, options.Managers[plan.Manager], remaining)
		}
		if err == nil {
			for _, pkg := range plan.Packages {
				if hasPackage(Records(remaining), pkg.Manager, pkg.Name) {
					err = fmt.Errorf("%s:%s remains outdated", pkg.Manager, pkg.Name)
					break
				}
			}
		}
		if err != nil {
			outcome.Status = "verification-failed"
			outcome.Error = redactSensitive(err.Error())
			verificationErrors = append(verificationErrors, fmt.Errorf("verify plan %s: %w", plan.StateHash, err))
		}
	}
	return outcomes, errors.Join(verificationErrors...)
}

func runOutput(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return runOutputDir(ctx, "", executable, args...)
}

func runOutputDir(ctx context.Context, dir, executable string, args ...string) ([]byte, error) {
	cmd := commandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Env = managerEnvironment(dir)
	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}

	var stderr string
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		stderr = strings.TrimSpace(string(exitErr.Stderr))
		err = fmt.Errorf("%w: %s", err, stderr)
	}
	return output, classifyOperationError(ctx, err, stderr)
}

func managerEnvironment(dir string) []string {
	environment := os.Environ()
	overrides := map[string]string{
		"NO_COLOR": "1",
		"CLICOLOR": "0",
		"LC_ALL":   "C",
	}
	result := environment[:0]
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[name]; !overridden {
			result = append(result, entry)
		}
	}
	for _, name := range []string{"NO_COLOR", "CLICOLOR", "LC_ALL"} {
		result = append(result, name+"="+overrides[name])
	}
	if dir != "" {
		result = append(result, "PWD="+dir)
	}
	return result
}

func managerIndex(manager string) int {
	for i, candidate := range RegisteredPackageManagers {
		if candidate == manager {
			return i
		}
	}
	return len(RegisteredPackageManagers)
}

func LookPathAll(file string) ([]string, error) {
	if strings.ContainsAny(file, `/\`) {
		if isExecutable(file) {
			return []string{file}, nil
		}
		return nil, exec.ErrNotFound
	}

	var exts []string
	if runtime.GOOS == "windows" {
		exts = pathExts()
	} else {
		exts = []string{""}
	}

	seen := make(map[string]bool)
	var matches []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			candidate := filepath.Join(dir, file)
			if ext != "" && !strings.EqualFold(filepath.Ext(candidate), ext) {
				candidate += ext
			}
			abs, err := filepath.Abs(candidate)
			if err != nil || seen[abs] || !isExecutable(abs) {
				continue
			}
			seen[abs] = true
			matches = append(matches, abs)
		}
	}

	if len(matches) == 0 {
		return nil, exec.ErrNotFound
	}
	return matches, nil
}

func pathExts() []string {
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}

	var exts []string
	for _, ext := range filepath.SplitList(pathext) {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		exts = append(exts, strings.ToLower(ext))
	}
	return exts
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return hasWindowsExecutableExt(path)
	}
	return info.Mode().Perm()&0111 != 0
}

func hasWindowsExecutableExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range pathExts() {
		if ext == strings.ToLower(allowed) {
			return true
		}
	}
	return false
}
