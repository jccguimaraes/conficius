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
		var errs []error
		for _, name := range names {
			candidate, exists := byName[name]
			if !exists {
				errs = append(errs, fmt.Errorf("project %q not found in profile %q", name, profile.Name))
				continue
			}
			scope, err := projectScope(candidate.Name, candidate.Path)
			if err != nil {
				errs = append(errs, fmt.Errorf("project %q: %w", candidate.Name, err))
				continue
			}
			selected = append(selected, scope)
		}
		if err := errors.Join(errs...); err != nil {
			return nil, err
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
			scope, err := projectScope("", absolute)
			if err != nil {
				return nil, fmt.Errorf("project %q: %w", path, err)
			}
			scopes = append(scopes, scope)
		}
		return uniqueScopes(scopes)
	}
	if len(profile.Profile.Projects) > 0 {
		scopes := []Scope{{Kind: "global"}}
		var errs []error
		for _, candidate := range profile.Profile.Projects {
			scope, err := projectScope(candidate.Name, candidate.Path)
			if err != nil {
				errs = append(errs, fmt.Errorf("project %q: %w", candidate.Name, err))
				continue
			}
			scopes = append(scopes, scope)
		}
		if err := errors.Join(errs...); err != nil {
			return nil, err
		}
		return uniqueScopes(scopes)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	if hasProjectManifest(cwd) {
		scope, err := projectScope("", cwd)
		if err != nil {
			return nil, err
		}
		return []Scope{{Kind: "global"}, scope}, nil
	}
	return []Scope{{Kind: "global"}}, nil
}

func projectScope(name, path string) (Scope, error) {
	root, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return Scope{}, fmt.Errorf("resolve root %q: %w", path, err)
	}
	workspaces, err := npmWorkspaces(root)
	if err != nil {
		return Scope{}, err
	}
	return Scope{Kind: "project", Name: name, Path: root, Workspaces: workspaces}, nil
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

func npmWorkspaces(root string) ([]string, error) {
	root, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("resolve npm workspace root %q: %w", root, err)
	}
	manifest := filepath.Join(root, "package.json")
	contents, err := os.ReadFile(manifest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read npm manifest %q: %w", manifest, err)
	}
	var document struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal(contents, &document) != nil || len(document.Workspaces) == 0 {
		return nil, nil
	}
	var patterns []string
	if json.Unmarshal(document.Workspaces, &patterns) != nil {
		var object struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(document.Workspaces, &object) != nil {
			return nil, nil
		}
		patterns = object.Packages
	}
	workspaceSet := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("match npm workspace pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			resolved, err := filepath.EvalSymlinks(match)
			if err != nil {
				return nil, fmt.Errorf("resolve npm workspace %q: %w", match, err)
			}
			if !within(root, resolved) {
				return nil, fmt.Errorf("npm workspace %q resolves outside root %q", match, root)
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return nil, fmt.Errorf("inspect npm workspace %q: %w", resolved, err)
			}
			if !info.IsDir() {
				continue
			}
			manifest := filepath.Join(resolved, "package.json")
			info, err = os.Stat(manifest)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("inspect npm workspace manifest %q: %w", manifest, err)
			}
			if info.IsDir() {
				continue
			}
			workspaceSet[resolved] = struct{}{}
		}
	}
	workspaces := make([]string, 0, len(workspaceSet))
	for workspace := range workspaceSet {
		workspaces = append(workspaces, workspace)
	}
	sort.Strings(workspaces)
	return workspaces, nil
}
