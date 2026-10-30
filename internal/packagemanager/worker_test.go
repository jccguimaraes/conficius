package packagemanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestWorkerValidation(t *testing.T) {
	valid := workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Scopes: []Scope{{Kind: "global"}}}
	if err := validateWorkerRequest(valid); err != nil {
		t.Fatal(err)
	}
	cases := []workerRequest{valid, valid, valid, valid, valid, valid, valid, valid}
	cases[0].User = "-root"
	cases[1].Operation = "exec"
	cases[2].Manager = "sh"
	cases[3].Scopes = []Scope{{Kind: "project", Path: "/tmp"}}
	cases[4].Options.RunAs = map[string]string{PM_NPM: "root"}
	cases[5].Options.Executables = map[string]string{PM_NPM: "/bin/sh"}
	cases[6].Method = "doas"
	cases[7].Options.RunAsMethod = map[string]string{PM_NPM: "su"}
	for i, request := range cases {
		if validateWorkerRequest(request) == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
func TestWorkerConfirmation(t *testing.T) {
	for _, input := range []string{"no\n", "yes", "y\n", strings.Repeat("x", 129) + "yes\n"} {
		if confirmWorker(strings.NewReader(input), io.Discard, workerRequest{}) == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	var output bytes.Buffer
	if err := confirmWorker(strings.NewReader("yes\n"), &output, workerRequest{Manager: PM_NPM, User: "builder", Operation: "inventory"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "as builder") {
		t.Fatal(output.String())
	}
}
func TestWorkerRejectsMalformedProtocol(t *testing.T) {
	for _, args := range [][]string{{workerFlag}, {workerFlag, "!"}, {workerFlag, strings.Repeat("x", workerRequestLimit*2+1)}, {workerFlag, base64.RawURLEncoding.EncodeToString([]byte(`{"Operation":"exec"}`))}} {
		handled, err := HandleManagerWorker(args, io.Discard)
		if !handled || err == nil {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
	}
	if handled, err := HandleManagerWorker([]string{"packages"}, io.Discard); handled || err != nil {
		t.Fatal(handled, err)
	}
}
func TestWorkerBoundedOutput(t *testing.T) {
	var buffer boundedBuffer
	if _, err := buffer.Write(make([]byte, workerResponseLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("x")); err == nil {
		t.Fatal("oversized output accepted")
	}
	if buffer.Len() != workerResponseLimit {
		t.Fatal(buffer.Len())
	}
}
func TestWorkerPreservesRecordsAndIdentityHash(t *testing.T) {
	record := Package{Manager: PM_NPM, Name: "tool", Current: "1", Latest: "2", Requested: "2", Executable: "/target/npm", Scope: "global"}
	records := Records(workerRecords([]Package{record}, "builder", "su"))
	if len(records) != 1 || records[0].RunAs != "builder" || records[0].RunAsMethod != "su" || records[0].Executable != "/target/npm" {
		t.Fatal(records)
	}
	plans, err := PlanUpdates(QueryResult{Packages: workerRecords([]Package{record}, "builder", "su")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].RunAs != "builder" || plans[0].RunAsMethod != "su" || len(plans[0].Packages) != 1 || plans[0].Packages[0].RunAsMethod != "su" {
		t.Fatal(plans)
	}
	changed := plans[0]
	changed.RunAs = "root"
	if updatePlanHash(changed) == plans[0].StateHash {
		t.Fatal("identity not hash-bound")
	}
	sudoPlans, err := PlanUpdates(QueryResult{Packages: workerRecords([]Package{record}, "builder", "sudo")}, nil, nil)
	if err != nil || len(sudoPlans) != 1 {
		t.Fatal(sudoPlans, err)
	}
	if sudoPlans[0].StateHash == plans[0].StateHash {
		t.Fatal("run-as method not hash-bound")
	}
	filtered := Records(Filter(workerRecords([]Package{record}, "builder", "su"), func(pkg Package) bool { return pkg.RunAs == "builder" }))
	if len(filtered) != 1 || filtered[0].RunAs != "builder" || filtered[0].RunAsMethod != "su" {
		t.Fatalf("filter lost identity or method: %+v", filtered)
	}
}
func TestWorkerCacheFailsClosed(t *testing.T) {
	workerCache.Lock()
	previous := workerCache.only
	workerCache.only = true
	workerCache.Unlock()
	defer func() { workerCache.Lock(); workerCache.only = previous; workerCache.Unlock() }()
	_, err := callManagerWorker(context.Background(), workerRequest{Operation: "inventory", Manager: PM_NPM, User: "missing_account", Scopes: []Scope{{Kind: "global"}}})
	if err == nil || !strings.Contains(err.Error(), "stopped TUI") {
		t.Fatal(err)
	}
}
func TestWorkerNoTTYNoFallback(t *testing.T) {
	previous, previousCommand := workerTTY, workerCommand
	workerTTY = func() (*os.File, error) { return nil, errors.New("no terminal") }
	workerCommand = func(context.Context, string, ...string) *exec.Cmd {
		t.Fatal("command launched without terminal")
		return nil
	}
	defer func() { workerTTY, workerCommand = previous, previousCommand }()
	for _, method := range []string{"", "sudo", "su"} {
		_, err := callManagerWorker(context.Background(), workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Method: method, Scopes: []Scope{{Kind: "global"}}})
		if err == nil || !strings.Contains(err.Error(), "/dev/tty") {
			t.Fatal(err)
		}
	}
}
func TestWorkerTargetOptions(t *testing.T) {
	options := QueryOptionsFromProfile(config.Profile{Managers: map[string]config.ManagerSettings{PM_NPM: {RunAs: "builder", RunAsMethod: "su"}}})
	if options.RunAs[PM_NPM] != "builder" || options.RunAsMethod[PM_NPM] != "su" {
		t.Fatal(options)
	}
	options.Executables = map[string]string{PM_NPM: "/caller/npm"}
	if local := singleManagerOptions(options, PM_NPM); len(local.RunAsMethod) != 0 || len(local.RunAs) != 0 {
		t.Fatal(local)
	}
	target := managerWorkerOptions(options, PM_NPM)
	if len(target.Executables) != 0 || len(target.RunAs) != 0 || len(target.RunAsMethod) != 0 {
		t.Fatal(target)
	}
}
func TestWorkerRejectsForgedPlanWithoutExecuting(t *testing.T) {
	// Validation rejects identity mismatch before any command lookup.
	plan := UpdatePlan{Manager: PM_NPM, RunAs: "root", Scope: "global", Executable: "/bin/sh", Args: []string{"-c", "false"}}
	request := workerRequest{Operation: "apply", Manager: PM_NPM, User: "builder", Plan: &plan}
	if validateWorkerRequest(request) == nil {
		t.Fatal("mismatched identity accepted")
	}
}
func TestWorkerTransportUsesSudoAndTerminal(t *testing.T) {
	for _, method := range []string{"", "sudo", "su"} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%v", method, failure), func(t *testing.T) { testWorkerTransport(t, method, failure) })
		}
	}
}

func testWorkerTransport(t *testing.T, method string, failure bool) {
	previousCommand, previousTTY := workerCommand, workerTTY
	defer func() { workerCommand, workerTTY = previousCommand, previousTTY }()
	// Regular file stands in for terminal: write prompt then consume supplied yes.
	file, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	prompt := "\nTUI stopped / foreground terminal. Run npm inventory as builder using " + effectiveWorkerMethod(method) + " login environment? Type yes: "
	_, err = file.WriteString(strings.Repeat(" ", len(prompt)) + "yes\n")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	workerTTY = func() (*os.File, error) { return os.OpenFile(file.Name(), os.O_RDWR, 0) }
	calls := 0
	workerCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls++
		var encoded string
		if method == "su" {
			if name != "/usr/bin/su" || len(args) != 4 || !reflect.DeepEqual(args[:3], []string{"-l", "builder", "-c"}) {
				t.Fatalf("unsafe command %s %q", name, args)
			}
			parts := strings.Split(args[3], " ")
			encoded = strings.Trim(parts[len(parts)-1], "'")
		} else {
			if name != "/usr/bin/sudo" || len(args) != 7 || args[0] != "-i" || args[1] != "-u" || args[2] != "builder" || args[3] != "--" || args[5] != workerFlag {
				t.Fatalf("unsafe command %s %q", name, args)
			}
			encoded = args[6]
		}
		raw, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		var request workerRequest
		if json.Unmarshal(raw, &request) != nil || request.Manager != PM_NPM || request.Method != method {
			t.Fatal(string(raw))
		}
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestWorkerTransportHelper")
		command.Env = append(os.Environ(), "CONFICIUS_TRANSPORT_TEST_HELPER=1", "CONFICIUS_TRANSPORT_TEST_METHOD="+method)
		if failure {
			command.Env = append(command.Env, "CONFICIUS_TRANSPORT_TEST_FAILURE=1")
		}
		return command
	}
	response, err := callManagerWorker(context.Background(), workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Method: method, Scopes: []Scope{{Kind: "global"}}})
	if calls != 1 {
		t.Fatalf("expected one attempt, got %d", calls)
	}
	if failure {
		if err == nil {
			t.Fatal("transport failure accepted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Inventory.Managers) != 1 || response.Inventory.Managers[0].RunAs != "builder" || response.Inventory.Managers[0].RunAsMethod != method {
		t.Fatal(response)
	}
}
func TestWorkerApplyRebuildsAndVerifies(t *testing.T) {
	for _, method := range []string{"", "sudo", "su"} {
		t.Run(method, func(t *testing.T) { testWorkerApply(t, method) })
	}
}

func testWorkerApply(t *testing.T, method string) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "npm")
	marker := filepath.Join(dir, "updated")
	writeExecutable(t, executable, `#!/bin/sh
case "$1" in
 --version) printf '10.0.0';;
 outdated) if [ -f "$UPDATED_MARKER" ]; then printf '{}'; else printf '{"tool":{"current":"1","wanted":"2","latest":"2"}}'; fi;;
 update) printf updated > "$UPDATED_MARKER";;
 *) exit 1;;
esac
`)
	t.Setenv("PATH", dir)
	t.Setenv("UPDATED_MARKER", marker)
	result, err := QueryOutdatedScopes(context.Background(), []string{PM_NPM}, []Scope{{Kind: "global"}})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := PlanUpdates(QueryResult{Packages: workerRecords(Records(result.Packages), "builder", method)}, nil, nil)
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	previous := workerTTY
	workerTTY = func() (*os.File, error) { return os.OpenFile(os.DevNull, os.O_RDWR, 0) }
	defer func() { workerTTY = previous }()
	forged := plans[0]
	forged.Args = []string{"--unsafe"}
	if _, err := applyWorkerPlan(context.Background(), forged); err == nil {
		t.Fatal("altered args accepted")
	}
	outcomes, err := applyWorkerPlan(context.Background(), plans[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != "completed" || outcomes[0].StateHash != plans[0].StateHash {
		t.Fatal(outcomes)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := applyWorkerPlan(context.Background(), plans[0]); err == nil {
		t.Fatal("stale plan accepted")
	}
}

func TestWorkerTransportHelper(t *testing.T) {
	if os.Getenv("CONFICIUS_TRANSPORT_TEST_HELPER") != "1" {
		return
	}
	if os.Getenv("CONFICIUS_TRANSPORT_TEST_FAILURE") == "1" {
		os.Exit(1)
	}
	if os.Getenv("CONFICIUS_TRANSPORT_TEST_METHOD") == "su" {
		_, _ = io.WriteString(os.Stdout, "Password: \nLogin shell banner\n")
	}
	_, _ = io.WriteString(os.Stdout, workerResponseMarker+`{"Inventory":{"Managers":[{"name":"npm","status":"available"}]}}`)
	os.Exit(0)
}

func TestWorkerCommandMethodsAndQuoting(t *testing.T) {
	executable := "/tmp/hostile ' \" ; $(touch nope) `false` \\ executable"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"Method":"su"}`))
	for _, goos := range []string{"linux", "darwin"} {
		name, args, err := managerWorkerCommand(goos, "su", "builder", executable, encoded)
		if err != nil || name != "/usr/bin/su" || len(args) != 4 || !reflect.DeepEqual(args[:3], []string{"-l", "builder", "-c"}) {
			t.Fatalf("%s: %s %q %v", goos, name, args, err)
		}
		// Execute only the quoted shell fragment with exec replaced by printf.
		// No su, authentication, or hostile executable is run.
		command := strings.Replace(args[3], "exec ", "printf '%s\\n' ", 1)
		output, err := exec.Command("/bin/sh", "-c", command).Output()
		if err != nil || string(output) != executable+"\n"+workerFlag+"\n"+encoded+"\n" {
			t.Fatalf("quoting changed argv: %q %v", output, err)
		}
		for _, method := range []string{"", "sudo"} {
			name, args, err = managerWorkerCommand(goos, method, "builder", executable, encoded)
			if err != nil || name != "/usr/bin/sudo" || !reflect.DeepEqual(args, []string{"-i", "-u", "builder", "--", executable, workerFlag, encoded}) {
				t.Fatalf("%s: %s %q %v", method, name, args, err)
			}
		}
	}
	for _, path := range []string{"/tmp/hello!abc", "/tmp/hello\nabc", "/tmp/hello\rabc", "/tmp/hello\x00abc"} {
		if _, _, err := managerWorkerCommand("darwin", "su", "builder", path, encoded); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	for _, tc := range []struct{ goos, method string }{{"windows", "su"}, {"freebsd", "su"}, {"linux", "doas"}} {
		if _, _, err := managerWorkerCommand(tc.goos, tc.method, "builder", executable, encoded); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestWorkerApplyMethodValidation(t *testing.T) {
	for _, method := range []string{"", "sudo", "su", "doas"} {
		for _, planned := range []string{"", "sudo", "su", "doas"} {
			plan := UpdatePlan{Manager: PM_NPM, RunAs: "builder", RunAsMethod: planned, Scope: "global"}
			err := validateWorkerRequest(workerRequest{Operation: "apply", Manager: PM_NPM, User: "builder", Method: method, Plan: &plan})
			valid := method != "doas" && planned != "doas" && effectiveWorkerMethod(method) == effectiveWorkerMethod(planned)
			if (err == nil) != valid {
				t.Fatalf("method=%q plan=%q err=%v", method, planned, err)
			}
		}
	}
}

func TestWorkerCacheSeparatesMethods(t *testing.T) {
	workerCache.Lock()
	previousOnly, previousValues := workerCache.only, workerCache.values
	workerCache.only = true
	workerCache.values = make(map[string]workerResponse)
	workerCache.Unlock()
	t.Cleanup(func() {
		workerCache.Lock()
		workerCache.only, workerCache.values = previousOnly, previousValues
		workerCache.Unlock()
	})
	request := workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Scopes: []Scope{{Kind: "global"}}}
	for _, method := range []string{"", "sudo", "su"} {
		request.Method = method
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		workerCache.Lock()
		workerCache.values[string(raw)] = workerResponse{Error: "cached:" + method}
		workerCache.Unlock()
	}
	for _, method := range []string{"", "sudo", "su"} {
		request.Method = method
		_, err := callManagerWorker(context.Background(), request)
		if err == nil || err.Error() != "cached:"+method {
			t.Fatalf("method %q: %v", method, err)
		}
	}
}
