package packagemanager

import (
	"context"
	"fmt"
)

type Capability string

const (
	CapabilityGlobalInventory  Capability = "global_inventory"
	CapabilityProjectInventory Capability = "project_inventory"
	CapabilityGlobalOutdated   Capability = "global_outdated"
	CapabilityProjectOutdated  Capability = "project_outdated"
	CapabilityGlobalUpdate     Capability = "global_update"
	CapabilityProjectUpdate    Capability = "project_update"
)

const adapterVersion = "1"

type MutationImpact struct {
	RequiresAdmin      bool `json:"requires_admin"`
	MayRestartServices bool `json:"may_restart_services"`
	MayReboot          bool `json:"may_reboot"`
	Interactive        bool `json:"interactive"`
}

type VersionRange struct {
	MinimumMajor int `json:"minimum_major"`
	MaximumMajor int `json:"maximum_major"`
}

type managerAdapter interface {
	name() string
	capabilities() []Capability
	testedVersions() VersionRange
	updateImpact(Scope) MutationImpact
	recoveryArgs(Scope, []Package) [][]string
	projectFiles() []string
	listInstalled(context.Context, string, Scope) ([]InstalledPackage, error)
	listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error
	updateArgs([]Package) []string
}

var adapters = []managerAdapter{
	Mise{},
	Homebrew{},
	NPM{},
	MAS{},
	UV{},
}

func hasCapability(adapter managerAdapter, capability Capability) bool {
	for _, candidate := range adapter.capabilities() {
		if candidate == capability {
			return true
		}
	}
	return false
}

func scopeCapability(scope Scope, global, project Capability) Capability {
	if scope.Kind == "project" || scope.Kind == "workspace" {
		return project
	}
	return global
}

func adapterFor(name string) (managerAdapter, error) {
	for _, adapter := range adapters {
		if adapter.name() == name {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("unsupported package manager %q", name)
}
