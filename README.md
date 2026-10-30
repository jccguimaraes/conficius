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

macOS binaries are not signed or notarized by Apple. Homebrew casks remove quarantine from the installed binary, bypassing Gatekeeper for that file. Direct downloads may still be blocked. Install only if you trust the artifacts.

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

CLI releases use `cli-vX.Y.Z`; TUI releases use `tui-vYYYY.MM.revision`. Each product has its own version, changelog, archives, and Homebrew cask. Shared changes may warrant both releases, but neither forces the other. CLI prereleases use `-alpha.N`, `-beta.N`, or `-rc.N`; TUI releases are stable. Build metadata is not supported in release versions.

Dispatch the Release workflow from `main` and choose CLI or TUI. Version is calculated automatically; the version input is an optional override. No commit SHA input is needed: both jobs use the exact `main` commit captured when the workflow was dispatched. GitHub release notes are generated with git-cliff from the selected command, shared internal packages, module files, and its GoReleaser config, bounded by that product’s previous reachable tag. Checked-in changelog updates are optional and do not gate publishing.

The workflow calculates and validates the version once, validates the captured SHA, runs checks and a snapshot build, then creates the product tag and publishes through GoReleaser. The `release` GitHub environment can require approval before publishing; configure its protection rules in repository settings. Tag pushes do not publish. Releases are serialized across both products. Publish versions in increasing order: CLI stable releases may update GitHub’s repository-wide latest slot; TUI downloads must use product tags or Homebrew.

GoReleaser updates only the selected stable cask in `jccguimaraes/homebrew-tap`. Set repository Actions secret `HOMEBREW_TAP_TOKEN` with Contents read/write access to that tap. The normal `GITHUB_TOKEN` publishes tags and release assets. Prereleases skip stable cask updates.

Existing tags are rejected. If publication fails after tag creation, inspect the tag, GitHub release, uploaded assets, and tap commit before manual recovery; do not blindly delete tags or overwrite published releases. No custom retry or rollback subsystem is maintained. Binaries are not Apple-signed or notarized. On macOS, generated Homebrew casks remove quarantine only from their staged binary; system-wide Gatekeeper settings remain unchanged.

Local release tasks use the pinned GoReleaser from mise:

```sh
mise run release-check
mise run release-snapshot-cli
mise run release-snapshot-tui
PRODUCT=cli mise run next-version
PRODUCT=tui mise run next-version
PRODUCT=cli mise run changelog
PRODUCT=tui mise run changelog
# Changelog commands above are optional; publish only the selected product.
mise run release-cli
mise run release-tui
```

CLI version defaults come from git-cliff’s Conventional Commit bump calculation, filtered to CLI and shared paths. TUI defaults use the current UTC year/month and one more than the highest local tag revision for that month, starting at `1`. Fetch remote tags first (`git fetch origin --tags`) so calculations include published releases. Set `VERSION` to override either default. Publish tasks dispatch to `main`; CI calculates the version once and passes that exact version to packaging, even across a month boundary. Local changelog previews can differ if commits or tags change before dispatch. `mise run check-release-versions` checks defaults, overrides, and release-note headings without publishing.

Snapshot tasks build archives and generate casks without publishing. Both use `dist/`; run them sequentially. Publish tasks dispatch against `main`, not the local checkout’s SHA; tokens stay in Actions secrets. Merge this workflow before dispatching a release.

`mise run setup` installs hk hooks through mise, replacing the previous commitlint hook path when configured. Pre-commit checks formatting and generated-workflow drift without modifying or staging files; commit-msg runs commitlint. `mise run check` runs the same read-only checks across the repository in CI. Full race tests remain separate.

</details>
