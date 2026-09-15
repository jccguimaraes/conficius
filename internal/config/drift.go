package config

const (
	DriftSatisfied   = "satisfied"
	DriftBehind      = "behind"
	DriftMissing     = "missing"
	DriftUnsupported = "unsupported"
)

type PackageState struct {
	Manager        string `json:"manager"`
	Name           string `json:"name"`
	Scope          string `json:"scope"`
	Path           string `json:"path,omitempty"`
	Installed      string `json:"installed,omitempty"`
	DesiredVersion string `json:"desired_version,omitempty"`
	Status         string `json:"status"`
}

type InstalledVersion struct {
	Manager string
	Name    string
	Scope   string
	Path    string
	Version string
}

func EvaluatePackages(profile Profile, installed []InstalledVersion) ([]InstalledVersion, []PackageState) {
	selected := make([]InstalledVersion, 0, len(installed))
	states := make([]PackageState, 0)
	seen := make(map[string]struct{})
	for _, pkg := range installed {
		rules, mode := rulesForScope(profile, pkg.Path)
		rule, matched := matchingRule(rules, pkg.Manager, pkg.Name)
		include := mode == "denylist"
		if matched {
			include = rule.Policy == "include"
		}
		if !include {
			continue
		}
		selected = append(selected, pkg)
		if matched && rule.DesiredVersion != "" {
			states = append(states, packageState(pkg, rule.DesiredVersion))
			seen[packageKey(pkg.Path, pkg.Manager, pkg.Name)] = struct{}{}
		}
	}
	for _, scope := range desiredRules(profile) {
		key := packageKey(scope.Path, scope.Rule.Manager, scope.Rule.Name)
		if _, exists := seen[key]; exists || scope.Rule.Policy != "include" || scope.Rule.DesiredVersion == "" {
			continue
		}
		status := DriftMissing
		if scope.Rule.Manager == "brew" {
			status = DriftUnsupported
		}
		states = append(states, PackageState{Manager: scope.Rule.Manager, Name: scope.Rule.Name, Scope: scope.Scope, Path: scope.Path, DesiredVersion: scope.Rule.DesiredVersion, Status: status})
	}
	return selected, states
}

func packageState(pkg InstalledVersion, desired string) PackageState {
	status := DriftSatisfied
	if pkg.Manager == "brew" {
		status = DriftUnsupported
	} else if pkg.Version != desired {
		status = DriftBehind
	}
	return PackageState{Manager: pkg.Manager, Name: pkg.Name, Scope: pkg.Scope, Path: pkg.Path, Installed: pkg.Version, DesiredVersion: desired, Status: status}
}

func rulesForScope(profile Profile, path string) ([]PackageRule, string) {
	if path != "" {
		for _, project := range profile.Projects {
			if project.ResolvedPath == path || project.Path == path {
				mode := project.Mode
				if mode == "" {
					mode = profile.Mode
				}
				return project.Packages, mode
			}
		}
	}
	return profile.Packages, profile.Mode
}

func matchingRule(rules []PackageRule, manager, name string) (PackageRule, bool) {
	for _, rule := range rules {
		if rule.Manager == manager && rule.Name == name {
			return rule, true
		}
	}
	return PackageRule{}, false
}

type scopedRule struct {
	Scope string
	Path  string
	Rule  PackageRule
}

func desiredRules(profile Profile) []scopedRule {
	result := make([]scopedRule, 0, len(profile.Packages))
	for _, rule := range profile.Packages {
		result = append(result, scopedRule{Scope: "global", Rule: rule})
	}
	for _, project := range profile.Projects {
		for _, rule := range project.Packages {
			result = append(result, scopedRule{Scope: "project", Path: project.ResolvedPath, Rule: rule})
		}
	}
	return result
}

func packageKey(path, manager, name string) string {
	return path + "\x00" + manager + "\x00" + name
}
