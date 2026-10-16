package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func fakeCacheTools(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	t.Setenv("CACHE_TEST_LOG", log)
	for name, script := range map[string]string{
		"apt":     "#!/bin/sh\nprintf 'apt 2.6.1\\n'\n",
		"apt-get": "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$CACHE_TEST_LOG\"\nexit \"${CACHE_TEST_EXIT:-0}\"\n",
		"sudo":    "#!/bin/sh\nprintf 'sudo\\n' >> \"$CACHE_TEST_LOG\"\n[ \"$1\" = -- ] || exit 90\nshift\nexec \"$@\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir, log
}

func TestCacheRefreshBindingsRequireExplicitApproval(t *testing.T) {
	dir, log := fakeCacheTools(t)
	for _, view := range []string{"scopes", "results"} {
		t.Run(view, func(t *testing.T) {
			data := &model{}
			app := glyph.NewApp()
			app.View("home", glyph.Text("home"))
			app.View(view, glyph.Text(view))
			registerCacheRefreshForPlatform(data, app, "linux")
			app.Go("home")
			app.Input().Dispatch(riffkey.Key{Rune: 'c'})
			if app.CurrentView() != "home" || data.cacheExecutable != "" {
				t.Fatal("cache offered outside package views")
			}
			app.Go(view)
			for _, cancel := range []riffkey.Key{{Rune: 'n'}, {Special: riffkey.SpecialEscape}} {
				app.Input().Dispatch(riffkey.Key{Rune: 'c'})
				if app.CurrentView() != "cache-confirm" || !strings.Contains(data.cachePreview, filepath.Join(dir, "apt-get")) || !strings.Contains(data.cachePreview, "update --error-on=any") {
					t.Fatalf("preview: %s %s", app.CurrentView(), data.cachePreview)
				}
				app.Input().Dispatch(riffkey.Key{Special: riffkey.SpecialEnter})
				if data.runCacheRefresh {
					t.Fatal("Enter approved refresh")
				}
				app.Input().Dispatch(cancel)
				if app.CurrentView() != view || data.runCacheRefresh || data.cacheRefreshErr == "" {
					t.Fatal("cancel changed execution state")
				}
			}
			if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("implicit refresh: %v", err)
			}
			canceled := 0
			data.cancel = func() { canceled++ }
			data.dotfileCancel = func() { canceled++ }
			app.Input().Dispatch(riffkey.Key{Rune: 'c'})
			app.Input().Dispatch(riffkey.Key{Rune: 'y'})
			if !data.runCacheRefresh || data.runInteractive || canceled != 2 {
				t.Fatalf("approval: %+v canceled=%d", data, canceled)
			}
		})
	}
}

func TestCacheRefreshPlatform(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			fakeCacheTools(t)
			available := goos == "linux"
			if cacheRefreshAvailable(goos) != available {
				t.Fatal("incorrect cache refresh availability")
			}
			footer := cacheRefreshFooter("p profiles", goos)
			if strings.Contains(footer, "c update cache") != available || strings.Contains(footer, "APT") {
				t.Fatalf("footer: %s", footer)
			}
			for _, view := range []string{"scopes", "results"} {
				help := screenHelpForPlatform(view, goos)
				if strings.Contains(help, "c update cache") != available || strings.Contains(help, "APT") {
					t.Fatalf("help: %s", help)
				}
				data := &model{}
				app := glyph.NewApp()
				app.View(view, glyph.Text(view))
				registerCacheRefreshForPlatform(data, app, goos)
				app.Go(view)
				app.Input().Dispatch(riffkey.Key{Rune: 'c'})
				if available {
					if app.CurrentView() != "cache-confirm" {
						t.Fatal("Linux cache binding missing")
					}
				} else {
					if app.CurrentView() != view || data.cacheExecutable != "" || data.cacheRefreshErr != "" {
						t.Fatal("cache binding active outside Linux")
					}
					if _, ok := app.ViewRouter("cache-confirm"); ok {
						t.Fatal("cache confirmation registered outside Linux")
					}
				}
			}
		})
	}
}

func TestCacheRefreshCommandRejectsUnavailableTools(t *testing.T) {
	for _, kind := range []string{"disabled", "config", "missing apt", "missing apt-get", "nonexecutable", "directory", "ambiguous", "missing sudo"} {
		t.Run(kind, func(t *testing.T) {
			dir, _ := fakeCacheTools(t)
			data := &model{}
			switch kind {
			case "disabled":
				enabled := false
				data.profile.Profile.Managers = map[string]config.ManagerSettings{packagemanager.PM_APT: {Enabled: &enabled}}
			case "config":
				data.packageConfigErr = errors.New("invalid config")
			case "missing apt":
				if err := os.Remove(filepath.Join(dir, "apt")); err != nil {
					t.Fatal(err)
				}
			case "missing apt-get":
				if err := os.Remove(filepath.Join(dir, "apt-get")); err != nil {
					t.Fatal(err)
				}
			case "nonexecutable":
				if err := os.Chmod(filepath.Join(dir, "apt-get"), 0644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(filepath.Join(dir, "apt-get")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "apt-get"), 0755); err != nil {
					t.Fatal(err)
				}
			case "ambiguous":
				other := t.TempDir()
				if err := os.WriteFile(filepath.Join(other, "apt"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", dir+string(os.PathListSeparator)+other)
			case "missing sudo":
				if os.Geteuid() == 0 {
					t.Skip("root does not need sudo")
				}
				if err := os.Remove(filepath.Join(dir, "sudo")); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := cacheRefreshCommand(data); err == nil {
				t.Fatal("unsafe cache refresh accepted")
			}
		})
	}
}

func TestCacheRefreshExecutionAndRestart(t *testing.T) {
	for _, failed := range []bool{false, true} {
		for _, view := range []string{"scopes", "results"} {
			t.Run(view+map[bool]string{false: " success", true: " failure"}[failed], func(t *testing.T) {
				dir, log := fakeCacheTools(t)
				if failed {
					t.Setenv("CACHE_TEST_EXIT", "7")
				}
				data := &model{runCacheRefresh: true, cacheReturnView: view, profile: config.ResolvedProfile{Name: "work"}, active: &config.Scope{Kind: "project", Path: "/repo"}}
				var err error
				data.cacheExecutable, data.cacheArgs, err = cacheRefreshCommand(data)
				if err != nil {
					t.Fatal(err)
				}
				aptGet := filepath.Join(dir, "apt-get")
				wantArgs := []string{"update", "--error-on=any"}
				if os.Geteuid() != 0 {
					wantArgs = append([]string{"--", aptGet}, wantArgs...)
				}
				if !reflect.DeepEqual(data.cacheArgs, wantArgs) {
					t.Fatalf("args=%q", data.cacheArgs)
				}
				if !filepath.IsAbs(data.cacheExecutable) {
					t.Fatal("relative command")
				}
				terminal, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer terminal.Close()
				executeCacheRefresh(data, terminal)
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				want := "update\n--error-on=any\n"
				if os.Geteuid() != 0 {
					want = "sudo\n" + want
				}
				if string(calls) != want {
					t.Fatalf("calls=%q", calls)
				}
				if (data.cacheRefreshErr != "") != failed || !data.cacheRefreshDone {
					t.Fatalf("result=%+v", data)
				}
				state := tuiRestartState(data)
				if state.Profile != "work" || state.ScopeKind != "project" || state.ScopePath != "/repo" {
					t.Fatalf("state=%+v", state)
				}
				encoded, err := encodeRestartState(state)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := decodeRestartState(encoded)
				if err != nil {
					t.Fatal(err)
				}
				decoded.Profile = ""
				restored := &model{scopes: []config.Scope{{Kind: "project", Path: "/repo"}}}
				if got := restoreRestartState(restored, decoded); got != view {
					t.Fatalf("view=%s", got)
				}
				if fullRefreshOnStart(restored, view) == failed {
					t.Fatal("wrong startup refresh mode")
				}
				if restored.active == nil || restored.active.Path != "/repo" {
					t.Fatal("scope lost")
				}
				restored.status = "inventory refreshed"
				if failed {
					context := packageContext(restored).(func() string)()
					summary, _ := packageHomeSummary(restored)
					if !strings.Contains(context, restored.cacheRefreshErr) || !strings.Contains(summary, restored.cacheRefreshErr) || !strings.Contains(context, "stale") {
						t.Fatal("failure notice lost")
					}
				}
			})
		}
	}
	if fullRefreshOnStart(&model{}, "scopes") || !fullRefreshOnStart(&model{}, "results") {
		t.Fatal("ordinary startup changed")
	}
}
