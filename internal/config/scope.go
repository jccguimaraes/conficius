package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Scope struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name,omitempty"`
	Path       string   `json:"path,omitempty"`
	Workspaces []string `json:"workspaces,omitempty"`
}

func ResolveScopes(project *Project, profile ResolvedProfile, global bool, paths, names []string) ([]Scope, error) {
	if global && len(paths) > 0 {
		return nil, errors.New("--global cannot be combined with project paths")
	}
	if global {
		return []Scope{{Kind: "global"}}, nil
	}
	if len(names) > 0 {
		selected := make([]Scope, 0, len(names))
		byName := make(map[string]ProfileProject, len(profile.Profile.Projects))
		for _, candidate := range profile.Profile.Projects {
			byName[candidate.Name] = candidate
		}
		for _, name := range names {
			candidate, exists := byName[name]
			if !exists {
				return nil, fmt.Errorf("project %q not found in profile %q", name, profile.Name)
			}
			selected = append(selected, projectScope(candidate.Name, candidate.Path))
		}
		return uniqueScopes(selected)
	}
	if len(paths) > 0 {
		scopes := make([]Scope, 0, len(paths))
		for _, path := range paths {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return nil, fmt.Errorf("resolve project path %q: %w", path, err)
			}
			scopes = append(scopes, projectScope("", absolute))
		}
		return uniqueScopes(scopes)
	}
	if len(profile.Profile.Projects) > 0 {
		scopes := []Scope{{Kind: "global"}}
		for _, candidate := range profile.Profile.Projects {
			scopes = append(scopes, projectScope(candidate.Name, candidate.Path))
		}
		return uniqueScopes(scopes)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	if hasProjectManifest(cwd) {
		return []Scope{{Kind: "global"}, projectScope("", cwd)}, nil
	}
	return []Scope{{Kind: "global"}}, nil
}

func projectScope(name, path string) Scope {
	resolved := filepath.Clean(path)
	if target, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = target
	}
	return Scope{Kind: "project", Name: name, Path: resolved, Workspaces: npmWorkspaces(resolved)}
}

func uniqueScopes(scopes []Scope) ([]Scope, error) {
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		key := scope.Kind + "\x00" + scope.Path
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate %s scope %q", scope.Kind, scope.Path)
		}
		seen[key] = struct{}{}
	}
	return scopes, nil
}

func hasProjectManifest(path string) bool {
	for _, name := range []string{"package.json", "mise.toml", ".mise.toml"} {
		if isFile(filepath.Join(path, name)) {
			return true
		}
	}
	return false
}

func npmWorkspaces(root string) []string {
	contents, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var document struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal(contents, &document) != nil || len(document.Workspaces) == 0 {
		return nil
	}
	var patterns []string
	if json.Unmarshal(document.Workspaces, &patterns) != nil {
		var object struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(document.Workspaces, &object) != nil {
			return nil
		}
		patterns = object.Packages
	}
	var workspaces []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			resolved, err := filepath.EvalSymlinks(match)
			if err != nil || !within(root, resolved) || !isFile(filepath.Join(resolved, "package.json")) {
				continue
			}
			workspaces = append(workspaces, resolved)
		}
	}
	sort.Strings(workspaces)
	return workspaces
}
