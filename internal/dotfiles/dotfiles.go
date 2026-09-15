package dotfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type Entry struct {
	Name      string
	Source    string
	Target    string
	Platforms []string
}

type State string

const (
	StateHealthy   State = "healthy"
	StateMissing   State = "missing"
	StateWrongLink State = "wrong_link"
	StateConflict  State = "conflict"
)

type Result struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Target string `json:"target"`
	State  State  `json:"state"`
	Action string `json:"action,omitempty"`
}

type Options struct {
	ConfigDir string
	HomeDir   string
	Platform  string
	Names     []string
	Exclude   []string
	Apply     bool
	Force     bool
}

type prepared struct {
	entry  Entry
	source string
	target string
	state  State
}

func Inspect(entries []Entry, options Options) ([]Result, error) {
	prepared, err := prepare(entries, options)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(prepared))
	for _, item := range prepared {
		results = append(results, resultFor(item, false))
	}
	return results, nil
}

func Link(entries []Entry, options Options) ([]Result, error) {
	prepared, err := prepare(entries, options)
	if err != nil {
		return nil, err
	}

	for _, item := range prepared {
		if item.state == StateConflict {
			return nil, fmt.Errorf("%s: target exists and is not a symlink: %s", item.entry.Name, item.target)
		}
		if item.state == StateWrongLink && !options.Force {
			return nil, fmt.Errorf("%s: target is a different symlink (use --force to replace repository links): %s", item.entry.Name, item.target)
		}
		if item.state == StateWrongLink {
			inside, err := symlinkDestinationInside(item.target, options.ConfigDir)
			if err != nil {
				return nil, fmt.Errorf("%s: resolve existing symlink: %w", item.entry.Name, err)
			}
			if !inside {
				return nil, fmt.Errorf("%s: refusing to replace symlink outside current repository: %s", item.entry.Name, item.target)
			}
		}
	}

	results := make([]Result, 0, len(prepared))
	for _, item := range prepared {
		results = append(results, resultFor(item, options.Apply))
	}
	if !options.Apply {
		return results, nil
	}

	if err := apply(prepared, osOperations()); err != nil {
		return nil, err
	}
	return results, nil
}

func prepare(entries []Entry, options Options) ([]prepared, error) {
	configDir, err := filepath.Abs(options.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("resolve config directory: %w", err)
	}
	homeDir, err := filepath.Abs(options.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	selected, err := selectEntries(entries, options.Names, options.Exclude, options.Platform)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Name != selected[j].Name {
			return selected[i].Name < selected[j].Name
		}
		if selected[i].Target != selected[j].Target {
			return selected[i].Target < selected[j].Target
		}
		return selected[i].Source < selected[j].Source
	})

	items := make([]prepared, 0, len(selected))
	targets := make(map[string]string, len(selected))
	for _, entry := range selected {
		source, err := rootedPath(configDir, entry.Source)
		if err != nil {
			return nil, fmt.Errorf("%s: source: %w", entry.Name, err)
		}
		if _, err := os.Stat(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%s: source does not exist: %s", entry.Name, source)
			}
			return nil, fmt.Errorf("%s: inspect source: %w", entry.Name, err)
		}
		if err := ensureResolvedInside(source, configDir); err != nil {
			return nil, fmt.Errorf("%s: source: %w", entry.Name, err)
		}

		target, err := homePath(homeDir, entry.Target)
		if err != nil {
			return nil, fmt.Errorf("%s: target: %w", entry.Name, err)
		}
		if previous, exists := targets[target]; exists {
			return nil, fmt.Errorf("%s and %s use the same target: %s", previous, entry.Name, target)
		}
		targets[target] = entry.Name
		if err := ensureExistingParentInside(target, homeDir); err != nil {
			return nil, fmt.Errorf("%s: target: %w", entry.Name, err)
		}

		state, err := inspectState(source, target)
		if err != nil {
			return nil, fmt.Errorf("%s: inspect target: %w", entry.Name, err)
		}
		items = append(items, prepared{entry: entry, source: source, target: target, state: state})
	}
	return items, nil
}

func selectEntries(entries []Entry, names, excluded []string, platform string) ([]Entry, error) {
	byName := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Name == "" {
			return nil, errors.New("dotfile name is empty")
		}
		byName[entry.Name] = true
	}

	for _, name := range append(slices.Clone(names), excluded...) {
		if !byName[name] {
			return nil, fmt.Errorf("unknown dotfile: %s", name)
		}
	}

	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	skipped := make(map[string]bool, len(excluded))
	for _, name := range excluded {
		skipped[name] = true
	}

	selected := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if len(wanted) > 0 && !wanted[entry.Name] {
			continue
		}
		if skipped[entry.Name] || len(entry.Platforms) > 0 && !slices.Contains(entry.Platforms, platform) {
			continue
		}
		selected = append(selected, entry)
	}
	return selected, nil
}

func rootedPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !inside(path, root) || path == root {
		return "", fmt.Errorf("path is outside config directory: %s", path)
	}
	return path, nil
}

func homePath(home, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	path = filepath.Clean(path)
	if !inside(path, home) || path == home {
		return "", fmt.Errorf("path is outside home directory: %s", path)
	}
	return path, nil
}

func inside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func ensureResolvedInside(path, root string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if !inside(resolvedPath, resolvedRoot) {
		return fmt.Errorf("resolved path is outside config directory: %s", resolvedPath)
	}
	return nil
}

func ensureExistingParentInside(target, home string) error {
	parent := filepath.Dir(target)
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return errors.New("no existing target parent")
		}
		parent = next
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve target parent: %w", err)
	}
	if !inside(resolvedParent, resolvedHome) {
		return fmt.Errorf("resolved target parent is outside home directory: %s", resolvedParent)
	}
	return nil
}

func inspectState(source, target string) (State, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return StateMissing, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return StateConflict, nil
	}

	same, err := sameResolvedPath(source, target)
	if err != nil {
		return StateWrongLink, nil
	}
	if same {
		return StateHealthy, nil
	}
	return StateWrongLink, nil
}

func sameResolvedPath(left, right string) (bool, error) {
	resolvedLeft, err := filepath.EvalSymlinks(left)
	if err != nil {
		return false, err
	}
	resolvedRight, err := filepath.EvalSymlinks(right)
	if err != nil {
		return false, err
	}
	return filepath.Clean(resolvedLeft) == filepath.Clean(resolvedRight), nil
}

func symlinkDestinationInside(link, root string) (bool, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	destination, err := os.Readlink(link)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(filepath.Dir(link), destination)
	}
	resolvedDestination, err := resolveWithMissingTail(destination)
	if err != nil {
		return false, err
	}
	return inside(resolvedDestination, resolvedRoot), nil
}

func resolveWithMissingTail(path string) (string, error) {
	missing := make([]string, 0)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		path = parent
	}
}

func resultFor(item prepared, applying bool) Result {
	action := "none"
	switch item.state {
	case StateMissing:
		action = "create"
	case StateWrongLink:
		action = "replace"
	}
	if applying && action != "none" {
		action += "d"
	}
	return Result{Name: item.entry.Name, Source: item.source, Target: item.target, State: item.state, Action: action}
}

type operation struct {
	kind      string
	path      string
	oldTarget string
	newTarget string
}

type operations struct {
	mkdir    func(string, os.FileMode) error
	symlink  func(string, string) error
	remove   func(string) error
	readlink func(string) (string, error)
}

func osOperations() operations {
	return operations{mkdir: os.Mkdir, symlink: os.Symlink, remove: os.Remove, readlink: os.Readlink}
}

func apply(items []prepared, ops operations) error {
	journal := make([]operation, 0, len(items))
	fail := func(err error) error {
		if rollbackErr := rollback(journal, ops); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}

	for _, item := range items {
		if item.state == StateHealthy {
			continue
		}
		createdDirs, err := createParents(filepath.Dir(item.target), ops)
		for _, dir := range createdDirs {
			journal = append(journal, operation{kind: "directory", path: dir})
		}
		if err != nil {
			return fail(fmt.Errorf("%s: create target parents: %w", item.entry.Name, err))
		}

		relative, err := filepath.Rel(filepath.Dir(item.target), item.source)
		if err != nil {
			return fail(fmt.Errorf("%s: compute relative link: %w", item.entry.Name, err))
		}

		if item.state == StateWrongLink {
			oldTarget, err := ops.readlink(item.target)
			if err != nil {
				return fail(fmt.Errorf("%s: read existing symlink: %w", item.entry.Name, err))
			}
			if err := ops.remove(item.target); err != nil {
				return fail(fmt.Errorf("%s: remove existing symlink: %w", item.entry.Name, err))
			}
			journal = append(journal, operation{kind: "replaced", path: item.target, oldTarget: oldTarget, newTarget: relative})
		}

		if err := ops.symlink(relative, item.target); err != nil {
			return fail(fmt.Errorf("%s: create symlink: %w", item.entry.Name, err))
		}
		if item.state == StateMissing {
			journal = append(journal, operation{kind: "created", path: item.target})
		}
	}
	return nil
}

func createParents(path string, ops operations) ([]string, error) {
	missing := []string{}
	for current := path; ; current = filepath.Dir(current) {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
	}

	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := ops.mkdir(missing[i], 0o755); err != nil {
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func rollback(journal []operation, ops operations) error {
	var rollbackErr error
	for i := len(journal) - 1; i >= 0; i-- {
		entry := journal[i]
		switch entry.kind {
		case "created":
			if err := ops.remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		case "replaced":
			info, err := os.Lstat(entry.path)
			if err == nil {
				if info.Mode()&os.ModeSymlink == 0 {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing to remove changed rollback target %s", entry.path))
					continue
				}
				currentTarget, readErr := ops.readlink(entry.path)
				if readErr != nil || currentTarget != entry.newTarget {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing to remove changed rollback symlink %s", entry.path))
					continue
				}
				if err := ops.remove(entry.path); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
					continue
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, err)
				continue
			}
			if err := ops.symlink(entry.oldTarget, entry.path); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		case "directory":
			// Non-empty means concurrent content appeared; preserve it without treating safe preservation as rollback failure.
			if err := ops.remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrExist) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
	}
	return rollbackErr
}
