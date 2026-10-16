package packagemanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestWorkerValidation(t *testing.T) {
	valid := workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Scopes: []Scope{{Kind: "global"}}}
	if err := validateWorkerRequest(valid); err != nil {
		t.Fatal(err)
	}
	cases := []workerRequest{valid, valid, valid, valid, valid, valid}
	cases[0].User = "-root"
	cases[1].Operation = "exec"
	cases[2].Manager = "sh"
	cases[3].Scopes = []Scope{{Kind: "project", Path: "/tmp"}}
	cases[4].Options.RunAs = map[string]string{PM_NPM: "root"}
	cases[5].Options.Executables = map[string]string{PM_NPM: "/bin/sh"}
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
	records := Records(workerRecords([]Package{record}, "builder"))
	if len(records) != 1 || records[0].RunAs != "builder" || records[0].Executable != "/target/npm" {
		t.Fatal(records)
	}
	plans, err := PlanUpdates(QueryResult{Packages: workerRecords([]Package{record}, "builder")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].RunAs != "builder" {
		t.Fatal(plans)
	}
	changed := plans[0]
	changed.RunAs = "root"
	if updatePlanHash(changed) == plans[0].StateHash {
		t.Fatal("identity not hash-bound")
	}
	filtered := Filter(workerRecords([]Package{record}, "builder"), func(pkg Package) bool { return pkg.RunAs == "builder" })
	if len(Records(filtered)) != 1 {
		t.Fatal("filter lost identity")
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
	previous := workerTTY
	workerTTY = func() (*os.File, error) { return nil, errors.New("no terminal") }
	defer func() { workerTTY = previous }()
	_, err := callManagerWorker(context.Background(), workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Scopes: []Scope{{Kind: "global"}}})
	if err == nil || !strings.Contains(err.Error(), "/dev/tty") {
		t.Fatal(err)
	}
}
func TestWorkerTargetOptions(t *testing.T) {
	options := QueryOptionsFromProfile(config.Profile{Managers: map[string]config.ManagerSettings{PM_NPM: {RunAs: "builder"}}})
	if options.RunAs[PM_NPM] != "builder" {
		t.Fatal(options)
	}
	options.Executables = map[string]string{PM_NPM: "/caller/npm"}
	target := managerWorkerOptions(options, PM_NPM)
	if len(target.Executables) != 0 || len(target.RunAs) != 0 {
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
	previousCommand, previousTTY := workerCommand, workerTTY
	defer func() { workerCommand, workerTTY = previousCommand, previousTTY }()
	// Regular file stands in for terminal: write prompt then consume supplied yes.
	file, err := os.CreateTemp(t.TempDir(), "terminal")
	if err != nil {
		t.Fatal(err)
	}
	prompt := "\nTUI stopped / foreground terminal. Run npm inventory as builder using sudo login environment? Type yes: "
	_, err = file.WriteString(strings.Repeat(" ", len(prompt)) + "yes\n")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	workerTTY = func() (*os.File, error) { return os.OpenFile(file.Name(), os.O_RDWR, 0) }
	workerCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name != "/usr/bin/sudo" || len(args) != 7 || args[0] != "-i" || args[1] != "-u" || args[2] != "builder" || args[3] != "--" || args[5] != workerFlag {
			t.Fatalf("unsafe command %s %q", name, args)
		}
		raw, decodeErr := base64.RawURLEncoding.DecodeString(args[6])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		var request workerRequest
		if json.Unmarshal(raw, &request) != nil || request.Manager != PM_NPM {
			t.Fatal(string(raw))
		}
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestWorkerTransportHelper")
		command.Env = append(os.Environ(), "CONFICIUS_TRANSPORT_TEST_HELPER=1")
		return command
	}
	response, err := callManagerWorker(context.Background(), workerRequest{Operation: "inventory", Manager: PM_NPM, User: "builder", Scopes: []Scope{{Kind: "global"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Inventory.Managers) != 1 || response.Inventory.Managers[0].RunAs != "builder" {
		t.Fatal(response)
	}
}
func TestWorkerApplyRebuildsAndVerifies(t *testing.T) {
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
	plans, err := PlanUpdates(QueryResult{Packages: workerRecords(Records(result.Packages), "builder")}, nil, nil)
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
	_, _ = io.WriteString(os.Stdout, `{"Inventory":{"Managers":[{"name":"npm","status":"available"}]}}`)
	os.Exit(0)
}
