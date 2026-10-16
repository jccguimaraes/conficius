package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jccguimaraes/conficius/internal/packagemanager"
	"github.com/kungfusheep/glyph"
	"github.com/kungfusheep/riffkey"
)

func cacheRefreshCommand(data *model) (string, []string, error) {
	if data.packageConfigErr != nil {
		return "", nil, fmt.Errorf("package configuration error: %w", data.packageConfigErr)
	}
	if packagemanager.QueryOptionsFromProfile(data.profile.Profile).Disabled[packagemanager.PM_APT] {
		return "", nil, fmt.Errorf("APT disabled by profile")
	}
	statuses, err := packagemanager.Inspect([]string{packagemanager.PM_APT})
	if err != nil {
		return "", nil, err
	}
	if len(statuses) != 1 || statuses[0].Status != packagemanager.StatusAvailable || len(statuses[0].Executables) == 0 {
		return "", nil, fmt.Errorf("APT unavailable")
	}
	if len(statuses[0].Executables) != 1 {
		return "", nil, fmt.Errorf("ambiguous APT executables; repair PATH")
	}
	aptGet, err := filepath.Abs(filepath.Join(filepath.Dir(statuses[0].Executables[0]), "apt-get"))
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(aptGet)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", nil, fmt.Errorf("executable apt-get required beside discovered APT: %s", aptGet)
	}
	args := []string{"update", "--error-on=any"}
	if os.Geteuid() == 0 {
		return aptGet, args, nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return "", nil, fmt.Errorf("sudo unavailable: %w", err)
	}
	sudo, err = filepath.Abs(sudo)
	if err != nil {
		return "", nil, err
	}
	return sudo, append([]string{"--", aptGet}, args...), nil
}

func cacheRefreshAvailable(goos string) bool {
	return goos == "linux"
}

func cacheRefreshFooter(help, goos string) string {
	if cacheRefreshAvailable(goos) {
		return help + "  c update cache"
	}
	return help
}

func registerCacheRefresh(data *model, app *glyph.App) {
	registerCacheRefreshForPlatform(data, app, runtime.GOOS)
}

func registerCacheRefreshForPlatform(data *model, app *glyph.App, goos string) {
	if !cacheRefreshAvailable(goos) {
		return
	}
	prepare := func(_ riffkey.Match) {
		view := app.CurrentView()
		if view != "scopes" && view != "results" {
			return
		}
		data.cacheReturnView = view
		executable, args, err := cacheRefreshCommand(data)
		if err != nil {
			data.cacheRefreshErr = "APT cache refresh blocked: " + err.Error()
			app.RequestRender()
			return
		}
		data.cacheExecutable, data.cacheArgs = executable, args
		data.cachePreview = formatPlan(packagemanager.UpdatePlan{Executable: executable, Args: args})
		app.Go("cache-confirm")
	}
	for _, view := range []string{"scopes", "results"} {
		if router, ok := app.ViewRouter(view); ok {
			router.Handle("c", prepare)
		}
	}
	cancel := func() {
		data.cacheRefreshErr = "APT cache refresh canceled; cached metadata may be stale"
		data.cacheExecutable, data.cacheArgs = "", nil
		app.Go(data.cacheReturnView)
	}
	app.View("cache-confirm", screenFrame("Packages / Update package cache", packageContext(data), nil,
		glyph.VBox.Grow(1)(glyph.Text("Refresh APT metadata only; no package upgrades. Explicit y required."), glyph.TextBlock(&data.cachePreview).CharWrap()), "y refresh cache  n/Esc cancel")).
		Handle("n", cancel).Handle("<Esc>", cancel).Handle("q", cancel).
		Handle("y", func() {
			if data.cacheExecutable == "" {
				return
			}
			if data.cancel != nil {
				data.cancel()
			}
			if data.dotfileCancel != nil {
				data.dotfileCancel()
			}
			data.runCacheRefresh = true
			app.Stop()
		})
}

func executeCacheRefresh(data *model, terminal io.ReadWriter) {
	command := exec.Command(data.cacheExecutable, data.cacheArgs...)
	command.Stdin, command.Stdout, command.Stderr = terminal, terminal, terminal
	data.cacheRefreshDone = true
	data.cacheRefreshErr = ""
	if err := command.Run(); err != nil {
		data.cacheRefreshErr = "APT cache refresh failed: " + err.Error() + "; cached metadata may be stale"
	}
}

func runInteractiveCacheRefresh(data *model) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		data.cacheRefreshDone = true
		data.cacheRefreshErr = "APT cache refresh failed: " + err.Error() + "; cached metadata may be stale"
		data.runCacheRefresh = false
		return runSession(data, data.cacheReturnView)
	}
	defer func() { _ = tty.Close() }()
	executeCacheRefresh(data, tty)
	return execFreshTUI(tty, data)
}

func fullRefreshOnStart(data *model, view string) bool {
	if data.cacheRefreshDone {
		return data.cacheRefreshErr == ""
	}
	return view == "results"
}
