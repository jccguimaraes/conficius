package packagemanager

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func kubernetesWrite(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0640); err != nil {
		t.Fatal(err)
	}
}

func TestKubernetesWorkloadPaths(t *testing.T) {
	for _, tc := range []struct{ api, kind, path string }{
		{"v1", "Pod", "spec: "},
		{"v1", "ReplicationController", "spec: {template: {spec: "},
		{"apps/v1", "Deployment", "spec: {template: {spec: "},
		{"apps/v1", "StatefulSet", "spec: {template: {spec: "},
		{"apps/v1", "DaemonSet", "spec: {template: {spec: "},
		{"apps/v1", "ReplicaSet", "spec: {template: {spec: "},
		{"batch/v1", "Job", "spec: {template: {spec: "},
		{"batch/v1", "CronJob", "spec: {jobTemplate: {spec: {template: {spec: "},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			data := "apiVersion: " + tc.api + "\nkind: " + tc.kind + "\n" + tc.path + "{containers: [{name: app, image: 'node:22.1'}], initContainers: [{image: node:22.1}], ephemeralContainers: [{image: node:22.1}]}" + strings.Repeat("}", strings.Count(tc.path, "{")) + "\n"
			sources, err := dockerParse("workload.yaml", []byte(data))
			if err != nil || len(sources) != 3 {
				t.Fatalf("sources=%+v err=%v", sources, err)
			}
		})
	}
	data := "apiVersion: v1\nkind: List\nitems:\n- apiVersion: v1\n  kind: Pod\n  spec: {containers: [{image: node:22.1}]}\n---\napiVersion: v1\nkind: ConfigMap\ndata: {image: '${IGNORE}'}\n---\napiVersion: example/v1\nkind: Deployment\nspec: {image: ignored:1}\n"
	sources, err := dockerParse("list.yml", []byte(data))
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
	for _, bad := range []string{
		"apiVersion: v1\nkind: Pod\nspec: &shared {containers: [{image: node:22.1}]}\n",
		"apiVersion: v1\nkind: Pod\nshared: &shared {containers: [{image: node:22.1}]}\nspec: *shared\n",
		"apiVersion: v1\nkind: Pod\nspec: {containers: [{image: '${IMAGE}'}]}\n",
	} {
		if _, err := dockerParse("pod.yaml", []byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestKubernetesDiscoveryAndUpdate(t *testing.T) {
	server := dockerTestRegistry(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"tags":["1.1","1.2","2.1"]}`) })
	root := t.TempDir()
	image := dockerTestHost(server) + "/org/app:"
	before := "# keep\r\napiVersion: v1\r\nkind: Pod\r\nspec: {containers: [{image: '" + image + "1.1'}]} # untouched\r\n---\r\napiVersion: v1\r\nkind: Pod\r\nspec: {initContainers: [{image: \"" + image + "1.1\"}]}\r\n"
	for _, name := range []string{"a/pod.yaml", "b/pod.yaml"} {
		kubernetesWrite(t, root, name, before)
	}
	for _, name := range []string{"charts/x.yaml", "templates/pod.yaml", "node_modules/x.yaml", "vendor/x.yaml", ".git/x.yaml"} {
		kubernetesWrite(t, root, name, "{{ invalid Helm }}")
	}
	outside := t.TempDir()
	kubernetesWrite(t, outside, "pod.yaml", "invalid: [")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "pod.yaml"), filepath.Join(root, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	names, err := dockerProjectFiles(root)
	if err != nil || !slices.Equal(names, []string{"a/pod.yaml", "b/pod.yaml"}) {
		t.Fatalf("names=%v err=%v", names, err)
	}
	scope := Scope{Kind: "project", Path: root}
	packages := Packages{}
	if err := (Docker{}).listOutdatedScope(context.Background(), "", scope, ManagerOptions{}, packages); err != nil {
		t.Fatal(err)
	}
	records := Records(packages)
	if len(records) != 4 {
		t.Fatalf("records=%+v", records)
	}
	seen := map[string]bool{}
	for _, p := range records {
		if seen[p.Name] {
			t.Fatal("identity collision")
		}
		seen[p.Name] = true
	}
	plan := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Packages: records}
	stale := plan
	stale.Packages = slices.Clone(records)
	stale.Packages[3].Current = image + "1.0"
	if err := dockerApply(context.Background(), stale); err == nil {
		t.Fatal("stale accepted")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a/pod.yaml"))
	if string(data) != before {
		t.Fatal("partial write")
	}
	if err := dockerApply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := dockerVerify(plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, _ := os.ReadFile(filepath.Join(root, name))
		if string(data) != strings.ReplaceAll(before, image+"1.1", image+"1.2") {
			t.Fatalf("format lost: %s", data)
		}
	}
	packages = Packages{}
	if err := (Docker{}).listOutdatedScope(context.Background(), "", scope, ManagerOptions{IncludeBumpCandidates: true}, packages); err != nil {
		t.Fatal(err)
	}
	plan.Bump = true
	plan.Packages = Records(packages)
	if err := dockerApply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := dockerVerify(plan); err != nil {
		t.Fatal(err)
	}
	kubernetesWrite(t, root, "c/new.yaml", "apiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}]}\n")
	names, err = dockerProjectFiles(root)
	if err != nil || len(names) != 3 {
		t.Fatalf("dynamic discovery: %v %v", names, err)
	}
}

func TestKustomizeScalarEdits(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	nextDigest := "sha256:" + strings.Repeat("b", 64)
	for _, tc := range []struct{ name, entry, current, latest, want string }{
		{"numeric", "  - {name: node, newTag: 22.1}\n", "node:22.1", "node:22.2", "  - {name: node, newTag: \"22.2\"}\n"},
		{"digestOnly", "  - {name: node, digest: " + digest + "}\n", "node@" + digest, "node@" + nextDigest, "  - {name: node, digest: " + nextDigest + "}\n"},
		{"newName", "  - name: app\n    newName: 'registry.example/org/app'\n    newTag: \"1.1\" # keep\n", "registry.example/org/app:1.1", "registry.example/org/app:1.2", "  - name: app\n    newName: 'registry.example/org/app'\n    newTag: \"1.2\" # keep\n"},
		{"nameTag", "  - name: 'node:22.1' # keep\n", "node:22.1", "node:22.2", "  - name: 'node:22.2' # keep\n"},
		{"newNameTag", "  - name: app\n    newName: node:22.1\n", "node:22.1", "node:22.2", "  - name: app\n    newName: node:22.2\n"},
		{"digest", "  - {name: node, newTag: '22.1', digest: '" + digest + "'} # pin\n", "node:22.1@" + digest, "node:22.1@" + nextDigest, "  - {name: node, newTag: '22.1', digest: '" + nextDigest + "'} # pin\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			prefix := "# header\napiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nimages:\n"
			kubernetesWrite(t, root, "overlay/kustomization.yaml", prefix+tc.entry)
			files, err := dockerDiscover(Scope{Kind: "project", Path: root})
			if err != nil {
				t.Fatal(err)
			}
			source := files[0].sources[0]
			if source.ref != tc.current {
				t.Fatalf("ref=%s", source.ref)
			}
			p := Package{Manager: PM_DOCKER, Name: dockerIdentity(files[0], source), Manifest: files[0].path, Current: tc.current, Latest: tc.latest}
			plan := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Packages: []Package{p}}
			if err := dockerApply(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if err := dockerVerify(plan); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(files[0].path)
			if string(data) != prefix+tc.want {
				t.Fatalf("format lost: %s", data)
			}
		})
	}
	for _, entry := range []string{"- name: node\n", "- {name: node, newTag: '${TAG}'}\n", "- &image {name: node, newTag: '22.1'}\n", "- {name: node, newTag: '22.1'}\n- {name: node, newTag: '22.2'}\n"} {
		if _, err := dockerParse("Kustomization", []byte("images:\n"+entry)); err == nil {
			t.Fatalf("accepted %s", entry)
		}
	}
}

func TestKustomizeResourceSafetyAndOverride(t *testing.T) {
	root := t.TempDir()
	kubernetesWrite(t, root, "independent/pod.yaml", "apiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}]}\n")
	kubernetesWrite(t, root, "base/kustomization.yaml", "resources: [pod.yaml]\n")
	kubernetesWrite(t, root, "base/pod.yaml", "apiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}, {image: redis:7.1}]}\n")
	kubernetesWrite(t, root, "overlay/kustomization.yml", "resources: [../base]\nimages: [{name: node, newTag: '22.2'}]\n")
	files, err := dockerDiscover(Scope{Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		for _, source := range file.sources {
			count++
			if source.ref == "" {
				t.Fatal("empty image reference")
			}
		}
	}
	if count != 4 {
		t.Fatalf("sources=%d", count)
	}
	var selected []Package
	for _, file := range files {
		for _, source := range file.sources {
			if source.ref == "node:22.1" {
				continue
			}
			latest := "redis:8.1"
			if strings.Contains(source.name, "/kustomize/") {
				latest = "node:22.3"
			}
			selected = append(selected, Package{Manager: PM_DOCKER, Name: dockerIdentity(file, source), Manifest: file.path, Current: source.ref, Latest: latest})
		}
	}
	plan := UpdatePlan{Manager: PM_DOCKER, Scope: "project", Directory: root, Bump: true, Packages: selected}
	if err := dockerApply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := dockerVerify(plan); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(root, "base/pod.yaml"))
	if err != nil || !strings.Contains(string(base), "node:22.1") || !strings.Contains(string(base), "redis:8.1") {
		t.Fatalf("base rewrite: %s %v", base, err)
	}
	for _, resource := range []string{"../../outside", "/tmp", "../linked"} {
		if resource == "../linked" {
			if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
				t.Fatal(err)
			}
		}
		kubernetesWrite(t, root, "overlay/kustomization.yml", "resources: ['"+resource+"']\n")
		if _, err := dockerProjectFiles(root); err == nil {
			t.Fatalf("accepted resource %s", resource)
		}
	}
	kubernetesWrite(t, root, "overlay/kustomization.yml", "resources: ['https://example.invalid/repo?ref=main']\n")
	if _, err := dockerProjectFiles(root); err != nil {
		t.Fatal(err)
	}
}

func TestKubernetesIgnoresUnrelatedYAML(t *testing.T) {
	root := t.TempDir()
	kubernetesWrite(t, root, "Dockerfile", "FROM node:22.1\n")
	for name, data := range map[string]string{
		"ci.yaml":       "defaults: &defaults {image: node:22.1}\njob: {<<: *defaults}\n",
		"template.yaml": "{{ invalid template }}\n",
		"broken.yaml":   "jobs: [\n",
		"config.yaml":   "apiVersion: v1\nkind: ConfigMap\n<<: &defaults {data: {x: y}}\n",
		"mixed.yaml":    "broken: [\n---\napiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}]}\n",
	} {
		kubernetesWrite(t, root, name, data)
	}
	files, err := dockerDiscover(Scope{Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		count += len(file.sources)
	}
	if count != 2 {
		t.Fatalf("sources=%d", count)
	}
}

func TestKustomizeExplicitSkippedResources(t *testing.T) {
	root := t.TempDir()
	kubernetesWrite(t, root, "kustomization.yaml", "resources: [templates/pod.manifest, vendor/base]\n")
	kubernetesWrite(t, root, "vendor/base/Kustomization", "resources: [pod]\n")
	for _, name := range []string{"templates/pod.manifest", "vendor/base/pod"} {
		kubernetesWrite(t, root, name, "apiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}]}\n")
	}
	files, err := dockerDiscover(Scope{Kind: "project", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		count += len(file.sources)
	}
	if count != 2 {
		t.Fatalf("sources=%d", count)
	}
}

func TestDockerReadRejectsSymlinkComponent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	kubernetesWrite(t, outside, "pod.yaml", "apiVersion: v1\nkind: Pod\nspec: {containers: [{image: node:22.1}]}\n")
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if _, err := dockerReadFile(root, "nested/pod.yaml"); err == nil {
		t.Fatal("accepted symlink component")
	}
}
