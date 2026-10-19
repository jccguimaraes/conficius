# Conficius

Conficius manages dotfiles, checks outdated packages and vulnerabilities, and previews or applies package updates on macOS and Linux. It has separate CLI and TUI binaries.

Early beta. Feedback and bug reports are welcome through GitHub issues.

[Project site](https://jccguimaraes.github.io/conficius/) · [Releases](https://github.com/jccguimaraes/conficius/releases)

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

`conficius init` creates a dotfiles `conficius.toml` in the current directory and refuses to overwrite an existing file. Edit its commented link examples before use. Dotfiles discovery searches the current directory and its parents, then `$XDG_CONFIG_HOME/conficius/conficius.toml` (default: `~/.config/conficius/conficius.toml`).

Package settings are independent: `$XDG_CONFIG_HOME/conficius/config.toml` (default: `~/.config/conficius/config.toml`) or, on macOS, `~/Library/Application Support/conficius/config.toml`. Discovery fails if both package locations exist. Missing package config uses defaults; package commands never read repository `conficius.toml`.

`CONFICIUS_DOTFILES_CONFIG` and `CONFICIUS_PACKAGES_CONFIG` hold paths to the dotfiles and package config files respectively, not TOML contents or directory paths. Both CLI and TUI use them. Explicit config flags take precedence over environment variables, which take precedence over discovery. Empty environment variables keep normal discovery; missing or invalid files selected by these variables return errors. `init` and `config layout` do not use these variables.

```sh
export CONFICIUS_DOTFILES_CONFIG="$HOME/dotfiles/conficius.toml"
export CONFICIUS_PACKAGES_CONFIG="$HOME/dotfiles/.config/conficius/config.toml"
```

The file selected by `CONFICIUS_PACKAGES_CONFIG` configures Conficius itself: package profiles, manager settings, layout, and optional vulnerability mappings. A minimal file contains only `version = 2`. Dotfile links belong in the separate file selected by `CONFICIUS_DOTFILES_CONFIG`. Create both files before using these exports.

CLI `--config FILE` is command-specific: dotfiles commands, `init`, and `config migrate` use a dotfiles config; package inspection/update commands and `config resolve` use a package config. Put the flag before the command. `config layout` rejects it and works only with the central package locations:

```sh
conficius config layout --xdg             # preview creation or relocation
conficius config layout --xdg --apply
conficius config layout --os-native      # preview native layout
conficius config layout --os-native --apply
```

Layout application retains existing config as `.conficius-layout-backup-*` beside its original path. Without a layout flag it uses the stored `layout`, defaulting to `xdg`. On Linux, XDG and native package locations coincide.

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

Link sources can be files or folders. A folder link points to the entire source directory; it does not copy files or merge directories. Writes through the link, including files and folders generated later by tooling, land in the source directory in your dotfiles repository.

Add generated files and folders that should not be tracked to your dotfiles repository's `.gitignore`. For example, if Zsh stores runtime data under the linked `zsh/` folder:

```gitignore
/zsh/.zsh_sessions/
/zsh/.zsh_history
/zsh/.zsh_history.*
/zsh/.zcompdump*
/zsh/*.zwc
/zsh/*.zwc.old
```

Adjust paths to match your layout and history settings. `.gitignore` affects Git tracking, not file creation or linking; already-tracked files must be untracked separately. Link individual config files instead if runtime data must stay outside the repository.

Existing regular files and directories are never overwritten. Forced replacement is limited to symlinks whose destination remains inside the current config repository. All links are validated before mutation, and failed batches are rolled back.

Package commands inspect global installations and project dependencies across 13 managers. Manager capabilities vary; `conficius packages pms --json` reports which inventory, outdated, and update operations each manager supports.

| Manager / `--pm` | Global inventory | Project inventory | Global outdated | Project outdated | Global update | Project update |
| --- | --- | --- | --- | --- | --- | --- |
| mise / `mise` | Yes | Yes | Yes | Yes | Yes | Yes |
| Homebrew / `brew` | Yes | No | Yes | No | Yes | No |
| npm / `npm` | Yes | Yes | Yes | Yes | Yes | Yes |
| Mac App Store / `mas` | Yes | No | Yes | No | Yes | No |
| uv / `uv` | Yes | No | Yes | No | Yes | No |
| pipx / `pipx` | Yes | No | Yes | No | Yes | No |
| Poetry / `poetry` | No | Yes | No | Yes | No | Yes |
| pnpm / `pnpm` | No | Yes | No | Yes | No | Yes |
| Cargo / `cargo` | Yes | Yes | No | No | No | No |
| Yarn / `yarn` | No | Yes | No | Yes | No | Yes |
| Bun / `bun` | No | Yes | No | Yes | No | Yes |
| Go / `go` | Yes | Yes | Yes | Yes | Yes | Yes |
| apt / `apt` | Yes | No | Yes | No | No | No |

Bun and pnpm use native range-respecting `update` commands for selected project packages. Bun reports actionable updates only; packages already at their range-compatible target are omitted even when a newer major exists. Yarn 4 checks registry `latest` tags and uses `yarn up` for selected names, which can cross declared ranges. Yarn updates and outdated checks currently reject workspace projects to prevent changes outside the approved project. Yarn local, workspace, and alias resolutions are skipped. Bun and Yarn updates require manifest-change permission and project Git safeguards; neither has global support.

These are declared adapter capabilities, not all operations offered by the upstream tools. Existing `go.mod` files in the current working directory are auto-detected for project inspection. A project containing both `mise.toml` and `go.mod` is inspected by both mise (configured tools) and Go (module dependencies); neither replaces the other.

```sh
conficius packages pms
conficius packages pms --json
conficius packages outdated
conficius packages outdated --pm mise --pm npm
conficius packages outdated --global --pm apt --offline
conficius packages outdated --global --pm apt
conficius packages vulnerabilities --global --pm apt
conficius packages outdated --pm npm --project ./my-app
conficius packages vulnerabilities --pm npm --project ./my-app
conficius packages vulnerabilities --pm npm --project ./my-app --transitive
conficius packages outdated --pm go --project ./my-go-app
conficius packages vulnerabilities --pm go --project ./my-go-app
conficius packages update
conficius packages update --global --pm npm --include npm:typescript --apply
conficius --config ~/.config/conficius/config.toml packages outdated --profile work
conficius packages update --profile work --profile-project app --pm npm
```

Package-manager inspection reports unavailable and failed managers without hiding other results. Updates preview by default; `--apply` runs native manager commands and requires explicit scope intent. Command-line `--include`/`--exclude` select updates, not persistent profile rules. Conficius re-queries available updates before execution; refreshed plans can differ from the displayed preview. Non-interactive `--yes` requires exact `--accept-plan` hashes. Updates have no universal rollback; keep backups and review the [safeguards and current limits](https://jccguimaraes.github.io/conficius/cli.html#updates). Inspection commands support schema-versioned JSON with `--json`.

Go inspects global installed tools through its explicit ownership journal at `$XDG_STATE_HOME/conficius/go-tools.json` (default: `~/.local/state/conficius/go-tools.json`), not by scanning all installed binaries. `packages go-tools import` records module/version/command ownership without installing tools; a missing journal means empty global inventory. Global outdated checks and updates cover those recorded tools only. Go project inventory uses native `go list -m -json -mod=readonly all`; project freshness uses `go list -m -u -json -mod=readonly all`. Both consume Go's JSON object stream. By default, project outdated results include direct dependencies and modules providing explicitly declared `tool` packages in the scope's `go.mod`, including indirect tool modules. Vulnerability inventory retains the full module graph; security queries use the explicit scan selection described below. Inspection does not mutate project manifests, but Go may query or download module metadata and populate caches: read-only is not offline. CLI and TUI project updates batch selected modules as `go get module@version ...` in the project directory, pinned to previewed versions; global tools still use one `go install module@version` per module. Project updates require configured `allow_manifest_changes = true` and Git safety checks. Native Go may change `go.mod`, `go.sum`, transitive dependencies, and Go/toolchain requirements while retaining existing `tool` directives; review the diff and run project tests. Plan hashes include both manifests (including a missing `go.sum`); changed files invalidate accepted previews. Project updates never change the global tool journal and offer no automatic rollback or recovery command; keep Git history or backups.

Go security scans default to direct dependencies and explicitly declared tool modules. `--include-indirect` adds indirect modules used by applications, tests or tools, including nested tool dependencies. `--include-graph-only` independently adds modules not confirmed used by the current build; it does not mean indirect-but-used. Neither optional group is queried by default. JSON contains all evidence from the selected scan only, with independent `transitive`, `declared_tool`, `dependency_class` and `scan_scope` metadata. Skipped modules are not clean results.

The TUI scope summary starts at `Direct + tools · Indirect off · Graph off`. Direct dependencies and declared tools stay included when either optional group is toggled. On narrow terminals, the summary uses two lines. An empty completed scan says “No findings in selected scope”; an incomplete scan says “No findings yet.” Neither message establishes coverage outside the selected scope.

TUI vulnerability views are read-only: `i` toggles indirect scanning and `g` toggles separate graph-only scanning. Each restarts security work for the active scope using cached inventory, without repeating outdated checks; expanded scans can take time. Options persist for the TUI session. In vulnerabilities or outdated results, `x` cancels an active vulnerability scan without stopping inventory or outdated checks. Canceled scans remain incomplete, not clean; retained findings may be from the previous scan. Use `i`/`g` or `r` to restart. The cancel footer appears only while security scanning is active. Dim Origin labels describe direct/indirect/declared-tool membership, not marks. Enter explains installed version, advisory, fixed version and current-build usage; no update keys exist here. Busy phases show dependency graph loading, installed-version checking and security checking with known counts, never estimated percentages.

Go replacements need care: local filesystem replacements have no registry identity, so they cannot establish registry vulnerability coverage. Versioned replacements use the actual replacement module path and version for security checks; freshness must not imply that updating the original module updates the replacement.

Online Homebrew outdated queries run `brew update` before `brew outdated`, including queries made during previews. Offline inventory skips freshness checks; preview does not mean offline or read-only metadata.

apt inspects global system packages on Debian, Ubuntu, and other apt-based Linux systems only. Installed inventory uses `dpkg-query`; outdated inspection uses an `apt-get` simulation against locally cached package lists, not live latest versions. `--offline` reads installed inventory only, with freshness unknown. Conficius never refreshes apt metadata automatically. To refresh it, explicitly run `sudo apt-get update` yourself before inspection, or on Linux, press `c` (Update package cache, currently APT) on TUI package scopes/results screens. The TUI previews the command: `y` approves; `n`/Esc cancels. It runs `sudo -- <apt-get> update --error-on=any` through the controlling TTY, with passwords handled by sudo; root runs apt-get directly. No shell is used. Success restarts the TUI and refreshes outdated/security results; failure or cancellation shows a TUI notice and leaves local inventory only. apt has no project or update capability in Conficius: no automated apt upgrades, root invocation, or privilege escalation.

Vulnerability checks query OSV with exact installed versions. Global and project npm packages map directly to OSV's `npm` ecosystem. Versioned Go modules map to OSV's `Go` ecosystem using their module path and exact version, subject to the replacement limitations above. Homebrew and mise packages require explicit mappings because names alone do not prove package identity. apt packages have no automatic Debian OSV mapping or coverage assurance; unmapped packages must be reported as unknown, not clean. Missing mappings are not evidence that a package is clean. Vulnerability findings do not change exit status; inventory or query failures do.

## Configuration

Both files require TOML schema `version = 2`, but serve separate purposes: repository `conficius.toml` defines dotfile links; central `config.toml` defines package profiles, layout, and optional vulnerability mappings.

### Manage Conficius from your dotfiles repository

This macOS example keeps Conficius's own package config in `~/dotfiles` and links its directory into `~/.config`. macOS uses the Go platform name `darwin`; the package config still uses the supported `xdg` layout, not `os-native`.

```text
~/dotfiles/
├── conficius.toml
└── .config/
    └── conficius/
        └── config.toml
```

Create the source directory and files first. Replace `/path/project-x` and `/path/project-y` with your existing project directories, or create those directories before using their scopes. They are placeholders, not paths to create at the filesystem root. No root privileges or `sudo` are needed for these user-owned config files and links.

Save this complete link config as `~/dotfiles/conficius.toml`:

```toml
version = 2

[[tools.conficius.links]]
source = ".config/conficius"
target = "$HOME/.config/conficius"
platforms = ["darwin"]
```

Save this complete package config as `~/dotfiles/.config/conficius/config.toml`:

```toml
version = 2
layout = "xdg"
profile = "default"

[[profiles.default.projects]]
name = "project-x"
path = "/path/project-x"

[[profiles.work.projects]]
name = "project-y"
path = "/path/project-y"
```

If `~/.config/conficius` already exists as a regular directory, linking will not overwrite or merge it, even with `--force`. Back it up, copy any settings you need into the repository source, then manually relocate the existing directory outside the target path before applying. Preserve the backup until you verify the new setup. Other conflicting real files or unrelated symlinks also require manual resolution.

Preview first, inspect the target, then apply:

```sh
conficius --config ~/dotfiles/conficius.toml dotfiles link conficius
conficius --config ~/dotfiles/conficius.toml dotfiles link conficius --apply
```

Add `"linux"` to `platforms` to use this link on Linux too; omit `platforms` to allow every platform. The target above is fixed at `~/.config/conficius` and ignores a custom `XDG_CONFIG_HOME`. For that setup, use `target = "${XDG_CONFIG_HOME}/conficius"` instead; the resolved target must remain inside your home directory.

After linking, edit `~/dotfiles/.config/conficius/config.toml`; reads through the central path see the same file. In TUI package views, `r` reloads settings and project scopes. Press `p`, then `Enter`, to switch profiles for the session without changing the file. The selected profile supplies its projects alongside default/global package scopes: `default` adds `project-x`, while `work` adds `project-y`. Profiles are independent; `work` does not inherit `default` projects or manager settings.

Version control the repository files deliberately: project paths are machine-local and may need adjustment on another machine. Do not commit secrets. Directory links are live, so runtime writes also land in the repository; `.gitignore` does not protect secrets or remove already tracked files.

Repository linking and `config layout` migration are separate workflows. Choose repository linking for this self-managed setup; changing `layout` does not move files automatically. Layout relocation requires a regular source config file and refuses a symlink source. A directory symlink does not itself make the contained `config.toml` a symlink; do not treat migration as a way to manage or relocate repository links.

### Package config fields

| Field | Required / default | Meaning |
| --- | --- | --- |
| `version` | Required: `2` | Package config schema version. |
| `layout` | Optional: `"xdg"` | `"xdg"` or `"os-native"`; chooses the default for an explicit `config layout` action, not automatic relocation. |
| `profile` | Optional: `"default"` fallback | Selection priority: `--profile NAME`, `CONFICIUS_PROFILE`, this field, then `default`. |
| `profiles.<name>.managers.<manager>.enabled` | Optional: `true` | Managers stay enabled unless explicitly disabled. Enabling one does not disable others. |
| `profiles.<name>.managers.<manager>.run_as` | Optional: current user | Local account for global manager operations, e.g. `run_as = "builder"` under `[profiles.work.managers.brew]`. Uses `sudo -i -u USER` with explicit foreground `/dev/tty` confirmation; no password arguments or fallback. |
| `profiles.<name>.projects[].name` | Required per project | Unique project name within the profile, used by `--profile-project`. |
| `profiles.<name>.projects[].path` | Required per project | Absolute path or path relative to the package config file's directory, not the repository root. Supports `$HOME`, `${HOME}`, and other set environment variables via `$VAR` or `${VAR}`; HOME/XDG variables have defaults. Unset variables without defaults fail; `~` is not expanded. |
| `profiles.<name>.projects[].allow_manifest_changes` | Optional: `false` | Set `true` explicitly to permit native project updates that may change manifests or lockfiles. Git overrides do not bypass this guard. |
| `vulnerabilities.mappings` / `vulnerabilities.euvd` | Optional | Explicit vulnerability identities; see [vulnerability checks and security caveats](docs/cli.html#vulnerabilities). |

For example, disable a manager with `[profiles.work.managers.brew]` and `enabled = false`. Native manifests and lockfiles remain the authority for versions and dependency declarations; Conficius does not store a second version policy or executable overrides.

```sh
conficius config resolve --profile work --verbose
conficius packages outdated --profile work --profile-project project-y --pm npm
```

`run_as` currently supports global scopes only: use `--global` in CLI, or select global scope in TUI. Target account must exist, be allowed by sudo policy, and be able to execute this Conficius binary. Manager lookup uses target login environment; configure that account’s PATH rather than caller executable overrides. stdout carries bounded internal JSON; stdin and stderr use controlling terminal. TUI refresh stops and restarts before confirmation; results from target and current-user managers remain combined. Noninteractive execution cannot bypass this confirmation, even with `--yes`. Plans bind target username, executable, packages and command; worker rebuilds and checks plan before mutation and verifies afterwards. Project operations fail closed rather than running as caller. Manager self-update discovery for delegated managers is not supported.

Use separate accounts according to manager ownership: Homebrew normally runs as its non-root owner; APT system updates require root privileges and appropriate sudo authorization, but Conficius currently supports APT inspection only. Setting `run_as = "root"` does not enable unsupported APT upgrades. APT cache refresh with `run_as` is blocked; use an authorized target-account terminal instead. Switching to an unprivileged account does not grant APT write access. Grant only needed sudo permissions; Conficius does not install sudo rules or change ownership.

The example leaves project mutation disabled. Set `allow_manifest_changes = true` in the matching project entry only when you intend to permit updates, then preview before using `--apply`.

## TUI

Run `conficius-tui`, or `./bin/conficius-tui` after building from source. Select either config independently:

```sh
conficius-tui --config ~/dotfiles/conficius.toml
conficius-tui --package-config ~/.config/conficius/config.toml
conficius-tui --config ~/dotfiles/conficius.toml --package-config ~/.config/conficius/config.toml
```

The home screen opens package scopes or dotfile audits and linking. Package views use compact rows for outdated versions and vulnerability findings. Missing dotfiles show optional onboarding: “Add your dotfiles when you're ready.” Run `conficius init` in your dotfiles repo when needed; package use does not require dotfiles.

`Enter` on a package, vulnerability, dotfile link, or environment finding opens details with clear sections: bold cyan headings, dim labels and paths, and ordinary values in the terminal’s default color. Warnings use yellow, errors red; CVSS scores use blue for scores above 0 and below 4, yellow from 4 to below 9, and red for 9 or higher. Detail text wraps when resized and preserves scrolling, clamped to the available content.

- Arrows or `j`/`k`: move; `Enter`: open scope or details.
- In details, `↑`/`↓` or `k`/`j` scroll; `Ctrl-u`/`Ctrl-d` page up/down; `Esc` returns to the originating list.
- `Esc`: go back; `r`: reload configuration and project scopes, then refresh; `q`: quit; `?`: screen help.
- `p`: open profile picker from Home, scopes, or outdated packages; `Enter` selects. Selection overrides the profile for this session only; it is not saved to disk.
- In outdated packages, `Space` marks a package and `a` toggles all.
- `u` previews marked upgrades; `U` previews all upgrades in the current scope.
- In the command preview, `y` applies upgrades; `n` or `Esc` cancels.
- `v` opens vulnerabilities; `o` returns to outdated packages.

For dirty Git projects, the TUI first asks **Continue with uncommitted changes**, listing affected project paths and warning that updates have no automatic rollback. Press `y` to allow dirty Git state for that batch only, then review the normal command preview and press `y` again to apply; `n` or `Esc` cancels. Cancellation, a new preview, refresh, profile changes, and restart discard approval. Git safety is checked again before execution; a clean preview that becomes dirty requires confirmation. Manifest changes still require `allow_manifest_changes = true`; unversioned projects remain blocked. CLI users instead pass `--allow-dirty` or `--allow-unversioned` explicitly; neither bypasses manifest permission.

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
