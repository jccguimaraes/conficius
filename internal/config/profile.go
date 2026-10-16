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
	Managers map[string]ManagerSettings `toml:"managers" json:"managers"`
	Projects []ProfileProject           `toml:"projects" json:"projects"`
}

type ManagerSettings struct {
	Enabled *bool `toml:"enabled" json:"enabled,omitempty"`
}

type ProfileProject struct {
	Name                 string `toml:"name" json:"name"`
	Path                 string `toml:"path" json:"path"`
	AllowManifestChanges bool   `toml:"allow_manifest_changes" json:"allow_manifest_changes"`
	ResolvedPath         string `toml:"-" json:"resolved_path"`
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
	return resolved, nil
}

func defaultProfile() Profile {
	return Profile{Managers: map[string]ManagerSettings{}}
}

func normalizeProfile(profile *Profile, root string) error {
	for manager := range profile.Managers {
		if !supportedManager(manager) {
			return fmt.Errorf("unsupported manager %q", manager)
		}
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
		if strings.TrimSpace(project.Path) == "" {
			return fmt.Errorf("project %q path must not be empty", project.Name)
		}
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
	}
	return nil
}

func supportedManager(manager string) bool {
	return manager == "mise" || manager == "brew" || manager == "npm" || manager == "mas" || manager == "uv" || manager == "pipx" || manager == "poetry" || manager == "pnpm" || manager == "cargo" || manager == "yarn" || manager == "bun"
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
