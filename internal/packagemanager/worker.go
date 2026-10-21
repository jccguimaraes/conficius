package packagemanager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jccguimaraes/conficius/internal/config"
)

const workerFlag = "--internal-manager-operation"
const workerRequestLimit = 48 * 1024
const workerResponseLimit = 16 * 1024 * 1024

type workerRequest struct {
	Operation string
	Manager   string
	Method    string
	User      string
	Scopes    []Scope
	Options   QueryOptions
	Plan      *UpdatePlan
}
type workerResponse struct {
	Managers  []ManagerStatus
	Records   []Package
	Inventory InstalledResult
	Integrity []ManagerIntegrity
	Outcomes  []UpdateOutcome
	Error     string
}

// recordTool preserves worker records without serializing interface implementations.
type recordTool struct{ Package }

func (t recordTool) GetPackageManager() string   { return t.Manager }
func (t recordTool) GetName() string             { return t.Name }
func (t recordTool) GetCurrentVersion() string   { return t.Current }
func (t recordTool) GetLatestVersion() string    { return t.Latest }
func (t recordTool) GetRequestedVersion() string { return t.Requested }
func (t recordTool) GetExecutable() string       { return t.Executable }

func selectedManagers(pms []string) []string {
	if len(pms) == 0 {
		return RegisteredPackageManagers
	}
	return pms
}
func hasRunAs(pms []string, options QueryOptions) bool {
	for _, pm := range selectedManagers(pms) {
		if options.RunAs[pm] != "" && !options.Disabled[pm] {
			return true
		}
	}
	return false
}

// HasRunAs reports whether enabled managers require a foreground terminal handoff.
func HasRunAs(options QueryOptions) bool { return hasRunAs(nil, options) }

func managerWorkerOptions(options QueryOptions, manager string) QueryOptions {
	options.RunAs = nil
	options.RunAsMethod = nil
	options.Executables = nil // Resolve only inside target login environment.
	options.Required = map[string]bool{manager: options.Required[manager]}
	return options
}
func workerRecords(records []Package, user string, method ...string) Packages {
	packages := make(Packages)
	for _, record := range records {
		record.RunAs = user
		if len(method) > 0 {
			record.RunAsMethod = method[0]
		}
		packages[record.Name] = append(packages[record.Name], recordTool{record})
	}
	return packages
}
func queryRunAsOutdated(ctx context.Context, pms []string, scopes []Scope, options QueryOptions) (QueryResult, error) {
	result := QueryResult{Packages: make(Packages)}
	if err := Supported(pms); err != nil {
		return result, err
	}
	if err := validateRequiredSelection(pms, options); err != nil {
		return result, err
	}
	var errs []error
	for _, pm := range selectedManagers(pms) {
		var part QueryResult
		var err error
		if target := options.RunAs[pm]; target != "" && !options.Disabled[pm] {
			var response workerResponse
			response, err = callManagerWorker(ctx, workerRequest{Operation: "outdated", Manager: pm, User: target, Method: options.RunAsMethod[pm], Scopes: scopes, Options: managerWorkerOptions(options, pm)})
			part = QueryResult{Managers: response.Managers, Packages: workerRecords(response.Records, target, options.RunAsMethod[pm])}
		} else {
			part, err = QueryOutdatedScopesWithOptions(ctx, []string{pm}, scopes, singleManagerOptions(options, pm))
		}
		if err != nil && len(part.Managers) == 0 {
			part.Managers = []ManagerStatus{{Name: pm, RunAs: options.RunAs[pm], RunAsMethod: options.RunAsMethod[pm], Status: StatusError, Error: redactSensitive(err.Error())}}
		}
		result.Managers = append(result.Managers, part.Managers...)
		MergePackages(result.Packages, part.Packages)
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}
func validateRequiredSelection(pms []string, options QueryOptions) error {
	selected := make(map[string]bool)
	for _, pm := range selectedManagers(pms) {
		selected[pm] = true
	}
	for pm, required := range options.Required {
		if required && !selected[pm] {
			return fmt.Errorf("required manager %s is excluded by manager filter", pm)
		}
	}
	return nil
}

func singleManagerOptions(options QueryOptions, manager string) QueryOptions {
	options.RunAs = nil
	options.RunAsMethod = nil
	options.Required = map[string]bool{manager: options.Required[manager]}
	return options
}
func queryRunAsInstalled(ctx context.Context, pms []string, scopes []Scope, options QueryOptions) (InstalledResult, error) {
	var result InstalledResult
	var errs []error
	if err := Supported(pms); err != nil {
		return result, err
	}
	if err := validateRequiredSelection(pms, options); err != nil {
		return result, err
	}
	for _, pm := range selectedManagers(pms) {
		var part InstalledResult
		var err error
		if target := options.RunAs[pm]; target != "" && !options.Disabled[pm] {
			var response workerResponse
			response, err = callManagerWorker(ctx, workerRequest{Operation: "inventory", Manager: pm, User: target, Method: options.RunAsMethod[pm], Scopes: scopes, Options: managerWorkerOptions(options, pm)})
			part = response.Inventory
		} else {
			part, err = QueryInstalledScopesWithOptions(ctx, []string{pm}, scopes, singleManagerOptions(options, pm))
		}
		if err != nil && len(part.Managers) == 0 {
			part.Managers = []ManagerStatus{{Name: pm, RunAs: options.RunAs[pm], RunAsMethod: options.RunAsMethod[pm], Status: StatusError, Error: redactSensitive(err.Error())}}
		}
		result.Managers = append(result.Managers, part.Managers...)
		result.Packages = append(result.Packages, part.Packages...)
		result.Commands = append(result.Commands, part.Commands...)
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}
func checkRunAsIntegrity(pms []string, options QueryOptions) ([]ManagerIntegrity, error) {
	var result []ManagerIntegrity
	var errs []error
	if err := Supported(pms); err != nil {
		return nil, err
	}
	if err := validateRequiredSelection(pms, options); err != nil {
		return nil, err
	}
	for _, pm := range selectedManagers(pms) {
		var checks []ManagerIntegrity
		var err error
		if target := options.RunAs[pm]; target != "" && !options.Disabled[pm] {
			var response workerResponse
			response, err = callManagerWorker(context.Background(), workerRequest{Operation: "doctor", Manager: pm, User: target, Method: options.RunAsMethod[pm], Options: managerWorkerOptions(options, pm)})
			checks = response.Integrity
		} else {
			checks, err = CheckManagerIntegrity([]string{pm}, singleManagerOptions(options, pm))
		}
		result = append(result, checks...)
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}

// Worker cache is populated before glyph starts. In cache-only mode no goroutine
// can launch a worker; a missing entry fails closed until the next terminal handoff.
var workerCache = struct {
	sync.Mutex
	only      bool
	preparing bool
	values    map[string]workerResponse
}{values: make(map[string]workerResponse)}

// PrepareRunAsQueries runs confirmed foreground queries before starting the TUI.
func PrepareRunAsQueries(ctx context.Context, scopes []Scope, options QueryOptions) error {
	workerCache.Lock()
	workerCache.only = false
	workerCache.preparing = true
	clear(workerCache.values)
	workerCache.Unlock()
	var errs []error
	for _, pm := range RegisteredPackageManagers {
		if options.RunAs[pm] == "" || options.Disabled[pm] {
			continue
		}
		for _, operation := range []string{"inventory", "outdated"} {
			request := workerRequest{Operation: operation, Manager: pm, User: options.RunAs[pm], Method: options.RunAsMethod[pm], Scopes: scopes, Options: managerWorkerOptions(options, pm)}
			response, err := callManagerWorker(ctx, request)
			if err != nil {
				response.Error = err.Error()
				raw, _ := json.Marshal(request)
				workerCache.Lock()
				workerCache.values[string(raw)] = response
				workerCache.Unlock()
			}
			errs = append(errs, err)
		}
	}
	workerCache.Lock()
	workerCache.only = true
	workerCache.preparing = false
	workerCache.Unlock()
	return errors.Join(errs...)
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > workerResponseLimit-b.Len() {
		return 0, errors.New("manager worker response exceeds 16 MiB")
	}
	return b.Buffer.Write(p)
}

var workerTerminal *os.File // Set only in single-use worker process before queries start.
var workerCommand = exec.CommandContext
var workerTTY = func() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

func effectiveWorkerMethod(method string) string {
	if method == "" {
		return "sudo"
	}
	return method
}

func validateWorkerMethod(method string) error {
	switch method {
	case "", "sudo", "su":
		return nil
	default:
		return fmt.Errorf("unsupported run_as method %q", method)
	}
}

func managerWorkerCommand(goos, method, user, executable, encoded string) (string, []string, error) {
	if err := validateWorkerMethod(method); err != nil {
		return "", nil, err
	}
	if effectiveWorkerMethod(method) == "sudo" {
		return "/usr/bin/sudo", []string{"-i", "-u", user, "--", executable, workerFlag, encoded}, nil
	}
	switch goos {
	case "linux", "darwin":
		if strings.ContainsAny(executable, "!\x00\r\n") {
			return "", nil, errors.New("su requires a Conficius executable path without exclamation marks or control characters")
		}
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
		command := "exec " + quote(executable) + " " + quote(workerFlag) + " " + quote(encoded)
		return "/usr/bin/su", []string{"-l", user, "-c", command}, nil
	default:
		return "", nil, fmt.Errorf("su run_as method is unsupported on %s", goos)
	}
}

func callManagerWorker(ctx context.Context, request workerRequest) (workerResponse, error) {
	var response workerResponse
	raw, err := json.Marshal(request)
	if err != nil {
		return response, err
	}
	if len(raw) > workerRequestLimit {
		return response, errors.New("manager worker request exceeds 48 KiB")
	}
	if err := validateWorkerRequest(request); err != nil {
		return response, err
	}
	key := string(raw)
	workerCache.Lock()
	cached, ok := workerCache.values[key]
	only := workerCache.only
	preparing := workerCache.preparing
	workerCache.Unlock()
	if request.Operation != "apply" {
		if ok && only {
			return cached, workerResponseError(cached)
		}
		if only {
			return response, errors.New("run_as query requires stopped TUI and confirmed terminal handoff; refresh session")
		}
	}
	tty, err := workerTTY()
	if err != nil {
		return response, fmt.Errorf("run_as requires controlling /dev/tty: %w", err)
	}
	defer func() { _ = tty.Close() }()
	if err := confirmWorker(tty, tty, request); err != nil {
		return response, err
	}
	executable, err := os.Executable()
	if err != nil {
		return response, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	name, args, err := managerWorkerCommand(runtime.GOOS, request.Method, request.User, executable, base64.RawURLEncoding.EncodeToString(raw))
	if err != nil {
		return response, err
	}
	cmd := workerCommand(ctx, name, args...)
	cmd.Stdin, cmd.Stderr = tty, tty
	var output boundedBuffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return response, fmt.Errorf("%s as %s worker: %w", request.Manager, request.User, err)
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, fmt.Errorf("invalid manager worker response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return workerResponse{}, errors.New("trailing manager worker output")
	}
	for i := range response.Managers {
		response.Managers[i].RunAs = request.User
		response.Managers[i].RunAsMethod = request.Method
	}
	for i := range response.Inventory.Managers {
		response.Inventory.Managers[i].RunAs = request.User
		response.Inventory.Managers[i].RunAsMethod = request.Method
	}
	if request.Operation != "apply" && preparing {
		workerCache.Lock()
		workerCache.values[key] = response
		workerCache.Unlock()
	}
	return response, workerResponseError(response)
}
func managerCommandOutput(cmd *exec.Cmd) ([]byte, error) {
	if workerTerminal == nil {
		return cmd.Output()
	}
	cmd.Stdin, cmd.Stderr = workerTerminal, workerTerminal
	var output boundedBuffer
	cmd.Stdout = &output
	err := cmd.Run()
	return output.Bytes(), err
}

func workerResponseError(response workerResponse) error {
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}
func confirmWorker(input io.Reader, output io.Writer, request workerRequest) error {
	if _, err := fmt.Fprintf(output, "\nTUI stopped / foreground terminal. Run %s %s as %s using %s login environment? Type yes: ", request.Manager, request.Operation, request.User, effectiveWorkerMethod(request.Method)); err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(input, 128)).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "yes" {
		return errors.New("run_as terminal handoff declined")
	}
	return nil
}
func validateWorkerRequest(request workerRequest) error {
	if err := validateWorkerMethod(request.Method); err != nil {
		return err
	}
	if !config.ValidRunAsUser(request.User) {
		return errors.New("invalid worker target user")
	}
	if err := Supported([]string{request.Manager}); err != nil {
		return err
	}
	switch request.Operation {
	case "inventory", "outdated", "doctor":
		if request.Plan != nil {
			return errors.New("query cannot contain update plan")
		}
	case "apply":
		if request.Plan == nil || request.Plan.Manager != request.Manager || request.Plan.RunAs != request.User {
			return errors.New("invalid target-user update plan")
		}
		if err := validateWorkerMethod(request.Plan.RunAsMethod); err != nil {
			return err
		}
		if effectiveWorkerMethod(request.Method) != effectiveWorkerMethod(request.Plan.RunAsMethod) {
			return errors.New("worker method does not match update plan")
		}
	default:
		return errors.New("unsupported manager worker operation")
	}
	if len(request.Scopes) > 64 {
		return errors.New("too many worker scopes")
	}
	if len(request.Options.RunAs) > 0 || len(request.Options.RunAsMethod) > 0 || len(request.Options.Executables) > 0 {
		return errors.New("worker cannot delegate or accept executable overrides")
	}
	// ponytail: global target-user managers only. Project mutation needs target-side
	// repository-policy validation before extending this boundary.
	if request.Operation != "doctor" && request.Operation != "apply" && len(request.Scopes) == 0 {
		return errors.New("worker requires explicit global scopes")
	}
	for _, scope := range request.Scopes {
		if (scope.Kind != "global" && scope.Kind != "system" && scope.Kind != "user" && scope.Kind != "global-config") || scope.Path != "" {
			return errors.New("run_as currently supports global scope only; select --global")
		}
	}
	if request.Plan != nil && (request.Plan.Scope != "global" || request.Plan.Directory != "") {
		return errors.New("run_as updates require global scope")
	}
	return nil
}

// HandleManagerWorker must run before normal argument parsing in both binaries.
// The protocol is internal, version-coupled, bounded, and never accepts commands.
func HandleManagerWorker(args []string, stdout io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != workerFlag {
		return false, nil
	}
	if len(args) != 2 || len(args[1]) > workerRequestLimit*2 {
		return true, errors.New("invalid manager worker arguments")
	}
	raw, err := base64.RawURLEncoding.DecodeString(args[1])
	if err != nil || len(raw) > workerRequestLimit {
		return true, errors.New("invalid manager worker request encoding")
	}
	var request workerRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return true, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return true, errors.New("trailing worker request")
	}
	if err := validateWorkerRequest(request); err != nil {
		return true, err
	}
	account, err := user.Lookup(request.User)
	if err != nil {
		return true, err
	}
	if account.Uid != strconv.Itoa(os.Geteuid()) {
		return true, errors.New("manager worker effective user does not match requested user")
	}
	// Login environment must belong to the selected account, including HOME.
	if os.Getenv("HOME") != account.HomeDir {
		return true, errors.New("manager worker HOME does not match target account")
	}
	tty, err := workerTTY()
	if err != nil {
		return true, fmt.Errorf("worker requires controlling terminal: %w", err)
	}
	defer func() { _ = tty.Close(); workerTerminal = nil }()
	workerTerminal = tty
	response := executeManagerWorker(context.Background(), request)
	raw, err = json.Marshal(response)
	if err != nil {
		return true, err
	}
	if len(raw) > workerResponseLimit {
		return true, errors.New("manager worker response exceeds 16 MiB")
	}
	_, err = stdout.Write(raw)
	return true, err
}
func executeManagerWorker(ctx context.Context, request workerRequest) workerResponse {
	var response workerResponse
	var err error
	switch request.Operation {
	case "inventory":
		response.Inventory, err = QueryInstalledScopesWithOptions(ctx, []string{request.Manager}, request.Scopes, request.Options)
	case "outdated":
		var result QueryResult
		result, err = QueryOutdatedScopesWithOptions(ctx, []string{request.Manager}, request.Scopes, request.Options)
		response.Managers, response.Records = result.Managers, Records(result.Packages)
	case "doctor":
		response.Integrity, err = CheckManagerIntegrity([]string{request.Manager}, request.Options)
	case "apply":
		response.Outcomes, err = applyWorkerPlan(ctx, *request.Plan)
	}
	if err != nil {
		response.Error = redactSensitive(err.Error())
	}
	return response
}
func applyWorkerPlan(ctx context.Context, expected UpdatePlan) ([]UpdateOutcome, error) {
	if expected.StateHash != updatePlanHash(expected) {
		return nil, errors.New("target-user update plan was altered")
	}
	for _, pkg := range expected.Packages {
		if pkg.Name == "" || strings.HasPrefix(pkg.Name, "-") || strings.ContainsAny(pkg.Name, "\x00\r\n") {
			return nil, errors.New("invalid update package name")
		}
	}
	// Re-query and rebuild command locally. Never execute supplied Args/Executable.
	result, err := QueryOutdatedScopes(ctx, []string{expected.Manager}, []Scope{{Kind: "global"}})
	if err != nil {
		return nil, err
	}
	var include []string
	for _, pkg := range expected.Packages {
		include = append(include, pkg.Manager+":"+pkg.Name)
	}
	if len(include) == 0 || len(include) > 1024 {
		return nil, errors.New("invalid update selection")
	}
	plans, err := PlanUpdates(result, include, nil)
	if err != nil {
		return nil, err
	}
	var selected *UpdatePlan
	for i := range plans {
		plans[i].RunAs = expected.RunAs
		plans[i].RunAsMethod = expected.RunAsMethod
		for j := range plans[i].Packages {
			plans[i].Packages[j].RunAs = expected.RunAs
			plans[i].Packages[j].RunAsMethod = expected.RunAsMethod
		}
		plans[i].StateHash = updatePlanHash(plans[i])
		if plans[i].StateHash == expected.StateHash {
			copyPlan := plans[i]
			selected = &copyPlan
		}
	}
	if selected == nil {
		return nil, errors.New("target-user update plan is stale or altered; refresh preview")
	}
	tty, err := workerTTY()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tty.Close() }()
	originalHash := selected.StateHash
	selected.RunAs = ""
	selected.RunAsMethod = ""
	outcomes, applyErr := ApplyUpdatesWithOutcomes(ctx, []UpdatePlan{*selected}, false, tty, tty, tty)
	outcomes, verifyErr := VerifyUpdateOutcomes(ctx, []UpdatePlan{*selected}, outcomes, QueryOptions{})
	for i := range outcomes {
		outcomes[i].StateHash = originalHash
	}
	return outcomes, errors.Join(applyErr, verifyErr)
}
