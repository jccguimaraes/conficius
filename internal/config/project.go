package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	FileName       = "conficius.toml"
	CurrentVersion = 2
)

var ErrNotFound = errors.New("conficius config not found")

const Example = `# Conficius project configuration.
version = 2

# Profiles select package managers, projects, packages, and exact desired versions.
# Keep global npm packages outside version-managed Node directories:
# npm config set prefix "$HOME/.npm-global"
# Add "$HOME/.npm-global/bin" to PATH.
[profiles.default]
mode = "denylist"
manager_order = ["mise", "brew", "npm"]

[profiles.default.managers.npm]
expected_global_prefix = "${HOME}/.npm-global"

# Tool names are free-form. Source paths are relative to this file's directory.
# Targets support $VAR and ${VAR}; standard HOME and XDG variables have defaults.
# Omit platforms or use an empty list to enable a link on every platform.
[tools.example]
links = [
  { source = "dotfiles/example.conf", target = "$HOME/.config/example/example.conf", platforms = ["linux", "darwin"] },
]

# Optional OSV mappings enable reliable scans for mise/Homebrew packages.
[[vulnerabilities.mappings]]
manager = "mise"
package = "example"
ecosystem = "Go"
name = "example.org/module"
`

type Project struct {
	Version         int                 `toml:"version"`
	Profile         string              `toml:"profile"`
	Profiles        map[string]Profile  `toml:"profiles"`
	Tools           map[string]Tool     `toml:"tools"`
	Vulnerabilities VulnerabilityConfig `toml:"vulnerabilities"`
	Path            string              `toml:"-"`
	Root            string              `toml:"-"`
}

type Tool struct {
	Links []Link `toml:"links"`
}

type Link struct {
	Source    string   `toml:"source"`
	Target    string   `toml:"target"`
	Platforms []string `toml:"platforms"`
}

type VulnerabilityConfig struct {
	Mappings []VulnerabilityMapping `toml:"mappings"`
}

type VulnerabilityMapping struct {
	Manager   string `toml:"manager"`
	Package   string `toml:"package"`
	Ecosystem string `toml:"ecosystem"`
	Name      string `toml:"name"`
}

// Discover applies config precedence: explicit path, cwd and its parents, then XDG.
func Discover(explicit string) (string, error) {
	if explicit != "" {
		return absolutePath(explicit)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return discover("", cwd, xdgConfigHome(home))
}

func discover(explicit, cwd, xdgHome string) (string, error) {
	if explicit != "" {
		return absolutePath(explicit)
	}

	dir, err := absolutePath(cwd)
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, FileName)
		if isFile(candidate) {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	candidate, err := absolutePath(filepath.Join(xdgHome, "conficius", FileName))
	if err != nil {
		return "", err
	}
	if isFile(candidate) {
		return candidate, nil
	}

	return "", ErrNotFound
}

func Load(path string) (*Project, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, err
	}

	var project Project
	metadata, err := toml.DecodeFile(path, &project)
	if err != nil {
		return nil, fmt.Errorf("decode config %q: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("unknown config fields: %s", strings.Join(keys, ", "))
	}
	for name, profile := range project.Profiles {
		profile.ModeSet = metadata.IsDefined("profiles", name, "mode")
		project.Profiles[name] = profile
	}
	if project.Version != 1 && project.Version != CurrentVersion {
		return nil, fmt.Errorf("config version must be 1 or %d, got %d", CurrentVersion, project.Version)
	}
	if project.Version == 1 && (project.Profile != "" || len(project.Profiles) > 0) {
		return nil, errors.New("profiles require config version 2")
	}

	project.Path = path
	project.Root = filepath.Dir(path)
	if err := project.normalize(); err != nil {
		return nil, err
	}

	return &project, nil
}

func LoadDiscovered(explicit string) (*Project, error) {
	path, err := Discover(explicit)
	if err != nil {
		return nil, err
	}
	project, err := Load(path)
	if err != nil || explicit != "" {
		return project, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}
	globalPath := filepath.Join(xdgConfigHome(home), "conficius", FileName)
	globalPath, err = absolutePath(globalPath)
	if err != nil || globalPath == path || !isFile(globalPath) {
		return project, err
	}
	global, err := Load(globalPath)
	if err != nil {
		return nil, err
	}
	project.Profiles = mergeProfiles(global.Profiles, project.Profiles)
	return project, nil
}

func mergeProfiles(global, local map[string]Profile) map[string]Profile {
	merged := make(map[string]Profile, len(global)+len(local))
	for name, profile := range global {
		merged[name] = profile
	}
	for name, override := range local {
		base, exists := merged[name]
		if !exists {
			merged[name] = override
			continue
		}
		if override.ModeSet {
			base.Mode = override.Mode
		}
		if override.SecurityScope != "" {
			base.SecurityScope = override.SecurityScope
		}
		if len(override.ManagerOrder) > 0 {
			base.ManagerOrder = override.ManagerOrder
		}
		if base.Managers == nil {
			base.Managers = map[string]ManagerSettings{}
		}
		for manager, settings := range override.Managers {
			base.Managers[manager] = settings
		}
		if len(override.Packages) > 0 {
			base.Packages = override.Packages
		}
		if len(override.Projects) > 0 {
			base.Projects = override.Projects
		}
		merged[name] = base
	}
	return merged
}

func (project *Project) normalize() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return fmt.Errorf("resolve home directory symlinks: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(project.Root)
	if err != nil {
		return fmt.Errorf("resolve config root symlinks: %w", err)
	}

	if err := project.normalizeVulnerabilityMappings(); err != nil {
		return err
	}
	if project.Version == 2 {
		if project.Profile != "" && !profileNamePattern.MatchString(project.Profile) {
			return fmt.Errorf("invalid selected profile %q", project.Profile)
		}
		for name, profile := range project.Profiles {
			if !profileNamePattern.MatchString(name) {
				return fmt.Errorf("invalid profile name %q", name)
			}
			if err := normalizeProfile(&profile, project.Root); err != nil {
				return fmt.Errorf("profile %q: %w", name, err)
			}
			project.Profiles[name] = profile
		}
	}

	for toolName, tool := range project.Tools {
		if strings.TrimSpace(toolName) == "" {
			return errors.New("tool name must not be empty")
		}
		if len(tool.Links) == 0 {
			return fmt.Errorf("tool %q must define at least one link", toolName)
		}

		for i := range tool.Links {
			link := &tool.Links[i]
			if err := normalizeLink(link, project.Root, resolvedRoot, home, resolvedHome); err != nil {
				return fmt.Errorf("tool %q link %d: %w", toolName, i+1, err)
			}
		}
		project.Tools[toolName] = tool
	}

	return nil
}

func (project *Project) normalizeVulnerabilityMappings() error {
	seen := make(map[string]struct{}, len(project.Vulnerabilities.Mappings))
	for i := range project.Vulnerabilities.Mappings {
		mapping := &project.Vulnerabilities.Mappings[i]
		mapping.Manager = strings.ToLower(strings.TrimSpace(mapping.Manager))
		mapping.Package = strings.TrimSpace(mapping.Package)
		mapping.Ecosystem = strings.TrimSpace(mapping.Ecosystem)
		mapping.Name = strings.TrimSpace(mapping.Name)
		if mapping.Manager == "" || mapping.Package == "" || mapping.Ecosystem == "" || mapping.Name == "" {
			return fmt.Errorf("vulnerability mapping %d requires manager, package, ecosystem, and name", i+1)
		}
		switch mapping.Manager {
		case "mise", "brew":
		default:
			return fmt.Errorf("vulnerability mapping %d has unsupported manager %q", i+1, mapping.Manager)
		}
		key := mapping.Manager + "\x00" + mapping.Package
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate vulnerability mapping for %s:%s", mapping.Manager, mapping.Package)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func normalizeLink(link *Link, root, resolvedRoot, home, resolvedHome string) error {
	if strings.TrimSpace(link.Source) == "" {
		return errors.New("source is required")
	}
	if strings.TrimSpace(link.Target) == "" {
		return errors.New("target is required")
	}

	source := link.Source
	if !filepath.IsAbs(source) {
		source = filepath.Join(root, source)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	if !within(root, source) {
		return fmt.Errorf("source %q is outside config root %q", source, root)
	}
	if resolved, err := filepath.EvalSymlinks(source); err == nil && !within(resolvedRoot, resolved) {
		return fmt.Errorf("source %q resolves outside config root %q", source, root)
	}

	target, err := expandTarget(link.Target, home)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(home, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	if !within(home, target) || target == home {
		return fmt.Errorf("target %q must be inside home %q", target, home)
	}
	if resolved, err := resolveExistingPath(filepath.Dir(target)); err == nil && !within(resolvedHome, resolved) {
		return fmt.Errorf("target parent for %q resolves outside home %q", target, home)
	}

	seenPlatforms := make(map[string]struct{}, len(link.Platforms))
	for i, platform := range link.Platforms {
		platform = strings.ToLower(strings.TrimSpace(platform))
		if platform == "" {
			return errors.New("platform must not be empty")
		}
		if _, exists := seenPlatforms[platform]; exists {
			return fmt.Errorf("duplicate platform %q", platform)
		}
		seenPlatforms[platform] = struct{}{}
		link.Platforms[i] = platform
	}

	link.Source = filepath.Clean(source)
	link.Target = filepath.Clean(target)
	return nil
}

func expandTarget(target, home string) (string, error) {
	if strings.HasPrefix(target, "~") {
		return "", errors.New("target does not support ~ expansion; use $HOME or ${HOME}")
	}

	defaults := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
	}

	var result strings.Builder
	for i := 0; i < len(target); {
		if target[i] != '$' {
			result.WriteByte(target[i])
			i++
			continue
		}

		name, end, err := environmentReference(target, i)
		if err != nil {
			return "", err
		}
		value, exists := os.LookupEnv(name)
		if !exists || value == "" {
			value, exists = defaults[name]
		}
		if !exists {
			return "", fmt.Errorf("target references unset environment variable %q", name)
		}
		result.WriteString(value)
		i = end
	}

	return result.String(), nil
}

func environmentReference(value string, start int) (string, int, error) {
	i := start + 1
	if i >= len(value) {
		return "", 0, errors.New("target contains incomplete environment variable")
	}

	if value[i] == '{' {
		end := strings.IndexByte(value[i+1:], '}')
		if end < 0 {
			return "", 0, errors.New("target contains unclosed environment variable")
		}
		end += i + 1
		name := value[i+1 : end]
		if !validEnvironmentName(name) {
			return "", 0, fmt.Errorf("target contains invalid environment variable %q", name)
		}
		return name, end + 1, nil
	}

	end := i
	for end < len(value) && environmentNameByte(value[end], end == i) {
		end++
	}
	if end == i {
		return "", 0, errors.New("target environment variables must use $VAR or ${VAR}")
	}
	return value[i:end], end, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		if !environmentNameByte(name[i], i == 0) {
			return false
		}
	}
	return true
}

func environmentNameByte(value byte, first bool) bool {
	if value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' {
		return true
	}
	return !first && value >= '0' && value <= '9'
}

func within(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func resolveExistingPath(path string) (string, error) {
	remainder := make([]string, 0)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(remainder) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, remainder[i])
			}
			return resolved, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		remainder = append(remainder, filepath.Base(path))
		path = parent
	}
}

func absolutePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	return filepath.Clean(path), nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func xdgConfigHome(home string) string {
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		return value
	}
	return filepath.Join(home, ".config")
}

func Init(path string) (string, error) {
	if path == "" {
		path = FileName
	}
	path, err := absolutePath(path)
	if err != nil {
		return "", err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("config %q already exists", path)
		}
		return "", fmt.Errorf("create config %q: %w", path, err)
	}

	if _, err := file.WriteString(Example); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write config %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close config %q: %w", path, err)
	}
	return path, nil
}
