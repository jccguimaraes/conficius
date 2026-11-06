package packagemanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

type helmPrepared struct {
	chart    helmChart
	expected map[string]string
	edits    map[string]string
}

func helmPrepare(plan UpdatePlan, verify bool) (helmPrepared, error) {
	var p helmPrepared
	if plan.Manager != PM_HELM || len(plan.Packages) == 0 || plan.RunAs != "" {
		return p, fmt.Errorf("helm: invalid or run-as plan")
	}
	c, err := helmRead(Scope{Kind: plan.Scope, Path: plan.Directory})
	if err != nil {
		return p, err
	}
	if len(c.locked) != len(c.deps) {
		return p, fmt.Errorf("helm: complete Chart.lock required; refresh dependencies with native Helm first")
	}
	p.chart = c
	p.expected = map[string]string{}
	p.edits = map[string]string{}
	seenNames := map[string]bool{}
	for _, d := range c.deps {
		if _, err = helmHTTPS(d.Repository); err != nil {
			return p, err
		}
		if seenNames[d.Name] {
			return p, fmt.Errorf("helm: repeated real chart names unsupported")
		}
		seenNames[d.Name] = true
		match, e := helmConstraint(d.Version)
		if e != nil {
			return p, e
		}
		current, _ := helmCurrent(c, d)
		if !helmStable(current) || !match(current) {
			return p, fmt.Errorf("helm: concrete compatible locked version required for %s", d.Name)
		}
		p.expected[helmKey(d)] = current
	}
	seen := map[string]bool{}
	for _, pkg := range plan.Packages {
		if pkg.Manager != PM_HELM || pkg.Manifest != c.path || seen[pkg.Name] || !helmStable(pkg.Latest) || !helmStable(pkg.Current) || helmCompare(pkg.Latest, pkg.Current) <= 0 {
			return p, fmt.Errorf("helm: invalid selected dependency %s", pkg.Name)
		}
		seen[pkg.Name] = true
		found := false
		for _, d := range c.deps {
			if pkg.Name != helmIdentity(d) {
				continue
			}
			found = true
			current, _ := helmCurrent(c, d)
			want := pkg.Current
			if verify {
				want = pkg.Latest
			}
			if current != want {
				return p, fmt.Errorf("helm: dependency state changed: %s", pkg.Name)
			}
			if pkg.Requested == "" {
				return p, fmt.Errorf("helm: approved declared range required")
			}
			match, e := helmConstraint(pkg.Requested)
			if e != nil {
				return p, e
			}
			targetRange := pkg.Requested
			if !match(pkg.Latest) {
				if !plan.Bump {
					return p, fmt.Errorf("helm: Update target outside declared range")
				}
				targetRange = pkg.Latest
				p.edits[helmKey(d)] = targetRange
			}
			expectedRange := pkg.Requested
			if verify {
				expectedRange = targetRange
			}
			if d.Version != expectedRange {
				return p, fmt.Errorf("helm: declared range changed")
			}
			p.expected[helmKey(d)] = pkg.Latest
		}
		if !found {
			return p, fmt.Errorf("helm: dependency not declared: %s", pkg.Name)
		}
	}
	if len(c.locked) != 0 && len(c.locked) != len(c.deps) {
		return p, fmt.Errorf("helm: lock dependency set differs from manifest")
	}
	return p, nil
}
func helmVerify(plan UpdatePlan) error { _, err := helmPrepare(plan, true); return err }

func helmApply(ctx context.Context, plan UpdatePlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := helmRequire(plan.Executable); err != nil {
		return err
	}
	if !slices.Equal(plan.Args, (Helm{}).updateArgs(plan.Packages)) {
		return fmt.Errorf("helm: invalid dependency update arguments")
	}
	temp, err := os.MkdirTemp("", "conficius-helm-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	original, err := os.OpenRoot(plan.Directory)
	if err != nil {
		return err
	}
	defer func() { _ = original.Close() }()
	before, err := helmApplySnapshot(original)
	if err != nil {
		return err
	}
	p, err := helmPrepare(plan, false)
	if err != nil {
		return err
	}
	preparedSnapshot, err := helmApplySnapshot(original)
	if err != nil {
		return err
	}
	if !helmApplySame(before, preparedSnapshot) {
		return fmt.Errorf("helm: original files changed during preparation")
	}
	if !bytes.Equal(before["Chart.yaml"].data, p.chart.data) {
		return fmt.Errorf("helm: Chart.yaml changed during preparation")
	}
	data, err := helmEditVersions(p.chart, p.edits)
	if err != nil {
		return err
	}
	stage := filepath.Join(temp, "chart")
	if err = os.Mkdir(stage, 0o700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "Chart.yaml"), data, p.chart.mode.Perm()); err != nil {
		return err
	}
	if lock, ok := before["Chart.lock"]; ok {
		if err = os.WriteFile(filepath.Join(stage, "Chart.lock"), lock.data, lock.mode.Perm()); err != nil {
			return err
		}
	}
	type repository struct {
		Name string `yaml:"name"`
		URL  string `yaml:"url"`
	}
	config := struct {
		APIVersion   string       `yaml:"apiVersion"`
		Repositories []repository `yaml:"repositories"`
	}{APIVersion: "v1"}
	indexes := map[string]helmIndex{}
	names := map[string]string{}
	fetched := map[string]helmIndex{}
	for _, d := range p.chart.deps {
		index, ok := fetched[d.Repository]
		if !ok {
			index, err = helmFetchIndex(ctx, d.Repository)
			if err != nil {
				return err
			}
			fetched[d.Repository] = index
		}
		name, ok := names[d.Repository]
		if !ok {
			name = fmt.Sprintf("conficius-%d", len(names))
			names[d.Repository] = name
			config.Repositories = append(config.Repositories, repository{name, d.Repository})
			indexes[name] = helmIndex{APIVersion: "v1", Entries: map[string][]helmEntry{}}
		}
		var selected *helmEntry
		for _, entry := range index.Entries[d.Name] {
			if entry.Version == p.expected[helmKey(d)] {
				if selected != nil {
					return fmt.Errorf("helm: duplicate index version")
				}
				copy := entry
				selected = &copy
			}
		}
		if selected == nil || len(selected.URLs) == 0 {
			return fmt.Errorf("helm: approved chart version unavailable: %s", d.Name)
		}
		if selected.Name != d.Name {
			return fmt.Errorf("helm: index chart name mismatch")
		}
		base, _ := helmHTTPS(strings.TrimRight(d.Repository, "/") + "/")
		for i, raw := range selected.URLs {
			u, e := base.Parse(raw)
			if e != nil {
				return e
			}
			if _, e = helmHTTPS(u.String()); e != nil {
				return e
			}
			selected.URLs[i] = u.String()
		}
		indexes[name].Entries[d.Name] = []helmEntry{*selected}
	}
	for name, index := range indexes {
		b, e := yaml.Marshal(index)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(temp, name+"-index.yaml"), b, 0o600); e != nil {
			return e
		}
	}
	b, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	configPath := filepath.Join(temp, "repositories.yaml")
	if err = os.WriteFile(configPath, b, 0o600); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	cmd := commandContext(ctx, plan.Executable, "dependency", "update", ".", "--skip-refresh", "--repository-config", configPath, "--repository-cache", temp, "--registry-config", filepath.Join(temp, "registry.json"))
	cmd.Dir = stage
	cmd.Env = managerEnvironment("")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm: native dependency update failed; original files unchanged: %w: %s", err, strings.TrimSpace(string(output)))
	}
	stagedRoot, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer func() { _ = stagedRoot.Close() }()
	staged, err := helmApplySnapshot(stagedRoot)
	if err != nil {
		return fmt.Errorf("helm: unsafe staged output: %w", err)
	}
	if !bytes.Equal(staged["Chart.yaml"].data, data) {
		return fmt.Errorf("helm: native Helm changed staged Chart.yaml")
	}
	after, err := helmRead(Scope{Kind: plan.Scope, Path: stage})
	if err != nil {
		return err
	}
	if len(after.locked) != len(p.expected) {
		return fmt.Errorf("helm: unexpected staged lock dependency set")
	}
	expectedFiles := map[string]bool{"Chart.yaml": true, "Chart.lock": true, "charts": true}
	for _, d := range after.deps {
		v, source := helmCurrent(after, d)
		if source != "lockfile" || v != p.expected[helmKey(d)] {
			return fmt.Errorf("helm: unexpected staged lock resolution for %s", d.Name)
		}
		name := filepath.Join("charts", d.Name+"-"+v+".tgz")
		if _, ok := staged[name]; !ok {
			return fmt.Errorf("helm: missing staged archive %s", name)
		}
		expectedFiles[name] = true
	}
	for name := range staged {
		if !expectedFiles[name] {
			return fmt.Errorf("helm: unexpected staged output %s", name)
		}
	}
	current, err := helmApplySnapshot(original)
	if err != nil {
		return err
	}
	if !helmApplySame(before, current) {
		return fmt.Errorf("helm: original files changed during native update; nothing published")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	publishNames := make([]string, 0, len(staged))
	for name := range staged {
		if name != "charts" && name != "Chart.yaml" && name != "Chart.lock" {
			publishNames = append(publishNames, name)
		}
	}
	sort.Strings(publishNames)
	publishNames = append(publishNames, "Chart.lock", "Chart.yaml")
	published := []string{}
	if _, ok := before["charts"]; !ok {
		if err = original.Mkdir("charts", 0o755); err != nil {
			return fmt.Errorf("helm: create charts: %w; no files published", err)
		}
	}
	for _, name := range publishNames {
		file := staged[name]
		if old, ok := before[name]; ok {
			file.mode = old.mode
			if bytes.Equal(old.data, file.data) {
				continue
			}
		}
		if err = helmApplyPrecheck(original, name, before); err != nil {
			return fmt.Errorf("helm: publish %s blocked: %w; already published: %v", name, err, published)
		}
		if err = helmApplyPublish(original, name, file); err != nil {
			return fmt.Errorf("helm: publish %s failed: %w; already published: %v", name, err, published)
		}
		published = append(published, name)
	}
	for _, d := range p.chart.deps {
		oldVersion, _ := helmCurrent(p.chart, d)
		if oldVersion == p.expected[helmKey(d)] {
			continue
		}
		name := filepath.Join("charts", d.Name+"-"+oldVersion+".tgz")
		if _, ok := before[name]; !ok {
			continue
		}
		if err = helmApplyPrecheck(original, name, before); err != nil {
			return fmt.Errorf("helm: remove superseded archive %s blocked: %w; already published: %v", name, err, published)
		}
		if err = original.Remove(name); err != nil {
			return fmt.Errorf("helm: remove superseded archive %s failed: %w; already published: %v", name, err, published)
		}
		published = append(published, "removed "+name)
	}
	return nil
}

func helmApplyPrecheck(root *os.Root, name string, before map[string]helmApplyFile) error {
	current, err := helmApplyRead(root, name, 64<<20)
	old, existed := before[name]
	if !existed && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !existed || old.mode != current.mode || !bytes.Equal(old.data, current.data) {
		return fmt.Errorf("helm: original file changed: %s", name)
	}
	return nil
}

type helmApplyFile struct {
	data []byte
	mode os.FileMode
}

func helmApplyRead(root *os.Root, name string, limit int64) (helmApplyFile, error) {
	var file helmApplyFile
	info, err := root.Lstat(name)
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return file, fmt.Errorf("helm: unsafe or oversized file %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return file, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return file, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return file, fmt.Errorf("helm: file changed while opening %s", name)
	}
	file.data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return file, err
	}
	if int64(len(file.data)) > limit {
		return file, fmt.Errorf("helm: oversized file %s", name)
	}
	file.mode = info.Mode()
	return file, nil
}

func helmApplySnapshot(root *os.Root) (map[string]helmApplyFile, error) {
	files := map[string]helmApplyFile{}
	for _, name := range []string{"Chart.yaml", "Chart.lock"} {
		file, err := helmApplyRead(root, name, 8<<20)
		if os.IsNotExist(err) && name == "Chart.lock" {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[name] = file
	}
	info, err := root.Lstat("charts")
	if os.IsNotExist(err) {
		return files, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("helm: charts must be a real directory")
	}
	files["charts"] = helmApplyFile{mode: info.Mode()}
	dir, err := root.Open("charts")
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(4097)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("helm: too many vendored chart files")
	}
	total := 0
	for _, entry := range entries {
		name := filepath.Join("charts", entry.Name())
		if entry.IsDir() {
			return nil, fmt.Errorf("helm: ambiguous vendored chart directory %s; archive files required", name)
		}
		file, err := helmApplyRead(root, name, 64<<20)
		if err != nil {
			return nil, err
		}
		total += len(file.data)
		if total > 256<<20 {
			return nil, fmt.Errorf("helm: vendored chart files exceed size limit")
		}
		files[name] = file
	}
	return files, nil
}

func helmApplySame(a, b map[string]helmApplyFile) bool {
	if len(a) != len(b) {
		return false
	}
	for name, file := range a {
		other, ok := b[name]
		if !ok || file.mode != other.mode || !bytes.Equal(file.data, other.data) {
			return false
		}
	}
	return true
}

func helmApplyPublish(root *os.Root, name string, file helmApplyFile) error {
	temp := filepath.Join(filepath.Dir(name), ".conficius-"+rand.Text())
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.mode.Perm())
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	_, err = f.Write(file.data)
	if err == nil {
		err = f.Chmod(file.mode.Perm())
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, name)
}
