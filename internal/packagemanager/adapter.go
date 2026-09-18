package packagemanager

import (
	"context"
	"fmt"
)

type managerAdapter interface {
	name() string
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
