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

type managerAdapter interface {
	name() string
	capabilities() []Capability
	listInstalled(context.Context, string, Scope) ([]InstalledPackage, error)
	listOutdatedScope(context.Context, string, Scope, ManagerOptions, Packages) error
	updateArgs([]Package) []string
}

var adapters = []managerAdapter{
	Mise{},
	Homebrew{},
	NPM{},
}

func adapterFor(name string) (managerAdapter, error) {
	for _, adapter := range adapters {
		if adapter.name() == name {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("unsupported package manager %q", name)
}
