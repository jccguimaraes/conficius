package packagemanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

const PM_HELM = "helm"

type Helm struct{}

var _ managerAdapter = Helm{}

func (Helm) name() string { return PM_HELM }
func (Helm) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate}
}
func (Helm) testedVersions() VersionRange             { return VersionRange{MinimumMajor: 3, MaximumMajor: 4} }
func (Helm) updateImpact(Scope) MutationImpact        { return MutationImpact{MayChangeManifests: true} }
func (Helm) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Helm) projectFiles() []string                   { return []string{"Chart.yaml", "Chart.lock"} }
func (Helm) updateArgs([]Package) []string {
	return []string{"dependency", "update", "--skip-refresh"}
}

type helmDependency struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
	Alias      string `yaml:"alias,omitempty"`
	node       *yaml.Node
}
type helmChart struct {
	path   string
	data   []byte
	mode   os.FileMode
	deps   []helmDependency
	locked map[string]helmDependency
}

func helmKey(d helmDependency) string {
	if d.Alias != "" {
		return d.Alias
	}
	return d.Name
}
func helmIdentity(d helmDependency) string { return "Chart.yaml#" + helmKey(d) }
func helmMapping(n *yaml.Node) (map[string]*yaml.Node, error) {
	if n.Kind != yaml.MappingNode || n.Anchor != "" {
		return nil, fmt.Errorf("helm: expected YAML mapping")
	}
	m := map[string]*yaml.Node{}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind != yaml.ScalarNode || k.Value == "<<" || m[k.Value] != nil {
			return nil, fmt.Errorf("helm: duplicate, merged or complex YAML key")
		}
		m[k.Value] = n.Content[i+1]
	}
	return m, nil
}
func helmReadRegular(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, 0, fmt.Errorf("helm: unsafe or oversized file %s", path)
	}
	b, err := os.ReadFile(path)
	return b, info.Mode(), err
}
func helmRead(scope Scope) (helmChart, error) {
	var c helmChart
	if scope.Kind != "project" && scope.Kind != "workspace" {
		return c, fmt.Errorf("helm: project/workspace scope required")
	}
	root, err := filepath.Abs(scope.Path)
	if err != nil {
		return c, err
	}
	c.path = filepath.Join(root, "Chart.yaml")
	c.data, c.mode, err = helmReadRegular(c.path)
	if err != nil {
		return c, err
	}
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(c.data))
	if err = dec.Decode(&doc); err != nil {
		return c, err
	}
	var extra yaml.Node
	if err = dec.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("helm: multiple YAML documents unsupported")
	}
	if len(doc.Content) != 1 {
		return c, fmt.Errorf("helm: invalid chart")
	}
	m, err := helmMapping(doc.Content[0])
	if err != nil {
		return c, err
	}
	if m["apiVersion"] == nil || m["apiVersion"].Value != "v2" {
		return c, fmt.Errorf("helm: only apiVersion v2 supported")
	}
	if n := m["dependencies"]; n != nil {
		if n.Kind != yaml.SequenceNode || n.Anchor != "" {
			return c, fmt.Errorf("helm: dependencies must be sequence")
		}
		seen := map[string]bool{}
		for _, node := range n.Content {
			fields, e := helmMapping(node)
			if e != nil {
				return c, e
			}
			var d helmDependency
			if e = node.Decode(&d); e != nil {
				return c, e
			}
			d.node = fields["version"]
			if d.Name == "" || strings.ContainsAny(d.Name, "/\\#\n\r") || d.node == nil || d.node.Kind != yaml.ScalarNode || d.node.Anchor != "" || seen[helmKey(d)] {
				return c, fmt.Errorf("helm: invalid or duplicate dependency")
			}
			seen[helmKey(d)] = true
			c.deps = append(c.deps, d)
		}
	}
	c.locked = map[string]helmDependency{}
	b, _, err := helmReadRegular(filepath.Join(root, "Chart.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var lock struct {
		Dependencies []helmDependency `yaml:"dependencies"`
	}
	if err = yaml.Unmarshal(b, &lock); err != nil {
		return c, err
	}
	for _, d := range lock.Dependencies {
		key := d.Name + "\n" + d.Repository
		if _, ok := c.locked[key]; ok {
			return c, fmt.Errorf("helm: duplicate lock dependency unsupported")
		}
		c.locked[key] = d
	}
	return c, nil
}
func helmCurrent(c helmChart, d helmDependency) (string, string) {
	if l, ok := c.locked[d.Name+"\n"+d.Repository]; ok {
		return l.Version, "lockfile"
	}
	return d.Version, "manifest"
}
func helmRequire(executable string) error {
	if executable == "" {
		return fmt.Errorf("helm: native Helm executable required")
	}
	_, err := exec.LookPath(executable)
	return err
}
func (Helm) listInstalled(ctx context.Context, executable string, scope Scope) ([]InstalledPackage, error) {
	if err := helmRequire(executable); err != nil {
		return nil, err
	}
	c, err := helmRead(scope)
	if err != nil {
		return nil, err
	}
	var ps []InstalledPackage
	for _, d := range c.deps {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		v, source := helmCurrent(c, d)
		ps = append(ps, InstalledPackage{Manager: PM_HELM, Name: helmIdentity(d), Version: v, Role: RoleLibrary, Scope: scope.Kind, Path: scope.Path, Manifest: c.path, Config: helmKey(d), VersionSource: source})
	}
	return ps, nil
}
func (Helm) listOutdatedScope(ctx context.Context, executable string, scope Scope, options ManagerOptions, packages Packages) error {
	if err := helmRequire(executable); err != nil {
		return err
	}
	c, err := helmRead(scope)
	if err != nil {
		return err
	}
	cache := map[string]helmIndex{}
	var records []Tool
	for _, d := range c.deps {
		match, e := helmConstraint(d.Version)
		if e != nil {
			return e
		}
		idx, ok := cache[d.Repository]
		if !ok {
			idx, e = helmFetchIndex(ctx, d.Repository)
			if e != nil {
				return e
			}
			cache[d.Repository] = idx
		}
		current, _ := helmCurrent(c, d)
		if !helmStable(current) {
			return fmt.Errorf("helm: dependency %s needs concrete stable Chart.lock version", d.Name)
		}
		latest := current
		for _, entry := range idx.Entries[d.Name] {
			v := entry.Version
			if !helmStable(v) || len(entry.URLs) == 0 {
				continue
			}
			if helmCompare(v, latest) > 0 && (options.IncludeBumpCandidates || match(v)) {
				latest = v
			}
		}
		if latest != current {
			records = append(records, recordTool{Package{Manager: PM_HELM, Name: helmIdentity(d), Current: current, Latest: latest, Requested: d.Version, Role: RoleLibrary, Scope: scope.Kind, Path: scope.Path, Manifest: c.path, Config: helmKey(d), Executable: executable}})
		}
	}
	packages[PM_HELM] = append(packages[PM_HELM], records...)
	return nil
}

func helmEdit(data []byte, n *yaml.Node, value string) ([]byte, error) {
	if n.Kind != yaml.ScalarNode || n.Anchor != "" || (n.Style != 0 && n.Style != yaml.SingleQuotedStyle && n.Style != yaml.DoubleQuotedStyle) {
		return nil, fmt.Errorf("helm: unsupported version scalar style")
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if n.Line < 1 || n.Line > len(lines) {
		return nil, fmt.Errorf("helm: invalid scalar position")
	}
	line := lines[n.Line-1]
	runes := []rune(string(line))
	if n.Column < 1 || n.Column > len(runes) {
		return nil, fmt.Errorf("helm: invalid scalar column")
	}
	start := 0
	for _, l := range lines[:n.Line-1] {
		start += len(l)
	}
	start += len(string(runes[:n.Column-1]))
	end := start
	token := n.Value
	switch n.Style {
	case yaml.SingleQuotedStyle:
		token = "'" + strings.ReplaceAll(n.Value, "'", "''") + "'"
		value = "'" + value + "'"
	case yaml.DoubleQuotedStyle:
		token = "\"" + n.Value + "\""
		value = "\"" + value + "\""
	}
	end += len(token)
	if end > len(data) || string(data[start:end]) != token {
		return nil, fmt.Errorf("helm: unsupported escaped scalar")
	}
	out := append([]byte(nil), data[:start]...)
	out = append(out, value...)
	out = append(out, data[end:]...)
	return out, nil
}
func helmEditVersions(c helmChart, versions map[string]string) ([]byte, error) {
	deps := append([]helmDependency(nil), c.deps...)
	sort.Slice(deps, func(i, j int) bool {
		a, b := deps[i].node, deps[j].node
		return a.Line > b.Line || a.Line == b.Line && a.Column > b.Column
	})
	data := c.data
	for _, d := range deps {
		if v, ok := versions[helmKey(d)]; ok {
			var err error
			data, err = helmEdit(data, d.node, v)
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

func helmProjectFiles(root string) ([]string, error) {
	names := []string{"Chart.yaml", "Chart.lock"}
	base := filepath.Join(root, "charts")
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) && path == base {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("helm: symlink in charts unsupported")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("helm: nonregular chart file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		names = append(names, rel)
		return nil
	})
	sort.Strings(names)
	return names, err
}
