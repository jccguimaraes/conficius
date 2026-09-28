package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ProfileEnvironment = "CONFICIUS_PROFILE"

var profileNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

type Profile struct {
	Mode          string                     `toml:"mode" json:"mode"`
	ModeSet       bool                       `toml:"-" json:"-"`
	SecurityScope string                     `toml:"security_scope" json:"security_scope"`
	ManagerOrder  []string                   `toml:"manager_order" json:"manager_order"`
	Managers      map[string]ManagerSettings `toml:"managers" json:"managers"`
	Packages      []PackageRule              `toml:"packages" json:"packages"`
	Projects      []ProfileProject           `toml:"projects" json:"projects"`
}

type ManagerSettings struct {
	Enabled               *bool  `toml:"enabled" json:"enabled,omitempty"`
	Executable            string `toml:"executable" json:"executable,omitempty"`
	IncludeBumpCandidates bool   `toml:"include_bump_candidates" json:"include_bump_candidates"`
	IncludeGreedy         bool   `toml:"include_greedy" json:"include_greedy"`
	ExpectedGlobalPrefix  string `toml:"expected_global_prefix" json:"expected_global_prefix,omitempty"`
}

type PackageRule struct {
	Manager        string `toml:"manager" json:"manager"`
	Name           string `toml:"name" json:"name"`
	Policy         string `toml:"policy" json:"policy"`
	DesiredVersion string `toml:"desired_version" json:"desired_version,omitempty"`
}

type ProfileProject struct {
	Name                 string        `toml:"name" json:"name"`
	Path                 string        `toml:"path" json:"path"`
	Mode                 string        `toml:"mode" json:"mode,omitempty"`
	AllowManifestChanges bool          `toml:"allow_manifest_changes" json:"allow_manifest_changes"`
	NPMDependencyClasses []string      `toml:"npm_dependency_classes" json:"npm_dependency_classes,omitempty"`
	Packages             []PackageRule `toml:"packages" json:"packages,omitempty"`
	ResolvedPath         string        `toml:"-" json:"resolved_path"`
}

type ResolvedProfile struct {
	SchemaVersion int               `json:"schema_version"`
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	Profile       Profile           `json:"profile"`
	Provenance    map[string]string `json:"provenance,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
}

func (project *Project) ResolveProfile(cliName string, verbose bool) (ResolvedProfile, error) {
	name := strings.TrimSpace(cliName)
	source := "command line"
	if name == "" {
		name = strings.TrimSpace(os.Getenv(ProfileEnvironment))
		source = ProfileEnvironment
	}
	if name == "" {
		name = strings.TrimSpace(project.Profile)
		source = project.Path
	}
	if name == "" {
		name = "default"
		source = "default"
	}
	if project.Version == 1 && name != "default" {
		return ResolvedProfile{}, fmt.Errorf("profile %q requires config version 2; run conficius config migrate", name)
	}
	if !profileNamePattern.MatchString(name) {
		return ResolvedProfile{}, fmt.Errorf("invalid profile name %q", name)
	}

	profile, exists := project.Profiles[name]
	if !exists {
		if name != "default" {
			return ResolvedProfile{}, fmt.Errorf("profile %q not found", name)
		}
		profile = defaultProfile()
	}
	if project.Version == 1 {
		profile = defaultProfile()
	}
	if err := normalizeProfile(&profile, project.Root); err != nil {
		return ResolvedProfile{}, fmt.Errorf("profile %q: %w", name, err)
	}

	resolved := ResolvedProfile{SchemaVersion: 1, Name: name, Source: source, Profile: profile}
	if verbose {
		resolved.Provenance = map[string]string{"profile": source, "config": project.Path}
	}
	if profile.Mode == "allowlist" && len(profile.Packages) == 0 && len(profile.Projects) == 0 {
		resolved.Warnings = append(resolved.Warnings, "allowlist profile selects no packages or projects")
	}
	return resolved, nil
}

func defaultProfile() Profile {
	return Profile{Mode: "denylist", ManagerOrder: []string{"mise", "brew", "npm"}, Managers: map[string]ManagerSettings{}}
}

func normalizeProfile(profile *Profile, root string) error {
	if profile.Mode == "" {
		profile.Mode = "denylist"
	}
	if profile.Mode != "allowlist" && profile.Mode != "denylist" {
		return fmt.Errorf("mode must be allowlist or denylist, got %q", profile.Mode)
	}
	if profile.SecurityScope == "" {
		profile.SecurityScope = "all"
	}
	if profile.SecurityScope != "all" && profile.SecurityScope != "selected" {
		return fmt.Errorf("security_scope must be all or selected, got %q", profile.SecurityScope)
	}
	if len(profile.ManagerOrder) == 0 {
		profile.ManagerOrder = []string{"mise", "brew", "npm"}
	}
	seenManagers := map[string]struct{}{}
	for i, manager := range profile.ManagerOrder {
		manager = strings.ToLower(strings.TrimSpace(manager))
		if !supportedManager(manager) {
			return fmt.Errorf("manager_order contains unsupported manager %q", manager)
		}
		if _, exists := seenManagers[manager]; exists {
			return fmt.Errorf("manager_order contains duplicate manager %q", manager)
		}
		seenManagers[manager] = struct{}{}
		profile.ManagerOrder[i] = manager
	}
	for manager, settings := range profile.Managers {
		if !supportedManager(manager) {
			return fmt.Errorf("unsupported manager %q", manager)
		}
		if settings.Executable != "" {
			executable, err := expandConfigPath(settings.Executable, root)
			if err != nil {
				return fmt.Errorf("manager %q executable: %w", manager, err)
			}
			if !filepath.IsAbs(executable) {
				return fmt.Errorf("manager %q executable must be absolute", manager)
			}
			settings.Executable = filepath.Clean(executable)
		}
		if settings.ExpectedGlobalPrefix != "" {
			prefix, err := expandConfigPath(settings.ExpectedGlobalPrefix, root)
			if err != nil {
				return fmt.Errorf("manager %q expected_global_prefix: %w", manager, err)
			}
			settings.ExpectedGlobalPrefix = filepath.Clean(prefix)
		}
		profile.Managers[manager] = settings
	}
	if err := normalizePackageRules(profile.Packages); err != nil {
		return err
	}
	seenProjects := map[string]struct{}{}
	seenPaths := map[string]string{}
	for i := range profile.Projects {
		project := &profile.Projects[i]
		if !profileNamePattern.MatchString(project.Name) {
			return fmt.Errorf("invalid project name %q", project.Name)
		}
		if _, exists := seenProjects[project.Name]; exists {
			return fmt.Errorf("duplicate project name %q", project.Name)
		}
		seenProjects[project.Name] = struct{}{}
		path, err := expandConfigPath(project.Path, root)
		if err != nil {
			return fmt.Errorf("project %q path: %w", project.Name, err)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		project.Path = filepath.Clean(path)
		project.ResolvedPath = project.Path
		if resolved, err := filepath.EvalSymlinks(project.Path); err == nil {
			project.ResolvedPath = resolved
		}
		if previous, exists := seenPaths[project.ResolvedPath]; exists {
			return fmt.Errorf("projects %q and %q resolve to duplicate path %q", previous, project.Name, project.ResolvedPath)
		}
		seenPaths[project.ResolvedPath] = project.Name
		if project.Mode != "" && project.Mode != "allowlist" && project.Mode != "denylist" {
			return fmt.Errorf("project %q mode must be allowlist or denylist", project.Name)
		}
		seenClasses := make(map[string]struct{}, len(project.NPMDependencyClasses))
		for classIndex, class := range project.NPMDependencyClasses {
			class = strings.ToLower(strings.TrimSpace(class))
			if class != "runtime" && class != "development" && class != "optional" && class != "peer" && class != "transitive" && class != "extraneous" {
				return fmt.Errorf("project %q has unsupported npm_dependency_classes value %q", project.Name, class)
			}
			if _, exists := seenClasses[class]; exists {
				return fmt.Errorf("project %q has duplicate npm_dependency_classes value %q", project.Name, class)
			}
			seenClasses[class] = struct{}{}
			project.NPMDependencyClasses[classIndex] = class
		}
		if err := normalizePackageRules(project.Packages); err != nil {
			return fmt.Errorf("project %q: %w", project.Name, err)
		}
	}
	return nil
}

func normalizePackageRules(rules []PackageRule) error {
	seen := map[string]struct{}{}
	for i := range rules {
		rule := &rules[i]
		rule.Manager = strings.ToLower(strings.TrimSpace(rule.Manager))
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Policy = strings.ToLower(strings.TrimSpace(rule.Policy))
		rule.DesiredVersion = strings.TrimSpace(rule.DesiredVersion)
		if !supportedManager(rule.Manager) || rule.Name == "" {
			return fmt.Errorf("package rule %d requires supported manager and name", i+1)
		}
		if rule.Policy != "include" && rule.Policy != "exclude" {
			return fmt.Errorf("package rule %d policy must be include or exclude", i+1)
		}
		if rule.Policy == "exclude" && rule.DesiredVersion != "" {
			return fmt.Errorf("excluded package %s:%s cannot set desired_version", rule.Manager, rule.Name)
		}
		key := rule.Manager + "\x00" + rule.Name
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate package rule for %s:%s", rule.Manager, rule.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func supportedManager(manager string) bool {
	return manager == "mise" || manager == "brew" || manager == "npm" || manager == "mas" || manager == "uv"
}

func expandConfigPath(value, root string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	expanded, err := expandTarget(value, home)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(root, expanded)
	}
	return filepath.Abs(expanded)
}

var errMigrationSymlink = errors.New("refusing to migrate symlinked config")
