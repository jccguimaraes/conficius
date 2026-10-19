package packagemanager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"
)

const cargoProjectTargetAbsent = "cargo-target-absent"

func cargoVerifyUpdate(plan UpdatePlan) error {
	if plan.Manager != PM_CARGO || len(plan.Packages) == 0 {
		return fmt.Errorf("invalid Cargo verification plan")
	}
	if plan.Scope == "global" {
		if cargoGlobalUpdateArgs(plan.Packages) == nil {
			return fmt.Errorf("invalid Cargo install verification metadata")
		}
		pkg := plan.Packages[0]
		var config cargoGlobalConfig
		if err := json.Unmarshal([]byte(pkg.Config), &config); err != nil {
			return err
		}
		installed, err := cargoGlobalTracking(config.Root)
		if err != nil {
			return err
		}
		actual, ok := installed[pkg.Name]
		if !ok || actual.Current != pkg.Latest {
			return fmt.Errorf("cargo:%s expected installed version %s, found %q", pkg.Name, pkg.Latest, actual.Current)
		}
		actual.Latest = pkg.Latest
		if !reflect.DeepEqual(cargoGlobalUpdateArgs([]Package{actual}), cargoGlobalUpdateArgs(plan.Packages)) {
			return fmt.Errorf("cargo:%s installation options changed", pkg.Name)
		}
		return nil
	}
	if (plan.Scope != "project" && plan.Scope != "workspace") || plan.Directory == "" {
		return fmt.Errorf("invalid Cargo project verification scope")
	}
	path := filepath.Join(plan.Directory, "Cargo.lock")
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read Cargo.lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cargo lockfile must be a regular, non-symlink file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lock struct {
		Packages []struct {
			Name    string `toml:"name"`
			Version string `toml:"version"`
			Source  string `toml:"source"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(contents, &lock); err != nil {
		return fmt.Errorf("decode Cargo.lock: %w", err)
	}
	for _, pkg := range plan.Packages {
		if pkg.Manager != PM_CARGO || !cargoProjectName.MatchString(pkg.Name) || !cargoProjectVersion.MatchString(pkg.Current) || !cargoProjectVersion.MatchString(pkg.Latest) || pkg.Current == pkg.Latest {
			return fmt.Errorf("invalid Cargo project verification target")
		}
		target, old := false, false
		for _, actual := range lock.Packages {
			if actual.Name != pkg.Name {
				continue
			}
			old = old || actual.Version == pkg.Current
			target = target || (actual.Version == pkg.Latest && actual.Source == cargoProjectRegistry)
		}
		if !target {
			return fmt.Errorf("cargo:%s expected locked version %s missing", pkg.Name, pkg.Latest)
		}
		if old && pkg.Config != cargoProjectTargetAbsent {
			return fmt.Errorf("cargo:%s@%s remains locked with preexisting or unknown target %s", pkg.Name, pkg.Current, pkg.Latest)
		}
	}
	return nil
}
