package packagemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/semver"
)

type cargoGlobalInstall struct {
	Bins              []string `json:"bins"`
	Features          []string `json:"features"`
	AllFeatures       *bool    `json:"all_features"`
	NoDefaultFeatures *bool    `json:"no_default_features"`
	Profile           string   `json:"profile"`
	Target            *string  `json:"target"`
}

type cargoGlobalConfig struct {
	Root    string             `json:"root"`
	Install cargoGlobalInstall `json:"install"`
}

var cargoGlobalName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
var cargoGlobalFeature = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_+./?-]*$`)
var cargoGlobalID = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]*) ([^ ]+) \((registry\+https://github\.com/rust-lang/crates\.io-index|sparse\+https://index\.crates\.io/)\)$`)
var cargoGlobalVersionLine = regexp.MustCompile(`^version: ([^ ]+)(?: \(latest:? ([^ )]+)\))?$`)

func cargoGlobalVersion(version string) bool {
	return strings.Count(strings.SplitN(strings.SplitN(version, "+", 2)[0], "-", 2)[0], ".") == 2 && semver.IsValid("v"+version)
}

func cargoGlobalRoot() (string, error) {
	home := os.Getenv("CARGO_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".cargo")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("relative CARGO_HOME unsupported")
	}
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(key, "CARGO_SOURCE_") || strings.HasPrefix(key, "CARGO_REGISTRIES_CRATES_IO_") {
			return "", fmt.Errorf("Cargo registry override %s unsupported", key)
		}
	}
	root := home
	for _, name := range []string{"config", "config.toml"} {
		contents, err := os.ReadFile(filepath.Join(home, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var config struct {
			Install struct {
				Root string `toml:"root"`
			} `toml:"install"`
			Registry struct {
				Index string `toml:"index"`
			} `toml:"registry"`
			Source     map[string]any `toml:"source"`
			Registries map[string]any `toml:"registries"`
		}
		if err := toml.Unmarshal(contents, &config); err != nil {
			return "", fmt.Errorf("decode Cargo config: %w", err)
		}
		if len(config.Source) != 0 || config.Registries["crates-io"] != nil || config.Registry.Index != "" {
			return "", fmt.Errorf("Cargo source or crates-io registry override unsupported")
		}
		if config.Install.Root != "" {
			root = config.Install.Root
		}
		break
	}
	if value := os.Getenv("CARGO_INSTALL_ROOT"); value != "" {
		root = value
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("relative Cargo install root unsupported")
	}
	return filepath.Clean(root), nil
}

func cargoGlobalInstallValid(config cargoGlobalConfig) bool {
	install := config.Install
	if !filepath.IsAbs(config.Root) || strings.ContainsAny(config.Root, "\x00\r\n") || len(install.Bins) == 0 || install.Features == nil || install.AllFeatures == nil || install.NoDefaultFeatures == nil || !cargoGlobalName.MatchString(install.Profile) || install.Target == nil || !cargoGlobalName.MatchString(*install.Target) {
		return false
	}
	for _, bin := range install.Bins {
		if !cargoGlobalName.MatchString(bin) {
			return false
		}
	}
	for _, feature := range install.Features {
		if !cargoGlobalFeature.MatchString(feature) {
			return false
		}
	}
	return true
}

func cargoGlobalBinTarget(bin, goos string) string {
	if goos == "windows" {
		return strings.TrimSuffix(bin, ".exe")
	}
	return bin
}

func cargoGlobalTracking(root string) (map[string]Package, error) {
	root, err := filepath.EvalSymlinks(root)
	if os.IsNotExist(err) {
		return make(map[string]Package), nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve Cargo install root: %w", err)
	}
	packages := make(map[string]Package)
	var owned struct {
		V1 map[string][]string `toml:"v1"`
	}
	contents, err := os.ReadFile(filepath.Join(root, ".crates.toml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read Cargo owned installs: %w", err)
	}
	if err == nil {
		if err := toml.Unmarshal(contents, &owned); err != nil {
			return nil, fmt.Errorf("decode Cargo owned installs: %w", err)
		}
	}
	contents, err = os.ReadFile(filepath.Join(root, ".crates2.json"))
	if os.IsNotExist(err) && len(owned.V1) == 0 {
		return packages, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Cargo install options: %w", err)
	}
	var tracking struct {
		Installs map[string]json.RawMessage `json:"installs"`
	}
	if err := json.Unmarshal(contents, &tracking); err != nil {
		return nil, fmt.Errorf("decode Cargo install tracking: %w", err)
	}
	if tracking.Installs == nil {
		return nil, fmt.Errorf("missing Cargo installs tracking")
	}
	for id, bins := range owned.V1 {
		match := cargoGlobalID.FindStringSubmatch(id)
		if match == nil || len(bins) == 0 {
			continue
		}
		if !cargoGlobalVersion(match[2]) {
			return nil, fmt.Errorf("invalid tracked Cargo version %q", match[2])
		}
		raw, ok := tracking.Installs[id]
		if !ok {
			return nil, fmt.Errorf("missing Cargo install options for %s", match[1])
		}
		var install cargoGlobalInstall
		if err := json.Unmarshal(raw, &install); err != nil {
			return nil, fmt.Errorf("decode Cargo install %s: %w", match[1], err)
		}
		for _, bin := range bins {
			if !slices.Contains(install.Bins, bin) {
				return nil, fmt.Errorf("Cargo owned bin %q missing install options for %s", bin, match[1])
			}
		}
		install.Bins = make([]string, len(bins))
		for i, bin := range bins {
			install.Bins[i] = cargoGlobalBinTarget(bin, runtime.GOOS)
		}
		if install.Target == nil || *install.Target == "" {
			return nil, fmt.Errorf("unknown Cargo install target for %s", match[1])
		}
		config := cargoGlobalConfig{Root: root, Install: install}
		if !cargoGlobalInstallValid(config) {
			return nil, fmt.Errorf("incomplete or unsupported Cargo install metadata for %s", match[1])
		}
		if _, exists := packages[match[1]]; exists {
			return nil, fmt.Errorf("ambiguous Cargo install %s", match[1])
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		packages[match[1]] = Package{Manager: PM_CARGO, Name: match[1], Current: match[2], Config: string(encoded), Role: RoleApplication, Scope: "global"}
	}
	return packages, nil
}

func cargoGlobalInfoVersion(output []byte, name string) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (strings.TrimSpace(lines[0]) != name && !strings.HasPrefix(lines[0], name+" #")) {
		return "", fmt.Errorf("unexpected cargo info package for %s", name)
	}
	version := ""
	for _, line := range lines {
		if !strings.HasPrefix(line, "version:") {
			continue
		}
		match := cargoGlobalVersionLine.FindStringSubmatch(line)
		if match == nil || version != "" || !cargoGlobalVersion(match[1]) {
			return "", fmt.Errorf("invalid cargo info version for %s", name)
		}
		version = match[1]
		if match[2] != "" {
			if !cargoGlobalVersion(match[2]) {
				return "", fmt.Errorf("invalid cargo info latest version")
			}
		}
	}
	if version == "" {
		return "", fmt.Errorf("missing cargo info version for %s", name)
	}
	return version, nil
}

func cargoGlobalOutdated(ctx context.Context, executable string, scope Scope, packages Packages) error {
	if scope.Kind != "global" {
		return fmt.Errorf("Cargo global updates require global scope")
	}
	root, err := cargoGlobalRoot()
	if err != nil {
		return err
	}
	installed, err := cargoGlobalTracking(root)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "conficius-cargo-info-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)
	pending := make(Packages)
	for _, name := range names {
		output, err := runOutputDir(ctx, dir, executable, "info", name, "--registry", "crates-io", "--color", "never")
		if err != nil {
			return err
		}
		latest, err := cargoGlobalInfoVersion(output, name)
		if err != nil {
			return err
		}
		pkg := installed[name]
		if semver.Compare("v"+latest, "v"+pkg.Current) <= 0 {
			continue
		}
		pkg.Latest, pkg.Executable = latest, executable
		pending[name] = append(pending[name], recordTool{pkg})
	}
	for name, tools := range pending {
		packages[name] = append(packages[name], tools...)
	}
	return nil
}

func cargoValidateGlobalPlan(plan UpdatePlan) error {
	stale := func(reason any) error {
		return fmt.Errorf("Cargo global update plan is stale; refresh preview before applying: %v", reason)
	}
	if plan.Manager != PM_CARGO || plan.Scope != "global" || len(plan.Packages) != 1 {
		return stale("expected one global Cargo package")
	}
	pkg := plan.Packages[0]
	var expected cargoGlobalConfig
	if err := json.Unmarshal([]byte(pkg.Config), &expected); err != nil || !cargoGlobalInstallValid(expected) {
		return stale("invalid install settings")
	}
	root, err := cargoGlobalRoot()
	if err != nil {
		return stale(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return stale(err)
	}
	if root != expected.Root {
		return stale("install root changed")
	}
	installed, err := cargoGlobalTracking(root)
	if err != nil {
		return stale(err)
	}
	actual, ok := installed[pkg.Name]
	if !ok || actual.Current != pkg.Current {
		return stale("installed version changed or package removed")
	}
	var current cargoGlobalConfig
	if err := json.Unmarshal([]byte(actual.Config), &current); err != nil || !reflect.DeepEqual(current, expected) {
		return stale("install settings changed")
	}
	actual.Latest = pkg.Latest
	args := cargoGlobalUpdateArgs([]Package{actual})
	if args == nil || !slices.Equal(args, plan.Args) || !slices.Equal(cargoGlobalUpdateArgs(plan.Packages), plan.Args) {
		return stale("install arguments changed")
	}
	return nil
}

func cargoGlobalUpdateArgs(packages []Package) []string {
	if len(packages) != 1 {
		return nil
	}
	pkg := packages[0]
	if pkg.Manager != PM_CARGO || pkg.Scope != "global" || !cargoGlobalName.MatchString(pkg.Name) || !cargoGlobalVersion(pkg.Latest) {
		return nil
	}
	var config cargoGlobalConfig
	if err := json.Unmarshal([]byte(pkg.Config), &config); err != nil || !cargoGlobalInstallValid(config) {
		return nil
	}
	install := config.Install
	args := []string{"install", pkg.Name, "--version", pkg.Latest, "--registry", "crates-io", "--root", config.Root, "--profile", install.Profile, "--target", *install.Target}
	for _, bin := range install.Bins {
		args = append(args, "--bin", bin)
	}
	for _, feature := range install.Features {
		args = append(args, "--features", feature)
	}
	if *install.AllFeatures {
		args = append(args, "--all-features")
	}
	if *install.NoDefaultFeatures {
		args = append(args, "--no-default-features")
	}
	return args
}
