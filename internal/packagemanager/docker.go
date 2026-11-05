package packagemanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

const PM_DOCKER = "docker"

type Docker struct{}

var _ managerAdapter = Docker{}

func (Docker) name() string { return PM_DOCKER }
func (Docker) capabilities() []Capability {
	return []Capability{CapabilityProjectInventory, CapabilityProjectOutdated, CapabilityProjectUpdate}
}
func (Docker) testedVersions() VersionRange             { return VersionRange{} }
func (Docker) updateImpact(Scope) MutationImpact        { return MutationImpact{MayChangeManifests: true} }
func (Docker) recoveryArgs(Scope, []Package) [][]string { return nil }
func (Docker) projectFiles() []string {
	return []string{"Dockerfile", "Containerfile", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
}
func (Docker) updateArgs(packages []Package) []string { return dockerPlanArgs(packages) }
func dockerPlanArgs(packages []Package) []string {
	args := make([]string, 0, len(packages))
	for _, p := range packages {
		args = append(args, fmt.Sprintf("edit %s [%s]: %s => %s", p.Manifest, p.Name, p.Current, p.Latest))
	}
	return args
}

type dockerSource struct {
	name, ref  string
	start, end int
	quote      byte
	fields     []dockerField
}

type dockerField struct {
	start, end int
	quote      byte
	part       string
}
type dockerFile struct {
	path     string
	relative string
	realRoot string
	data     []byte
	mode     os.FileMode
	sources  []dockerSource
}

func dockerReadFile(root, name string) (dockerFile, error) {
	f := dockerFile{path: filepath.Join(root, name), relative: filepath.ToSlash(name)}
	if err := dockerSafePath(root, name); err != nil {
		return f, err
	}
	var err error
	f.realRoot, err = filepath.EvalSymlinks(root)
	if err != nil {
		return f, err
	}
	info, err := os.Lstat(f.path)
	if err != nil {
		return f, err
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("docker: source is not a regular file: %s", f.path)
	}
	if info.Size() > 4<<20 {
		return f, fmt.Errorf("docker: source exceeds 4 MiB: %s", f.path)
	}
	handle, err := os.Open(f.path)
	if err != nil {
		return f, err
	}
	defer func() { _ = handle.Close() }()
	opened, err := handle.Stat()
	if err != nil {
		return f, err
	}
	if !os.SameFile(info, opened) {
		return f, fmt.Errorf("docker: source changed while opening: %s", f.path)
	}
	f.data, err = io.ReadAll(io.LimitReader(handle, (4<<20)+1))
	if err != nil {
		return f, err
	}
	if len(f.data) > 4<<20 {
		return f, fmt.Errorf("docker: source exceeds 4 MiB: %s", f.path)
	}
	f.mode = info.Mode().Perm()
	f.sources, err = dockerParse(name, f.data)
	if err != nil {
		return f, fmt.Errorf("%s: %w", f.path, err)
	}
	return f, nil
}

func dockerDiscover(scope Scope) ([]dockerFile, error) {
	if scope.Kind != "project" && scope.Kind != "workspace" {
		return nil, fmt.Errorf("docker: only project scope supported")
	}
	root, err := filepath.Abs(scope.Path)
	if err != nil {
		return nil, err
	}
	if err := dockerSafePath(root, "."); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("docker: project root must be a real directory")
	}
	var files []dockerFile
	names, err := dockerProjectFiles(root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		f, err := dockerReadFile(root, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func dockerParse(name string, data []byte) ([]dockerSource, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("docker: source must be UTF-8")
	}
	name = filepath.Base(name)
	if name == "Dockerfile" || name == "Containerfile" {
		return dockerParseBuild(data)
	}
	switch name {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return dockerParseCompose(data)
	default:
		return dockerParseKubernetes(name, data)
	}
}

var dockerWords = regexp.MustCompile(`\S+`)

func dockerParseBuild(data []byte) ([]dockerSource, error) {
	var sources []dockerSource
	aliases := map[string]bool{}
	stage := 0
	var logical []byte
	var positions []int
	offset := 0
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		content := bytes.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(string(content))
		if strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(strings.ToLower(trimmed), "# escape=") && !strings.HasSuffix(trimmed, "\\") {
				return nil, fmt.Errorf("docker: non-default escape directive unsupported")
			}
			offset += len(line)
			continue
		}
		if strings.Contains(string(content), "<<") {
			return nil, fmt.Errorf("docker: heredoc instructions unsupported")
		}
		continued := bytes.HasSuffix(content, []byte("\\"))
		if continued {
			content = content[:len(content)-1]
		}
		for i, c := range content {
			logical = append(logical, c)
			positions = append(positions, offset+i)
		}
		offset += len(line)
		if continued {
			logical = append(logical, ' ')
			positions = append(positions, -1)
			continue
		}
		words := dockerWords.FindAllIndex(logical, -1)
		if len(words) > 0 && strings.EqualFold(string(logical[words[0][0]:words[0][1]]), "FROM") {
			i := 1
			if i < len(words) && strings.HasPrefix(string(logical[words[i][0]:words[i][1]]), "--platform=") {
				i++
			}
			if i >= len(words) {
				return nil, fmt.Errorf("docker: malformed FROM")
			}
			token := words[i]
			ref := string(logical[token[0]:token[1]])
			if len(words) != i+1 && (len(words) != i+3 || !strings.EqualFold(string(logical[words[i+1][0]:words[i+1][1]]), "AS")) {
				return nil, fmt.Errorf("docker: malformed FROM")
			}
			if strings.ContainsAny(ref, "$`") {
				return nil, fmt.Errorf("docker: dynamic FROM/ARG interpolation unsupported")
			}
			if !aliases[strings.ToLower(ref)] && !strings.EqualFold(ref, "scratch") {
				if _, err := dockerParseReference(ref); err != nil {
					return nil, err
				}
				start, end := positions[token[0]], positions[token[1]-1]+1
				if start < 0 || end-start != len(ref) {
					return nil, fmt.Errorf("docker: continued image token unsupported")
				}
				sources = append(sources, dockerSource{name: fmt.Sprintf("stage:%d", stage), ref: ref, start: start, end: end})
			}
			if len(words) == i+3 {
				alias := strings.ToLower(string(logical[words[i+2][0]:words[i+2][1]]))
				if !regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`).MatchString(alias) || aliases[alias] {
					return nil, fmt.Errorf("docker: invalid or duplicate stage alias")
				}
				aliases[alias] = true
			}
			stage++
		}
		logical = nil
		positions = nil
	}
	if len(logical) != 0 {
		return nil, fmt.Errorf("docker: unterminated continuation")
	}
	return sources, nil
}

func dockerMapping(n *yaml.Node) (map[string]*yaml.Node, error) {
	if n.Kind != yaml.MappingNode || n.Anchor != "" {
		return nil, fmt.Errorf("docker: expected YAML mapping")
	}
	result := map[string]*yaml.Node{}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value == "<<" {
			return nil, fmt.Errorf("docker: YAML aliases/merge keys unsupported in service mappings")
		}
		if _, exists := result[k.Value]; exists {
			return nil, fmt.Errorf("docker: duplicate YAML key %q", k.Value)
		}
		result[k.Value] = n.Content[i+1]
	}
	return result, nil
}

func dockerParseCompose(data []byte) ([]dockerSource, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("docker: compose YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("docker: multiple YAML documents unsupported")
	}
	if len(doc.Content) != 1 {
		return nil, fmt.Errorf("docker: empty compose document")
	}
	root, err := dockerMapping(doc.Content[0])
	if err != nil {
		return nil, err
	}
	services := root["services"]
	if services == nil {
		return nil, nil
	}
	serviceMap, err := dockerMapping(services)
	if err != nil {
		return nil, err
	}
	var sources []dockerSource
	for name, service := range serviceMap {
		fields, err := dockerMapping(service)
		if err != nil {
			return nil, err
		}
		image := fields["image"]
		if image == nil {
			continue
		}
		if image.Kind != yaml.ScalarNode || image.Tag != "!!str" || image.Anchor != "" || image.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return nil, fmt.Errorf("docker: service %s image must be an unanchored single-line scalar", name)
		}
		if _, err := dockerParseReference(image.Value); err != nil {
			return nil, fmt.Errorf("service %s: %w", name, err)
		}
		span, err := dockerYAMLScalar(data, image)
		if err != nil {
			return nil, err
		}
		start, end, quote := span.start, span.end, span.quote
		sources = append(sources, dockerSource{name: "service:" + name, ref: image.Value, start: start, end: end, quote: quote})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].start < sources[j].start })
	return sources, nil
}

func dockerIdentity(file dockerFile, source dockerSource) string {
	name := file.relative
	if name == "" {
		name = filepath.Base(file.path)
	}
	return name + "#" + source.name
}

func (Docker) listInstalled(ctx context.Context, _ string, scope Scope) ([]InstalledPackage, error) {
	files, err := dockerDiscover(scope)
	if err != nil {
		return nil, err
	}
	var packages []InstalledPackage
	for _, f := range files {
		for _, s := range f.sources {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			packages = append(packages, InstalledPackage{Manager: PM_DOCKER, Name: dockerIdentity(f, s), Version: s.ref, Role: RoleRuntime, Scope: scope.Kind, Path: scope.Path, Manifest: f.path, Config: s.name, VersionSource: "manifest"})
		}
	}
	return packages, nil
}

func (Docker) listOutdatedScope(ctx context.Context, _ string, scope Scope, options ManagerOptions, packages Packages) error {
	files, err := dockerDiscover(scope)
	if err != nil {
		return err
	}
	cache := map[string]string{}
	var records []Tool
	for _, f := range files {
		for _, s := range f.sources {
			latest, ok := cache[s.ref]
			if !ok {
				latest, err = dockerLatest(ctx, s.ref, options.IncludeBumpCandidates)
				if err != nil {
					return fmt.Errorf("%s [%s]: %w", f.path, s.name, err)
				}
				cache[s.ref] = latest
			}
			if latest == "" || latest == s.ref {
				continue
			}
			records = append(records, recordTool{Package{Manager: PM_DOCKER, Name: dockerIdentity(f, s), Current: s.ref, Latest: latest, Role: RoleRuntime, Scope: scope.Kind, Path: scope.Path, Manifest: f.path, Config: s.name}})
		}
	}
	packages[PM_DOCKER] = append(packages[PM_DOCKER], records...)
	return nil
}

func dockerValidateChange(current, latest string, bump bool) error {
	a, err := dockerParseReference(current)
	if err != nil {
		return err
	}
	b, err := dockerParseReference(latest)
	if err != nil {
		return err
	}
	if a.repository != b.repository || a.registry != b.registry || a.original != b.original {
		return fmt.Errorf("docker: image repository cannot change")
	}
	if (a.digest == "") != (b.digest == "") {
		return fmt.Errorf("docker: digest pinning cannot change")
	}
	if a.tag == b.tag {
		if a.digest == "" || a.digest == b.digest {
			return fmt.Errorf("docker: reference has no update target")
		}
		return nil
	}
	if !bump && a.digest != "" {
		return fmt.Errorf("docker: pinned Update must keep same tag")
	}
	av, aok := dockerParseVersion(a.tag)
	bv, bok := dockerParseVersion(b.tag)
	if !aok || !bok || av.precision != bv.precision || av.suffix != bv.suffix || !bv.newer(av) || (!bump && (av.precision == 1 || av.parts[0] != bv.parts[0])) {
		return fmt.Errorf("docker: incompatible image tag change")
	}
	return nil
}

type dockerEdit struct {
	source dockerSource
	latest string
}
type dockerPending struct {
	file dockerFile
	data []byte
}

func dockerPrepare(plan UpdatePlan, verify bool) ([]dockerPending, error) {
	if plan.Manager != PM_DOCKER || (plan.Scope != "project" && plan.Scope != "workspace") {
		return nil, fmt.Errorf("docker: invalid project plan")
	}
	files, err := dockerDiscover(Scope{Kind: plan.Scope, Path: plan.Directory})
	if err != nil {
		return nil, err
	}
	byPath := map[string]dockerFile{}
	for _, f := range files {
		byPath[f.path] = f
	}
	edits := map[string][]dockerEdit{}
	seen := map[string]bool{}
	for _, p := range plan.Packages {
		if p.Manager != PM_DOCKER || seen[p.Name] {
			return nil, fmt.Errorf("docker: invalid or duplicate package %s", p.Name)
		}
		seen[p.Name] = true
		if err := dockerValidateChange(p.Current, p.Latest, plan.Bump); err != nil {
			return nil, err
		}
		f, ok := byPath[p.Manifest]
		if !ok {
			return nil, fmt.Errorf("docker: unapproved manifest %s", p.Manifest)
		}
		found := false
		for _, s := range f.sources {
			if dockerIdentity(f, s) != p.Name {
				continue
			}
			expected := p.Current
			if verify {
				expected = p.Latest
			}
			if s.ref != expected {
				return nil, fmt.Errorf("docker: stale or unexpected image in %s [%s]: expected %s, found %s", f.path, s.name, expected, s.ref)
			}
			edits[f.path] = append(edits[f.path], dockerEdit{s, p.Latest})
			found = true
		}
		if !found {
			return nil, fmt.Errorf("docker: image identity no longer exists: %s", p.Name)
		}
	}
	if verify {
		return nil, nil
	}
	var pending []dockerPending
	for _, f := range files {
		selected := edits[f.path]
		if len(selected) == 0 {
			continue
		}
		var scalarEdits []dockerEdit
		for _, edit := range selected {
			if len(edit.source.fields) == 0 {
				scalarEdits = append(scalarEdits, edit)
				continue
			}
			ref, err := dockerParseReference(edit.latest)
			if err != nil {
				return nil, err
			}
			for _, field := range edit.source.fields {
				value := ref.tag
				if field.part == "digest" {
					value = ref.digest
				}
				if field.part == "reference" {
					value = edit.latest
				}
				scalarEdits = append(scalarEdits, dockerEdit{source: dockerSource{start: field.start, end: field.end, quote: field.quote}, latest: value})
			}
		}
		sort.Slice(scalarEdits, func(i, j int) bool { return scalarEdits[i].source.start > scalarEdits[j].source.start })
		data := slices.Clone(f.data)
		for _, edit := range scalarEdits {
			token := edit.latest
			switch edit.source.quote {
			case '\'':
				token = "'" + token + "'"
			case '"':
				token = strconv.Quote(token)
			}
			data = append(data[:edit.source.start], append([]byte(token), data[edit.source.end:]...)...)
		}
		parsed, err := dockerParse(filepath.Base(f.path), data)
		if err != nil {
			return nil, err
		}
		originalSources, err := dockerParse(filepath.Base(f.path), f.data)
		if err != nil {
			return nil, err
		}
		if len(parsed) != len(originalSources) {
			return nil, fmt.Errorf("docker: rewritten source changed image count")
		}
		for i, s := range parsed {
			expected := originalSources[i].ref
			for _, e := range selected {
				if e.source.name == s.name {
					expected = e.latest
				}
			}
			if s.name != originalSources[i].name || s.ref != expected {
				return nil, fmt.Errorf("docker: rewritten image failed validation")
			}
		}
		pending = append(pending, dockerPending{f, data})
	}
	return pending, nil
}

func dockerApply(ctx context.Context, plan UpdatePlan) error {
	pending, err := dockerPrepare(plan, false)
	if err != nil {
		return err
	}
	temps := make([]string, len(pending))
	defer func() {
		for _, name := range temps {
			if name != "" {
				_ = os.Remove(name)
			}
		}
	}()
	for i, p := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := dockerSafePath(plan.Directory, filepath.FromSlash(p.file.relative)); err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(p.file.path), ".conficius-docker-*")
		if err != nil {
			return err
		}
		temps[i] = f.Name()
		err = f.Chmod(p.file.mode)
		if err == nil {
			_, err = f.Write(p.data)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	for _, p := range pending {
		current, err := dockerReadFile(plan.Directory, filepath.FromSlash(p.file.relative))
		if err != nil {
			return err
		}
		if !bytes.Equal(current.data, p.file.data) || current.mode != p.file.mode || current.realRoot != p.file.realRoot {
			return fmt.Errorf("docker: source changed before apply: %s", p.file.path)
		}
	}
	var written []string
	for i, p := range pending {
		err := ctx.Err()
		if err == nil {
			current, readErr := dockerReadFile(plan.Directory, filepath.FromSlash(p.file.relative))
			err = readErr
			if err == nil && (!bytes.Equal(current.data, p.file.data) || current.mode != p.file.mode || current.realRoot != p.file.realRoot) {
				err = fmt.Errorf("docker: source changed before rename: %s", p.file.path)
			}
		}
		if err == nil {
			err = os.Rename(temps[i], p.file.path)
		}
		if err != nil {
			return fmt.Errorf("docker: apply failed; files already changed %v: %w", written, err)
		}
		temps[i] = ""
		written = append(written, p.file.path)
	}
	return nil
}

func dockerVerify(plan UpdatePlan) error { _, err := dockerPrepare(plan, true); return err }

func dockerSafePath(root, name string) error {
	if filepath.IsAbs(name) || !filepath.IsLocal(name) {
		return fmt.Errorf("docker: source outside project: %s", name)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	path := real
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("docker: symlink source unsupported: %s", path)
		}
	}
	return nil
}

func dockerKustomization(name string) bool {
	return name == "kustomization.yaml" || name == "kustomization.yml" || name == "Kustomization"
}

func dockerProjectFiles(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := dockerSafePath(root, "."); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("docker: project root must be a real directory")
	}
	names := map[string]bool{}
	for _, name := range (Docker{}).projectFiles() {
		_, err := os.Lstat(filepath.Join(root, name))
		if err == nil {
			names[name] = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	count := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		count++
		if count > 10000 {
			return fmt.Errorf("docker: discovery exceeds 10000 entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "charts", "templates", ".helm", ".terraform", ".cache", "dist", "build":
				return filepath.SkipDir
			}
			if strings.Count(rel, string(filepath.Separator)) >= 16 {
				return fmt.Errorf("docker: discovery exceeds 16 directory levels")
			}
			return nil
		}
		ext := filepath.Ext(entry.Name())
		if entry.Type().IsRegular() && (ext == ".yaml" || ext == ".yml" || dockerKustomization(entry.Name())) {
			names[rel] = true
		}
		if len(names) > 1000 {
			return fmt.Errorf("docker: discovery exceeds 1000 source files")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	for index := 0; index < len(result); index++ {
		name := result[index]
		if !dockerKustomization(filepath.Base(name)) {
			continue
		}
		f, err := dockerReadFile(root, name)
		if err != nil {
			return result, err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(f.data, &doc); err != nil {
			return result, err
		}
		if len(doc.Content) == 0 {
			continue
		}
		fields, err := dockerMapping(doc.Content[0])
		if err != nil {
			return result, err
		}
		for _, key := range []string{"resources", "bases", "components"} {
			seq := fields[key]
			if seq == nil {
				continue
			}
			if seq.Kind != yaml.SequenceNode || seq.Anchor != "" {
				return result, fmt.Errorf("docker: %s must be a literal sequence", key)
			}
			for _, resource := range seq.Content {
				if resource.Kind != yaml.ScalarNode || resource.Tag != "!!str" || resource.Anchor != "" {
					return result, fmt.Errorf("docker: nonliteral Kustomize resource")
				}
				value := resource.Value
				if strings.Contains(value, "://") || strings.Contains(value, "git::") || strings.Contains(value, "?ref=") {
					continue
				}
				target := filepath.Clean(filepath.Join(filepath.Dir(name), value))
				if filepath.IsAbs(value) || !filepath.IsLocal(target) {
					return result, fmt.Errorf("docker: Kustomize resource outside root: %s", value)
				}
				if err := dockerSafePath(root, target); err != nil {
					return result, err
				}
				info, err := os.Stat(filepath.Join(root, target))
				if err != nil {
					return result, err
				}
				if info.IsDir() {
					found := false
					for _, base := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
						child := filepath.Join(target, base)
						if _, err := os.Lstat(filepath.Join(root, child)); err == nil {
							found = true
							if !names[child] {
								names[child] = true
								result = append(result, child)
							}
						}
					}
					if !found {
						return result, fmt.Errorf("docker: resource directory requires Kustomization: %s", target)
					}
				} else if info.Mode().IsRegular() {
					if !names[target] {
						names[target] = true
						result = append(result, target)
					}
				} else {
					return result, fmt.Errorf("docker: unsupported resource: %s", target)
				}
				if len(result) > 1000 {
					return result, fmt.Errorf("docker: discovery exceeds 1000 source files")
				}
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func dockerYAMLScalar(data []byte, image *yaml.Node) (dockerField, error) {
	if image.Kind != yaml.ScalarNode || image.Tag != "!!str" || image.Anchor != "" || image.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return dockerField{}, fmt.Errorf("docker: expected unanchored single-line string")
	}
	start := 0
	for line := 1; line < image.Line; line++ {
		p := bytes.IndexByte(data[start:], '\n')
		if p < 0 {
			return dockerField{}, fmt.Errorf("docker: invalid YAML source position")
		}
		start += p + 1
	}
	for col := 1; col < image.Column; col++ {
		_, size := utf8.DecodeRune(data[start:])
		start += size
	}
	end := start
	var quote byte
	if start < len(data) && (data[start] == '\'' || data[start] == '"') {
		quote = data[start]
		end++
		for end < len(data) {
			if data[end] == '\n' || data[end] == '\r' {
				return dockerField{}, fmt.Errorf("docker: multiline image scalar unsupported")
			}
			if quote == '"' && data[end] == '\\' {
				end += 2
				continue
			}
			if data[end] == quote {
				if quote == '\'' && end+1 < len(data) && data[end+1] == quote {
					end += 2
					continue
				}
				end++
				break
			}
			end++
		}
	} else {
		end += len(image.Value)
	}
	if end > len(data) {
		return dockerField{}, fmt.Errorf("docker: invalid YAML scalar extent")
	}
	var check yaml.Node
	if err := yaml.Unmarshal(data[start:end], &check); err != nil || len(check.Content) != 1 || check.Content[0].Value != image.Value {
		return dockerField{}, fmt.Errorf("docker: unsupported YAML image token")
	}
	return dockerField{start: start, end: end, quote: quote}, nil
}

func dockerParseKubernetes(filename string, data []byte) ([]dockerSource, error) {
	classified := slices.Clone(data)
	if !dockerKustomization(filename) {
		boundaries := regexp.MustCompile(`(?m)^---[ \t]*(?:#.*)?\r?$`).FindAllIndex(data, -1)
		start := 0
		for _, boundary := range append(boundaries, []int{len(data), len(data)}) {
			document := data[start:boundary[0]]
			api := regexp.MustCompile(`(?m)^(?:apiVersion|"apiVersion"|'apiVersion')[ \t]*:|[\{,][ \t]*["']?apiVersion["']?[ \t]*:`).Match(document)
			kind := regexp.MustCompile(`(?m)^(?:kind|"kind"|'kind')[ \t]*:|[\{,][ \t]*["']?kind["']?[ \t]*:`).Match(document)
			if !api || !kind {
				for i := start; i < boundary[0]; i++ {
					if classified[i] != '\n' && classified[i] != '\r' {
						classified[i] = ' '
					}
				}
			}
			start = boundary[1]
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(classified))
	var sources []dockerSource
	for document := 0; ; document++ {
		var doc yaml.Node
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("docker: Kubernetes YAML: %w", err)
		}
		if len(doc.Content) == 0 || doc.Content[0].Tag == "!!null" {
			continue
		}
		node := doc.Content[0]
		if node.Kind != yaml.MappingNode {
			continue
		}
		var visit func(*yaml.Node, string) error
		visit = func(node *yaml.Node, prefix string) error {
			if !dockerKustomization(filename) && node.Kind == yaml.MappingNode {
				hasAPI, hasKind := false, false
				for i := 0; i < len(node.Content); i += 2 {
					hasAPI = hasAPI || node.Content[i].Value == "apiVersion"
					hasKind = hasKind || node.Content[i].Value == "kind"
				}
				if !hasAPI || !hasKind {
					return nil
				}
			}
			minimal := map[string]*yaml.Node{}
			if node.Kind == yaml.MappingNode {
				for i := 0; i < len(node.Content); i += 2 {
					minimal[node.Content[i].Value] = node.Content[i+1]
				}
			}
			api, kind := minimal["apiVersion"], minimal["kind"]
			if !dockerKustomization(filename) && api != nil && kind != nil {
				known := (api.Value == "v1" && (kind.Value == "Pod" || kind.Value == "ReplicationController" || kind.Value == "List")) || (api.Value == "apps/v1" && slices.Contains([]string{"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"}, kind.Value)) || (api.Value == "batch/v1" && (kind.Value == "Job" || kind.Value == "CronJob"))
				if !known {
					return nil
				}
			}
			fields, err := dockerMapping(node)
			if err != nil {
				return err
			}
			if dockerKustomization(filename) {
				if kind != nil && kind.Value != "Kustomization" {
					return fmt.Errorf("docker: invalid Kustomization kind")
				}
				parsed, err := dockerParseKustomize(data, fields, prefix)
				sources = append(sources, parsed...)
				return err
			}
			if api == nil || kind == nil {
				return nil
			}
			if api.Kind != yaml.ScalarNode || api.Anchor != "" || kind.Kind != yaml.ScalarNode || kind.Anchor != "" {
				return fmt.Errorf("docker: nonliteral Kubernetes type")
			}
			if api.Value == "v1" && kind.Value == "List" {
				items := fields["items"]
				if items == nil {
					return nil
				}
				if items.Kind != yaml.SequenceNode || items.Anchor != "" {
					return fmt.Errorf("docker: invalid Kubernetes List")
				}
				for i, item := range items.Content {
					if err := visit(item, fmt.Sprintf("%s/items/%d", prefix, i)); err != nil {
						return err
					}
				}
				return nil
			}
			var path []string
			switch {
			case api.Value == "v1" && kind.Value == "Pod":
				path = []string{"spec"}
			case api.Value == "v1" && kind.Value == "ReplicationController":
				path = []string{"spec", "template", "spec"}
			case api.Value == "apps/v1" && (kind.Value == "Deployment" || kind.Value == "StatefulSet" || kind.Value == "DaemonSet" || kind.Value == "ReplicaSet"):
				path = []string{"spec", "template", "spec"}
			case api.Value == "batch/v1" && kind.Value == "Job":
				path = []string{"spec", "template", "spec"}
			case api.Value == "batch/v1" && kind.Value == "CronJob":
				path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
			default:
				return nil
			}
			for _, key := range path {
				child := fields[key]
				if child == nil {
					return nil
				}
				fields, err = dockerMapping(child)
				if err != nil {
					return err
				}
			}
			for _, key := range []string{"containers", "initContainers", "ephemeralContainers"} {
				seq := fields[key]
				if seq == nil {
					continue
				}
				if seq.Kind != yaml.SequenceNode || seq.Anchor != "" {
					return fmt.Errorf("docker: invalid %s", key)
				}
				for i, container := range seq.Content {
					mapping, err := dockerMapping(container)
					if err != nil {
						return err
					}
					image := mapping["image"]
					if image == nil {
						continue
					}
					span, err := dockerYAMLScalar(data, image)
					if err != nil {
						return err
					}
					if _, err := dockerParseReference(image.Value); err != nil {
						return err
					}
					sources = append(sources, dockerSource{name: fmt.Sprintf("%s/%s/%s/%d", prefix, kind.Value, key, i), ref: image.Value, start: span.start, end: span.end, quote: span.quote})
				}
			}
			return nil
		}
		if err := visit(node, fmt.Sprintf("document:%d", document)); err != nil {
			return nil, err
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].start < sources[j].start })
	return sources, nil
}

func dockerParseKustomize(data []byte, mapping map[string]*yaml.Node, prefix string) ([]dockerSource, error) {
	images := mapping["images"]
	if images == nil {
		return nil, nil
	}
	if images.Kind != yaml.SequenceNode || images.Anchor != "" {
		return nil, fmt.Errorf("docker: Kustomize images must be a literal sequence")
	}
	var sources []dockerSource
	seen := map[string]bool{}
	for i, image := range images.Content {
		fields, err := dockerMapping(image)
		if err != nil {
			return nil, err
		}
		name := fields["name"]
		if name == nil {
			return nil, fmt.Errorf("docker: Kustomize image name required")
		}
		if _, err := dockerYAMLScalar(data, name); err != nil {
			return nil, err
		}
		original, err := dockerParseReference(name.Value)
		if err != nil {
			return nil, err
		}
		if seen[original.original] {
			return nil, fmt.Errorf("docker: duplicate Kustomize image selector")
		}
		seen[original.original] = true
		base := name
		if fields["newName"] != nil {
			base = fields["newName"]
		}
		span, err := dockerYAMLScalar(data, base)
		if err != nil {
			return nil, err
		}
		ref, err := dockerParseReference(base.Value)
		if err != nil {
			return nil, err
		}
		source := dockerSource{name: fmt.Sprintf("%s/kustomize/images/%d", prefix, i), start: span.start, end: span.end}
		tag, digest := ref.tag, ref.digest
		if fields["digest"] != nil && fields["newTag"] == nil && !strings.Contains(base.Value[strings.LastIndex(base.Value, "/")+1:], ":") {
			tag = ""
		}
		for _, key := range []string{"newTag", "digest"} {
			node := fields[key]
			if node == nil {
				continue
			}
			scalar := *node
			if key == "newTag" && (scalar.Tag == "!!int" || scalar.Tag == "!!float") {
				scalar.Tag = "!!str"
			}
			field, err := dockerYAMLScalar(data, &scalar)
			if err != nil {
				return nil, err
			}
			if key == "newTag" {
				tag = node.Value
				field.part = "tag"
			} else {
				digest = node.Value
				field.part = "digest"
			}
			if node.Tag == "!!int" || node.Tag == "!!float" {
				field.quote = '"'
			}
			source.fields = append(source.fields, field)
		}
		if fields["newTag"] == nil && fields["digest"] == nil {
			if !strings.Contains(base.Value[strings.LastIndex(base.Value, "/")+1:], ":") {
				return nil, fmt.Errorf("docker: Kustomize image requires editable newTag or literal tagged name/newName")
			}
			span.part = "reference"
			source.fields = []dockerField{span}
		} else if ref.digest != "" {
			return nil, fmt.Errorf("docker: embedded digest with newTag unsupported; use digest field")
		}
		source.ref = ref.original
		if tag != "" {
			source.ref += ":" + tag
		}
		if digest != "" {
			source.ref += "@" + digest
		}
		if _, err := dockerParseReference(source.ref); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}
