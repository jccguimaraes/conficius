# Conficius

Conficius manages dotfiles, checks outdated packages and vulnerabilities, and previews or applies package updates on macOS and Linux. It has separate CLI and TUI binaries.

Early beta. Feedback and bug reports are welcome through GitHub issues.

[Project site](https://jccguimaraes.github.io/conficius/) · [Releases](https://github.com/jccguimaraes/conficius/releases)

<p align="center">
  <img src="./docs/assets/conficius-gopher.png" alt="Conficius Gopher" width="300">
</p>

> Inspired by the Go [gopher](https://go.dev/doc/gopher) mascot created by Renee French.

## Install

### Homebrew (macOS)

```sh
brew install --cask jccguimaraes/tap/conficius
brew install --cask jccguimaraes/tap/conficius-tui
```

### Binaries (macOS and Linux)

Download from [GitHub releases](https://github.com/jccguimaraes/conficius/releases):

- CLI: `conficius_VERSION_OS_ARCH.tar.gz`
- TUI: `conficius-tui_VERSION_OS_ARCH.tar.gz`

Choose `darwin` or `linux`, then `arm64` or `amd64`. Verify against `checksums.txt`, extract, and put the binary on `PATH`. Either binary runs independently.

macOS binaries are not signed or notarized by Apple and may be blocked by Gatekeeper. Install only if you trust the artifacts; no automatic Gatekeeper bypass is included.

## Start

Package operations need the corresponding package manager installed. Online checks need network access.

```sh
conficius packages pms
conficius packages outdated
conficius-tui
```

For dotfiles, run from your repository:

```sh
conficius init
```

Edit the generated link examples, then preview before applying:

```sh
conficius dotfiles link
conficius dotfiles link --apply
```

Updates and links require explicit approval. Previews can query registries and run manager inspection commands; they are not offline operations. Package updates have no universal rollback. Keep backups of configuration and project files.

## Configuration

Dotfile links and package settings use separate TOML files, both with `version = 2`.

If you have a dotfiles repository, keep link definitions there:

```text
/path/to/repo/dotfiles/
└── conficius.toml
```

Conficius’s own package settings live separately:

```text
~/.config/
└── conficius/
    └── config.toml
```

Missing package config uses defaults. To select files explicitly in CLI and TUI:

```sh
export CONFICIUS_DOTFILES_CONFIG="/path/to/repo/dotfiles/conficius.toml"
export CONFICIUS_PACKAGES_CONFIG="$HOME/.config/conficius/config.toml"
```

Replace the repository path with your own. Explicit flags override environment variables, which override discovery. These variables hold file paths, not TOML contents; missing or invalid selected files return errors. `init` and `config layout` ignore them.

See the [configuration reference](https://jccguimaraes.github.io/conficius/cli.html#config) for discovery locations, profiles, Zsh links, and symlink safety.

## Documentation

- [CLI commands and supported managers](https://jccguimaraes.github.io/conficius/cli.html)
- [Package updates and safeguards](https://jccguimaraes.github.io/conficius/cli.html#packages-update)
- Run `conficius COMMAND --help` for installed-version options, or press `?` in the TUI.

## Development

Use `mise install` for pinned tools, then:

```sh
mise run setup
mise run test-ci
mise run lint
mise run build-cli
mise run build-tui
```

CI runs race-enabled tests, vet, builds, and CLI smoke tests on Linux and macOS. Lint and release-config checks run on Linux.

<details>
<summary>Release publishing</summary>

CLI releases use `cli-vX.Y.Z`; TUI releases use calendar tags shaped `tui-vYYYY.MM.revision`. Tags must point to commits included in `main`; CLI versions also need a matching changelog heading. Tag pushes verify release state without publishing. To publish, manually run the matching release workflow against its tag with `confirm` set to `publish`. Both products currently publish as prereleases.

Publishing also updates `conficius` and `conficius-tui` casks in `jccguimaraes/homebrew-tap`. Set the repository Actions secret `HOMEBREW_TAP_TOKEN` to a token with Contents read/write access to that tap. The normal `GITHUB_TOKEN` still publishes release assets; the tap token is used only for tap updates. Cask uploads are explicitly enabled for the current prerelease policy. Binaries are not Apple-signed or notarized; no automatic Gatekeeper bypass is configured.

Local release tasks use the pinned GoReleaser from mise:

```sh
mise run release-check
mise run release-snapshot-cli
mise run release-snapshot-tui
TAG=cli-vX.Y.Z mise run release-cli
TAG=tui-vYYYY.MM.revision mise run release-tui
```

Snapshot tasks build archives and generate casks without publishing. Both use `dist/`; run them sequentially. Publish tasks dispatch GitHub Actions against an existing remote tag, so the tap token stays in Actions secrets. They do not create tags. Merge publishing configuration before creating the next release tags; existing tags retain their original workflows and will not gain tap publishing by rerunning them.

</details>
