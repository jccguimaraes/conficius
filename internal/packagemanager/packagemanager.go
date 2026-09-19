package packagemanager

import (
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
	scope Scope
}

type ManagerStatus struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	AdapterVersion string            `json:"adapter_version"`
	ManagerVersion string            `json:"manager_version,omitempty"`
	Capabilities   []Capability      `json:"capabilities,omitempty"`
	Executables    []string          `json:"executables,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
	Operations     []OperationStatus `json:"operations,omitempty"`
	Error          string            `json:"error,omitempty"`
}

type ManagerOptions struct {
	IncludeBumpCandidates bool
	IncludeGreedy         bool
	ExpectedGlobalPrefix  string
}

type QueryOptions struct {
	Managers    map[string]ManagerOptions
	Executables map[string]string
	Disabled    map[string]bool
}

func QueryOptionsFromProfile(profile config.Profile) QueryOptions {
	options := QueryOptions{
		Managers:    make(map[string]ManagerOptions, len(profile.Managers)),
		Executables: make(map[string]string),
		Disabled:    make(map[string]bool),
	}
	for manager, settings := range profile.Managers {
		if settings.Enabled != nil && !*settings.Enabled {
			options.Disabled[manager] = true
		}
		if settings.Executable != "" {
			options.Executables[manager] = settings.Executable
		}
		options.Managers[manager] = ManagerOptions{
			IncludeBumpCandidates: settings.IncludeBumpCandidates,
			IncludeGreedy:         settings.IncludeGreedy,
			ExpectedGlobalPrefix:  settings.ExpectedGlobalPrefix,
		}
	}
	return options
}

type QueryResult struct {
	Managers []ManagerStatus
	Packages Packages
}

type Package struct {
	Manager    string `json:"manager"`
	Name       string `json:"name"`
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Requested  string `json:"requested,omitempty"`
	Executable string `json:"executable"`
	Scope      string `json:"scope,omitempty"`
	Path       string `json:"path,omitempty"`
}

type PlanConfidence string

const (
	PlanConfidenceExact     PlanConfidence = "exact"
	PlanConfidenceEstimated PlanConfidence = "estimated"
	PlanConfidenceUnknown   PlanConfidence = "unknown"
)

type UpdatePlan struct {
	Manager    string         `json:"manager"`
	Executable string         `json:"executable"`
	Directory  string         `json:"directory,omitempty"`
	Scope      string         `json:"scope"`
	Confidence PlanConfidence `json:"confidence"`
	StateHash  string         `json:"state_hash"`
	Impact     MutationImpact `json:"impact"`
	Args       []string       `json:"args"`
	Packages   []Package      `json:"packages"`
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
				Capabilities:   adapter.capabilities(),
				Error:          "executable not found",
			})
			continue
		}
		if err != nil {
			statuses = append(statuses, ManagerStatus{Name: pm, Status: StatusError, AdapterVersion: adapterVersion, Capabilities: adapter.capabilities(), Error: redactSensitive(err.Error())})
			continue
		}

		status := ManagerStatus{Name: pm, Status: StatusAvailable, AdapterVersion: adapterVersion, Capabilities: adapter.capabilities(), Executables: executables}
		output, versionErr := runOutput(context.Background(), executables[0], "--version")
		if versionErr != nil {
			status.Warnings = append(status.Warnings, "read manager version: "+redactSensitive(versionErr.Error()))
		} else {
			status.ManagerVersion = firstVersion(string(output))
			if status.ManagerVersion == "" {
				status.Warnings = append(status.Warnings, "read manager version: no version in output")
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
	var queryErrors []error
	for _, operationResult := range runReadOperations(ctx, operations) {
		status := &result.Managers[operationResult.operation.managerIndex]
		operationStatus := newOperationStatus("outdated", operationResult.operation, operationResult.err)
		status.Operations = append(status.Operations, operationStatus)
		if operationResult.err != nil {
			status.Error = redactSensitive(operationResult.err.Error())
			queryErrors = append(queryErrors, fmt.Errorf("%s (%s, %s): %w", operationResult.operation.manager, operationResult.operation.executable, operationResult.operation.scope.Kind, operationResult.err))
			continue
		}
		for name, tools := range operationResult.value {
			for _, tool := range tools {
				result.Packages[name] = append(result.Packages[name], scopedTool{Tool: tool, scope: operationResult.operation.scope})
			}
		}
	}
	finalizeManagerStatuses(result.Managers)

	return result, errors.Join(queryErrors...)
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
	var enabled []string
	for _, manager := range pms {
		if !options.Disabled[manager] {
			enabled = append(enabled, manager)
		}
	}
	statuses, err := Inspect(enabled)
	if err != nil {
		return nil, err
	}
	for i := range statuses {
		if executable := options.Executables[statuses[i].Name]; executable != "" {
			statuses[i].Status = StatusAvailable
			statuses[i].Executables = []string{executable}
			statuses[i].Error = ""
		}
	}
	return statuses, nil
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

func Filter(packages Packages, keep func(Package) bool) Packages {
	filtered := make(Packages)
	for name, tools := range packages {
		for _, tool := range tools {
			scope := toolScope(tool)
			record := Package{Manager: tool.GetPackageManager(), Name: tool.GetName(), Current: tool.GetCurrentVersion(), Latest: tool.GetLatestVersion(), Requested: tool.GetRequestedVersion(), Executable: tool.GetExecutable(), Scope: scope.Kind, Path: scope.Path}
			if keep(record) {
				filtered[name] = append(filtered[name], tool)
			}
		}
	}
	return filtered
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
				Manager:    tool.GetPackageManager(),
				Name:       tool.GetName(),
				Current:    tool.GetCurrentVersion(),
				Latest:     tool.GetLatestVersion(),
				Requested:  tool.GetRequestedVersion(),
				Executable: tool.GetExecutable(),
				Scope:      toolScope(tool).Kind,
				Path:       toolScope(tool).Path,
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

	type planKey struct{ manager, executable, scope, directory string }
	plansByKey := make(map[planKey]*UpdatePlan)
	var keys []planKey
	for _, record := range selected {
		key := planKey{record.Manager, record.Executable, record.Scope, record.Path}
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
		plan.Impact = adapter.updateImpact(Scope{Kind: plan.Scope, Path: plan.Directory})
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

func ApplyUpdates(ctx context.Context, plans []UpdatePlan, stdin io.Reader, stdout, stderr io.Writer) error {
	var updateErrors []error
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			updateErrors = append(updateErrors, classifyOperationError(ctx, err, ""))
			break
		}
		cmd := commandContext(ctx, plan.Executable, plan.Args...)
		cmd.Dir = plan.Directory
		cmd.Env = managerEnvironment(plan.Directory)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			classified := classifyOperationError(ctx, err, "")
			updateErrors = append(updateErrors, fmt.Errorf("%s (%s): %w", plan.Manager, plan.Executable, classified))
			if ctx.Err() != nil {
				break
			}
		}
	}
	return errors.Join(updateErrors...)
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
