# Conficius

Conficius audits dotfile layouts, links configuration files, and inspects tools installed by supported package managers. It supports macOS and Linux.

<p align="center">
  <img src="./docs/assets/conficius-gopher.png" alt="Conficius Gopher" width="300">
</p>

> Inspired by the Go [gopher](https://go.dev/doc/gopher) mascot created by Renee French.

## Installation

Download a `conficius_VERSION_OS_ARCH.tar.gz` archive from the [GitHub releases page](https://github.com/jccguimaraes/conficius/releases), extract it, and place `conficius` on `PATH`.

No tagged release exists yet. To build current source, install Go 1.26.8 and run:

```sh
mise run build-cli
```

The executable is written to `./bin/conficius`.

## CLI

`conficius init` creates `conficius.toml` and refuses to overwrite an existing file. `--config FILE` selects an explicit config; otherwise Conficius searches the current directory and its parents, then `$XDG_CONFIG_HOME/conficius/conficius.toml`.

Audit current shell and config-directory state against either XDG or OS-native conventions:

```sh
conficius dotfiles audit --xdg
conficius dotfiles audit --os-native
conficius dotfiles audit --xdg --strict
conficius dotfiles audit --xdg --json
```

The audit reads the current OS, architecture, home directory, shell, and XDG environment variables. It reports mismatches and recommendations without changing files. `--strict` returns a nonzero exit code for warnings or errors.

Dotfile linking is configured explicitly. Linking previews changes by default; applying changes requires an explicit flag:

```sh
conficius dotfiles link
conficius dotfiles link zsh --exclude git
conficius dotfiles link --apply
conficius dotfiles link --apply --force
```

Existing regular files are never overwritten. Forced replacement is limited to symlinks whose destination remains inside the current config repository. All links are validated before mutation, and failed batches are rolled back.

Package commands support mise, Homebrew, and global npm packages:

```sh
conficius packages pms
conficius packages pms --json
conficius packages outdated
conficius packages outdated --pm mise --pm npm
conficius packages vulnerabilities --pm npm
conficius packages update
conficius packages update --apply --include npm:typescript
```

Package-manager inspection reports unavailable and failed managers without hiding other results. Updates preview by default; `--apply` runs them. Include/exclude filters select tools, and Conficius re-checks available updates immediately before execution. Inspection commands support schema-versioned JSON with `--json`.

Vulnerability checks query OSV with exact installed versions. Global npm packages map directly to OSV's `npm` ecosystem. Homebrew and mise packages require explicit mappings because names alone do not prove package identity. Unmapped packages report `unknown`, never `clean`. Vulnerability findings do not change exit status; inventory or OSV request failures do.

## Configuration

Configuration uses TOML schema version `1`. Link sources are relative to the configuration file. Targets use plain environment-variable references; Conficius expands them when loading config.

```toml
version = 1

[[tools.zsh.links]]
source = "zsh/.zshrc"
target = "$ZDOTDIR/.zshrc"
platforms = ["darwin", "linux"]

# Optional: map mise/Homebrew package identity to an OSV ecosystem.
[[vulnerabilities.mappings]]
manager = "mise"
package = "node"
ecosystem = "npm"
name = "node"
```

Current source provides CLI workflows only. TUI-assisted workflows are planned for a later iteration.

## Development

Run `mise run setup` once to configure repository hooks, then use:

```sh
mise run cli
mise run test-ci
mise run lint
mise run build-cli
mise run smoke
mise exec -- goreleaser check
mise exec -- goreleaser build --snapshot --clean
```

CI runs race-enabled tests, vet, lint, builds, smoke tests, and GoReleaser validation on Linux and macOS. CLI releases use `cli-vX.Y.Z` tags and a manual workflow. Tag pushes verify release state without publishing. Publishing requires the same tag ref and the exact `publish` confirmation input. Homebrew publication remains disabled until a tap repository exists.
