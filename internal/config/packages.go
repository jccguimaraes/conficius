package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

const PackageExample = `version = 2
layout = "xdg"

[profiles.default]
`

type packageDocument struct {
	Version         int                 `toml:"version"`
	Profile         string              `toml:"profile,omitempty"`
	Profiles        map[string]Profile  `toml:"profiles,omitempty"`
	Vulnerabilities VulnerabilityConfig `toml:"vulnerabilities,omitempty"`
	Layout          string              `toml:"layout"`
}

func packageLocations(goos, home, xdg, appdata string) (string, string, error) {
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	xdgPath, err := absolutePath(filepath.Join(xdg, "conficius", "config.toml"))
	if err != nil {
		return "", "", err
	}
	native := xdgPath
	switch goos {
	case "darwin":
		native = filepath.Join(home, "Library", "Application Support", "conficius", "config.toml")
	case "windows":
		if appdata == "" {
			appdata = filepath.Join(home, "AppData", "Roaming")
		}
		native = filepath.Join(appdata, "conficius", "config.toml")
	}
	native, err = absolutePath(native)
	return xdgPath, native, err
}

func currentPackageLocations() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return packageLocations(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA"))
}

func packageExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect package config %q: %w", path, err)
	}
	return true, nil
}

func discoverPackages(xdg, native string) (string, error) {
	x, err := packageExists(xdg)
	if err != nil {
		return "", err
	}
	n := x
	if native != xdg {
		n, err = packageExists(native)
		if err != nil {
			return "", err
		}
		if x && n {
			return "", fmt.Errorf("conflicting package configs: %q and %q", xdg, native)
		}
	}
	if x {
		return xdg, nil
	}
	if n {
		return native, nil
	}
	return "", nil
}

func LoadPackages(explicit string) (*Project, error) {
	path := explicit
	if path == "" {
		xdg, native, err := currentPackageLocations()
		if err != nil {
			return nil, err
		}
		path, err = discoverPackages(xdg, native)
		if err != nil {
			return nil, err
		}
		if path == "" {
			return &Project{Version: CurrentVersion, Layout: "xdg"}, nil
		}
	}
	path, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package config %q: %w", path, err)
	}
	return decodePackages(data, path)
}

func decodePackages(data []byte, path string) (*Project, error) {
	var doc packageDocument
	metadata, err := toml.Decode(string(data), &doc)
	if err != nil {
		return nil, fmt.Errorf("decode package config %q: %w", path, err)
	}
	if keys := metadata.Undecoded(); len(keys) != 0 {
		return nil, fmt.Errorf("unknown package config fields: %v", keys)
	}
	if doc.Version != CurrentVersion {
		return nil, fmt.Errorf("package config version must be %d, got %d", CurrentVersion, doc.Version)
	}
	if doc.Layout == "" {
		doc.Layout = "xdg"
	}
	if err := validatePackageLayout(doc.Layout); err != nil {
		return nil, err
	}
	project := &Project{Version: doc.Version, Profile: doc.Profile, Profiles: doc.Profiles,
		Vulnerabilities: doc.Vulnerabilities, Layout: doc.Layout, Path: path, Root: filepath.Dir(path)}
	if project.Profile != "" && !profileNamePattern.MatchString(project.Profile) {
		return nil, fmt.Errorf("invalid selected profile %q", project.Profile)
	}
	for name, profile := range project.Profiles {
		if !profileNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid profile name %q", name)
		}
		if err := normalizeProfile(&profile, project.Root); err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		project.Profiles[name] = profile
	}
	if err := project.normalizeVulnerabilityMappings(); err != nil {
		return nil, err
	}
	if err := project.normalizeEUVDMappings(); err != nil {
		return nil, err
	}
	return project, nil
}

func validatePackageLayout(mode string) error {
	if mode != "xdg" && mode != "os-native" {
		return fmt.Errorf("package layout must be xdg or os-native, got %q", mode)
	}
	return nil
}

type PackageLayoutPlan struct {
	Source      string
	Destination string
	Description string

	source      string
	destination string
	original    []byte
	encoded     []byte
	info        os.FileInfo
	xdg         string
	native      string
}

func (plan *PackageLayoutPlan) String() string { return plan.Description }

func PlanPackageLayout(mode string) (*PackageLayoutPlan, error) {
	xdg, native, err := currentPackageLocations()
	if err != nil {
		return nil, err
	}
	return planPackageLayout(mode, xdg, native)
}

func planPackageLayout(mode, xdg, native string) (*PackageLayoutPlan, error) {
	source, err := discoverPackages(xdg, native)
	if err != nil {
		return nil, err
	}
	plan := &PackageLayoutPlan{Source: source, source: source, xdg: xdg, native: native}
	project := &Project{Version: CurrentVersion, Layout: "xdg"}
	if source != "" {
		plan.info, err = os.Lstat(source)
		if err != nil {
			return nil, err
		}
		if !plan.info.Mode().IsRegular() {
			return nil, fmt.Errorf("package layout source must be a regular file: %q", source)
		}
		plan.original, err = os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		project, err = decodePackages(plan.original, source)
		if err != nil {
			return nil, err
		}
	}
	if mode == "" {
		mode = project.Layout
	}
	if err := validatePackageLayout(mode); err != nil {
		return nil, err
	}
	plan.Destination = xdg
	if mode == "os-native" {
		plan.Destination = native
	}
	plan.destination = plan.Destination
	doc := packageDocument{Version: project.Version, Profile: project.Profile, Profiles: project.Profiles,
		Vulnerabilities: project.Vulnerabilities, Layout: mode}
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(doc); err != nil {
		return nil, err
	}
	plan.encoded = output.Bytes()
	plan.Description = fmt.Sprintf("Move package config from %s to %s (layout %s)", source, plan.Destination, mode)
	if source == "" {
		plan.Description = fmt.Sprintf("Create package config at %s (layout %s)", plan.Destination, mode)
	}
	return plan, nil
}

func (plan *PackageLayoutPlan) verifySource(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stale package layout source: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(plan.info, info) || info.Mode() != plan.info.Mode() || !info.ModTime().Equal(plan.info.ModTime()) {
		return errors.New("stale package layout source")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, plan.original) {
		return errors.New("stale package layout source contents")
	}
	return nil
}

func (plan *PackageLayoutPlan) verifyLocations(source string) error {
	found, err := discoverPackages(plan.xdg, plan.native)
	if err != nil {
		return err
	}
	if found != source {
		return errors.New("stale package layout discovery")
	}
	return nil
}

func (plan *PackageLayoutPlan) moveSource() (string, error) {
	backup, err := os.CreateTemp(filepath.Dir(plan.source), ".conficius-layout-backup-*")
	if err != nil {
		return "", err
	}
	if err := backup.Close(); err != nil {
		return "", errors.Join(err, os.Remove(backup.Name()))
	}
	if err := os.Rename(plan.source, backup.Name()); err != nil {
		return "", errors.Join(err, os.Remove(backup.Name()))
	}
	if err := syncDirectory(filepath.Dir(plan.source)); err != nil {
		return backup.Name(), err
	}
	return backup.Name(), plan.verifySource(backup.Name())
}

func (plan *PackageLayoutPlan) restoreSource(backup string) error {
	if err := os.Link(backup, plan.source); err != nil {
		return fmt.Errorf("restore source (backup retained at %q): %w", backup, err)
	}
	return syncDirectory(filepath.Dir(plan.source))
}

func (plan *PackageLayoutPlan) Apply() error {
	return plan.apply(syncDirectory)
}

func (plan *PackageLayoutPlan) apply(syncDirectory func(string) error) (err error) {
	if plan == nil || plan.destination == "" || plan.Source != plan.source || plan.Destination != plan.destination {
		return errors.New("invalid package layout plan")
	}
	if err := plan.verifyLocations(plan.source); err != nil {
		return err
	}
	if plan.source != "" {
		if err := plan.verifySource(plan.source); err != nil {
			return err
		}
	}
	dir := filepath.Dir(plan.destination)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".conficius-layout-*")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.Remove(temp.Name()), syncDirectory(dir)) }()
	mode := os.FileMode(0600)
	if plan.info != nil {
		mode = plan.info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		return errors.Join(err, temp.Close())
	}
	if _, err := temp.Write(plan.encoded); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := plan.verifyLocations(plan.source); err != nil {
		return err
	}
	if plan.source != "" {
		backup, moveErr := plan.moveSource()
		if backup != "" {
			published := false
			defer func() {
				if !published {
					err = errors.Join(err, plan.restoreSource(backup))
				}
				if err != nil {
					err = fmt.Errorf("package layout backup retained at %q: %w", backup, err)
				}
			}()
			if moveErr != nil {
				return moveErr
			}
			if err := plan.verifyLocations(""); err != nil {
				return err
			}
			if err := os.Link(temp.Name(), plan.destination); err != nil {
				return err
			}
			published = true
			return syncDirectory(dir)
		}
		return moveErr
	}
	if err := plan.verifyLocations(""); err != nil {
		return err
	}
	if err := os.Link(temp.Name(), plan.destination); err != nil {
		return err
	}
	return syncDirectory(dir)
}
