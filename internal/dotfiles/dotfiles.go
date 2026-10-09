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
	Name              string      `json:"name"`
	Source            string      `json:"source"`
	Target            string      `json:"target"`
	State             State       `json:"state"`
	Action            string      `json:"action,omitempty"`
	Repairable        bool        `json:"repairable"`
	LinkDestination   string      `json:"link_destination,omitempty"`
	SourceDestination string      `json:"source_destination"`
	TargetDestination string      `json:"target_destination,omitempty"`
	TargetInfo        os.FileInfo `json:"-"`
}

type Options struct {
	ConfigDir string
	HomeDir   string
	Platform  string
	Names     []string
	Exclude   []string
	Apply     bool
	Force     bool
	Preview   []Result
}

type prepared struct {
	entry      Entry
	source     string
	target     string
	state      State
	configDir  string
	homeDir    string
	result     Result
	sourceInfo os.FileInfo
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

	if options.Preview != nil {
		if len(options.Preview) != len(prepared) {
			return nil, errors.New("stale dotfile preview: selection changed")
		}
		for i, item := range prepared {
			expected := options.Preview[i]
			expected.Action = item.result.Action
			if !sameResult(expected, item.result) {
				return nil, fmt.Errorf("%s: stale dotfile preview: %s", item.entry.Name, item.target)
			}
		}
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
		sourceInfo, err := os.Stat(source)
		if err != nil {
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

		targetInfo, err := os.Lstat(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: inspect target: %w", entry.Name, err)
		}
		state, err := inspectState(source, target)
		if err != nil {
			return nil, fmt.Errorf("%s: inspect target: %w", entry.Name, err)
		}
		sourceDestination, err := filepath.EvalSymlinks(source)
		if err != nil {
			return nil, fmt.Errorf("%s: resolve source: %w", entry.Name, err)
		}
		item := prepared{entry: entry, source: source, target: target, state: state, configDir: configDir, homeDir: homeDir, sourceInfo: sourceInfo}
		item.result = Result{Name: entry.Name, Source: source, Target: target, State: state, SourceDestination: sourceDestination, Action: "none"}
		item.result.TargetInfo = targetInfo
		if (state == StateMissing) != (targetInfo == nil) ||
			((state == StateHealthy || state == StateWrongLink) && targetInfo.Mode()&os.ModeSymlink == 0) {
			return nil, fmt.Errorf("%s: target changed during inspection: %s", entry.Name, target)
		}
		if state == StateHealthy || state == StateWrongLink {
			item.result.LinkDestination, err = os.Readlink(target)
			if err != nil {
				return nil, fmt.Errorf("%s: read existing symlink: %w", entry.Name, err)
			}
		}
		if state == StateHealthy || state == StateWrongLink {
			destination := item.result.LinkDestination
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(filepath.Dir(target), destination)
			}
			item.result.TargetDestination, err = resolveWithMissingTail(destination)
			if err != nil {
				return nil, err
			}
		}
		switch state {
		case StateMissing:
			item.result.Repairable = true
			item.result.Action = "create"
		case StateWrongLink:
			item.result.Repairable, _ = symlinkDestinationInside(target, configDir)
			item.result.Action = "replace"
		}
		currentInfo, statErr := os.Lstat(target)
		if (targetInfo == nil && !errors.Is(statErr, os.ErrNotExist)) ||
			(targetInfo != nil && (statErr != nil || !os.SameFile(targetInfo, currentInfo))) {
			return nil, fmt.Errorf("%s: target changed during inspection: %s", entry.Name, target)
		}
		items = append(items, item)
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
	result := item.result
	if applying && result.Action != "none" {
		result.Action += "d"
	}
	return result
}

type ApplyError struct {
	Action            string
	Path              string
	Cause             error
	RollbackAttempted bool
	RollbackError     error
	AffectedPaths     []string
}

func (err *ApplyError) Error() string {
	if err.RollbackError != nil {
		return errors.Join(err.Cause, fmt.Errorf("rollback: %w", err.RollbackError)).Error()
	}
	return err.Cause.Error()
}

func (err *ApplyError) Unwrap() []error {
	if err.RollbackError != nil {
		return []error{err.Cause, err.RollbackError}
	}
	return []error{err.Cause}
}

func sameResult(left, right Result) bool {
	leftInfo, rightInfo := left.TargetInfo, right.TargetInfo
	left.TargetInfo, right.TargetInfo = nil, nil
	return left == right && ((leftInfo == nil && rightInfo == nil) ||
		(leftInfo != nil && rightInfo != nil && os.SameFile(leftInfo, rightInfo)))
}

func revalidate(item prepared) error {
	entry := item.entry
	entry.Platforms = nil
	items, err := prepare([]Entry{entry}, Options{ConfigDir: item.configDir, HomeDir: item.homeDir})
	if err != nil {
		return err
	}
	if len(items) != 1 || !sameResult(items[0].result, item.result) || !os.SameFile(item.sourceInfo, items[0].sourceInfo) {
		return fmt.Errorf("%s: stale dotfile preview: %s", item.entry.Name, item.target)
	}
	return nil
}

type operation struct {
	kind      string
	path      string
	oldTarget string
	newTarget string
	info      os.FileInfo
}

type operations struct {
	mkdir    func(string, os.FileMode) error
	symlink  func(string, string) error
	remove   func(string) error
	readlink func(string) (string, error)
	rename   func(string, string) error
	link     func(string, string) error
}

func osOperations() operations {
	return operations{mkdir: os.Mkdir, symlink: os.Symlink, remove: os.Remove, readlink: os.Readlink, rename: os.Rename, link: os.Link}
}

func apply(items []prepared, ops operations) error {
	journal := make([]operation, 0, len(items))
	fail := func(action, path string, cause error) error {
		paths := []string{}
		for _, entry := range journal {
			if !slices.Contains(paths, entry.path) {
				paths = append(paths, entry.path)
			}
		}
		return &ApplyError{Action: action, Path: path, Cause: cause, RollbackAttempted: len(journal) > 0, RollbackError: rollback(journal, ops), AffectedPaths: paths}
	}

	// Validate entire selection before any mutation.
	for _, item := range items {
		if err := revalidate(item); err != nil {
			return fail("revalidate", item.target, err)
		}
	}
	for _, item := range items {
		if err := revalidate(item); err != nil {
			return fail("revalidate", item.target, err)
		}
		if item.state == StateHealthy {
			continue
		}
		createdDirs, err := createParents(filepath.Dir(item.target), ops)
		for _, dir := range createdDirs {
			journal = append(journal, operation{kind: "directory", path: dir})
		}
		if err != nil {
			return fail("create target parents", filepath.Dir(item.target), fmt.Errorf("%s: create target parents: %w", item.entry.Name, err))
		}

		relative, err := filepath.Rel(filepath.Dir(item.target), item.source)
		if err != nil {
			return fail("compute relative link", item.target, fmt.Errorf("%s: compute relative link: %w", item.entry.Name, err))
		}

		if err := revalidate(item); err != nil {
			return fail("revalidate", item.target, err)
		}
		if item.state == StateWrongLink {
			if !item.result.Repairable {
				return fail("replace", item.target, fmt.Errorf("%s: refusing to replace symlink outside current repository: %s", item.entry.Name, item.target))
			}
			oldTarget, err := ops.readlink(item.target)
			if err != nil {
				return fail("read existing symlink", item.target, fmt.Errorf("%s: read existing symlink: %w", item.entry.Name, err))
			}
			if oldTarget != item.result.LinkDestination {
				return fail("revalidate", item.target, fmt.Errorf("%s: stale dotfile preview: %s", item.entry.Name, item.target))
			}
			if err := removeLink(item.target, item.result.TargetInfo, oldTarget, ops); err != nil {
				return fail("remove existing symlink", item.target, fmt.Errorf("%s: remove existing symlink: %w", item.entry.Name, err))
			}
			journal = append(journal, operation{kind: "replaced", path: item.target, oldTarget: oldTarget, newTarget: relative})
		}

		if err := ops.symlink(relative, item.target); err != nil {
			return fail("create symlink", item.target, fmt.Errorf("%s: create symlink: %w", item.entry.Name, err))
		}
		info, err := os.Lstat(item.target)
		if err != nil {
			return fail("inspect created symlink", item.target, err)
		}
		if item.state == StateMissing {
			journal = append(journal, operation{kind: "created", path: item.target, newTarget: relative, info: info})
		} else {
			journal[len(journal)-1].info = info
		}
	}
	return nil
}

// removeLink captures the directory entry before deciding whether it is safe to delete.
// ponytail: pathname operations cannot exclude hostile parent swaps; descriptor-relative
// platform APIs are needed for that guarantee. Keep unexpected captured objects intact.
func removeLink(path string, expected os.FileInfo, destination string, ops operations) (err error) {
	if expected == nil || expected.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to remove unknown symlink %s", path)
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".dotfiles-*")
	if err != nil {
		return err
	}
	captured := filepath.Join(dir, "target")
	defer func() {
		if cleanupErr := os.Remove(dir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("captured object retained at %s: %w", captured, cleanupErr))
		}
	}()
	if err := ops.rename(path, captured); err != nil {
		return err
	}
	info, statErr := os.Lstat(captured)
	text, readErr := os.Readlink(captured)
	if statErr != nil || info.Mode()&os.ModeSymlink == 0 || !os.SameFile(expected, info) || readErr != nil || text != destination {
		// Link fails if path appeared meanwhile; never rename over another writer.
		restoreErr := ops.link(captured, path)
		return errors.Join(fmt.Errorf("refusing to delete changed target %s; captured object retained at %s", path, captured), statErr, restoreErr)
	}
	if err := ops.remove(captured); err != nil {
		return errors.Join(err, ops.link(captured, path))
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
			currentTarget, err := ops.readlink(entry.path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || currentTarget != entry.newTarget {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing to remove changed rollback target %s", entry.path))
				continue
			}
			if err := removeLink(entry.path, entry.info, entry.newTarget, ops); err != nil {
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
				if err := removeLink(entry.path, entry.info, entry.newTarget, ops); err != nil {
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
			info, err := os.Lstat(entry.path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.IsDir() {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing to remove changed rollback directory %s", entry.path))
				continue
			}
			if err := ops.remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove rollback directory %s: %w", entry.path, err))
			}
		}
	}
	return rollbackErr
}
