# Conficius

Conficius manages dotfiles, checks outdated packages and vulnerabilities, and previews or applies package updates on macOS and Linux. It has separate CLI and TUI binaries.

Early beta. Feedback and bug reports are welcome through GitHub issues.

<p align="center">
  <img src="./docs/assets/conficius-gopher.png" alt="Conficius Gopher" width="300">
</p>

> Inspired by the Go [gopher](https://go.dev/doc/gopher) mascot created by Renee French.

## Installation

Download an archive from the [GitHub releases page](https://github.com/jccguimaraes/conficius/releases):

- CLI: `conficius_VERSION_OS_ARCH.tar.gz`
- TUI: `conficius-tui_VERSION_OS_ARCH.tar.gz`

Choose `darwin` for macOS or `linux`, and `arm64` or `amd64` for your CPU. Extract the archive and place its binary on `PATH`. Releases include `checksums.txt` for verifying downloads.

The TUI runs standalone; it does not need the CLI binary. Package operations need the corresponding package manager installed. Online version and vulnerability checks need network access.

To build from source, use Go 1.26.8 or newer:

```sh
go build -o ./bin/conficius ./cmd/cli
go build -o ./bin/conficius-tui ./cmd/tui
```

With mise installed, `mise install` installs the pinned development tools. `mise run build-cli` and `mise run build-tui` run tests before building.

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

Package commands inspect global installations and project dependencies. Manager capabilities vary; `conficius packages pms --json` reports which inventory, outdated, and update operations each manager supports.

```sh
conficius packages pms
conficius packages pms --json
conficius packages outdated
conficius packages outdated --pm mise --pm npm
conficius packages outdated --pm npm --project ./my-app
conficius packages vulnerabilities --pm npm --project ./my-app
conficius packages vulnerabilities --pm npm --project ./my-app --transitive
conficius packages update
conficius packages update --apply --include npm:typescript
```

Package-manager inspection reports unavailable and failed managers without hiding other results. Updates preview by default; `--apply` runs them. Include/exclude filters select tools, and Conficius re-checks available updates immediately before execution. Inspection commands support schema-versioned JSON with `--json`.

Vulnerability checks query OSV with exact installed versions. Global and project npm packages map directly to OSV's `npm` ecosystem. Homebrew and mise packages require explicit mappings because names alone do not prove package identity. Missing mappings are not evidence that a package is clean. Vulnerability findings do not change exit status; inventory or query failures do.

## Configuration

Configuration uses TOML schema version `2`. Link sources are relative to the configuration file. Targets use plain environment-variable references; Conficius expands them when loading config.

```toml
version = 2

[profiles.default]
mode = "denylist"
manager_order = ["mise", "brew", "npm"]

# Keep global npm packages outside version-managed Node directories:
# npm config set prefix "$HOME/.npm-global"
# Add "$HOME/.npm-global/bin" to PATH.
[profiles.default.managers.npm]
expected_global_prefix = "${HOME}/.npm-global"

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

## TUI

Run `conficius-tui`, or `./bin/conficius-tui` after building from source. Use `-config FILE` to select a configuration explicitly.

The home screen opens package scopes or dotfile audits and linking. Package views show outdated versions, drift from configured desired versions, and vulnerability findings.

- Arrows or `j`/`k`: move; `Enter`: open scope or details.
- `Esc`: go back; `r`: refresh; `q`: quit; `?`: screen help.
- In outdated packages, `Space` marks a package and `a` toggles all.
- `u` previews marked upgrades; `U` previews all upgrades in the current scope.
- In the command preview, `y` applies upgrades; `n` or `Esc` cancels.
- `v` opens vulnerabilities; `d` opens drift; `o` returns to outdated packages.

Confirmed upgrades run in the terminal, then return to the TUI. Progress output depends on the package manager. Dotfile links also require a preview and explicit confirmation before changes are applied.

## Development

Run `mise run setup` once to configure repository hooks, then use:

```sh
mise run cli
mise run tui
mise run test-ci
mise run lint
mise run build-cli
mise run build-tui
mise run smoke
mise exec -- goreleaser check
mise exec -- goreleaser build --snapshot --clean
```

CI runs race-enabled tests, vet, builds, and CLI smoke tests on Linux and macOS. Lint and both GoReleaser configuration checks run on Linux.

CLI releases use `cli-vX.Y.Z`; TUI releases use calendar tags shaped `tui-vYYYY.MM.revision`. Tags must point to commits included in `main`; CLI versions also need a matching changelog heading. Tag pushes verify release state without publishing. To publish, manually run the matching release workflow against its tag with `confirm` set to `publish`. Both products currently publish as prereleases.

Workflow sources live in `cinzel/`. After editing HCL, regenerate GitHub Actions YAML:

```sh
cinzel github parse --directory cinzel --output-directory .github/workflows --yml
```
