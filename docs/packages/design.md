# Package audit and update design

## Purpose

Conficius inspects package state across machines, projects, and workspaces. It reports uncertainty instead of treating missing data as healthy state. Mutations start from reviewed plans and use each manager's native commands.

## Supported release scope

Current adapters:

- `mise`: global and project runtime inventory, outdated checks, and updates.
- Homebrew: global formula and cask inventory, outdated checks, and updates.
- npm: global, project, and workspace inventory, outdated checks, and updates.
- `mas`, `uv`, and `pipx`: global inventory, outdated checks, and updates.
- Go: journal-backed global tool inventory, outdated checks, and updates; project and workspace module inventory and outdated checks through `go.mod`, plus pinned native `go get` project updates guarded by manifest permission and Git checks; project updates do not change the global tool journal or promise rollback.
- Poetry and pnpm: project inventory, outdated checks, and updates.
- Cargo: global installed-crate and project lockfile inventory; outdated checks and updates unsupported.
- Yarn: project inventory, registry latest-tag checks, and selected-package upgrades with manifest/Git guards; workspace outdated/update operations are rejected. Local and alias resolutions are skipped.
- Bun: project inventory, actionable range-compatible outdated checks, and selected-package updates with manifest/Git guards.
- apt: global system inventory and cached-list outdated checks on apt-based Linux; project operations and updates unsupported.

Current frontends:

- CLI: inspection, vulnerability scans, planning, and guarded updates.
- TUI: installed/latest/security status, package and vulnerability details, update preview and apply; `p` switches profiles and `r` reloads package config and refreshes.

Adapter capability gaps and additional managers remain in [roadmap.md](roadmap.md).

## Configuration

Package commands and TUI use independent central `config.toml`, not dotfile project configuration. Default location is `$XDG_CONFIG_HOME/conficius/config.toml` (fallback `~/.config/conficius/config.toml`). OS-native alternatives are `~/Library/Application Support/conficius/config.toml` on macOS and `%APPDATA%/conficius/config.toml` on Windows. Both locations existing is an error; missing config uses defaults. `--config` selects an explicit file. `config layout --xdg|--os-native` previews creation/movement; `--apply` writes it.

Package config requires strict schema version 2. Profile selection follows CLI, `CONFICIUS_PROFILE`, central config `profile`, then `default`. Profiles group named project paths and manager `enabled` / optional `run_as` settings; each project may opt into `allow_manifest_changes` for update safety. Native manifests and lockfiles remain the source of truth for dependency intent and versions. There are no profile package include/exclude or allow/deny lists, desired-version drift, manager ordering, security scope, executable overrides, manager-specific tuning, or npm dependency-class filters. CLI update `--include`/`--exclude` selectors remain request-local.

Manager `run_as` selects an existing local account for global inspection and supported package updates on macOS/Linux; Windows delegation is unsupported. For example, `[profiles.work.managers.brew]` with `enabled = true` and `run_as = 'special-username'` runs Homebrew as its non-root owner. Use CLI `--global` or TUI global scope. Project/workspace delegation, delegated manager self-updates, and delegated APT cache refresh are blocked. APT remains inspection-only; root delegation does not enable upgrades. The separate Linux TUI `c` cache-refresh action still uses sudo/root when APT has no `run_as` setting. See [manager accounts and privileges](../cli.html#manager-run-as) for terminal and account requirements.

## Scope model

Normalized scope kinds are:

- `system`
- `user`
- `global-config`
- `global` for current compatibility
- `project`
- `workspace`

Project and workspace scopes require paths. Global kinds reject paths. Query paths become absolute. Physical roots reached through symlinks are queried once. If same root is both project and workspace, workspace label wins.

CLI presents machine-owned scopes as global where distinction does not affect behavior. Without scope selectors, queries include global packages and configured profile projects. When no projects are configured, working-directory discovery recognizes five files: `package.json`, `Cargo.toml`, `go.mod`, `mise.toml`, and `.mise.toml`. Go module discovery is independent of mise runtime discovery; both managers can inspect the same root.

## Manager discovery

Manager status reports executable evidence separately from global profile-config evidence. Project manifest evidence comes from these files:

| Evidence | Manager |
|---|---|
| `mise.toml`, `.mise.toml` | mise |
| `Brewfile` | Homebrew |
| `package.json` | npm |
| `go.mod` | Go |

Manifest evidence makes manager required. Missing required manager is an error. Optional manager without an executable is an unavailable warning. A profile manager table provides global-config evidence; `enabled = true` also makes the manager required, `enabled = false` disables it, and omitted `enabled` leaves it optional. A disabled manager remains visible; a disabled manager required by a manifest is a configuration error.

Executables are discovered through `PATH`; profiles cannot override executable paths. Discovery validates executable files and queries manager versions when possible.

## Package model

Each normalized package can carry:

- canonical identity composed from manager and native package name
- installed, native requested constraints, and latest versions when known
- role
- scope and canonical path
- owning manifest or config
- npm dependency class
- direct or transitive status
- exposed command names with canonical package ownership
- collision candidates only when distinct global owners expose same command
- resolved command path only for collision candidates
- warnings name command, resolved path when known, and canonical owners
- offline CLI JSON includes typed collision candidates
- native executable used for operation

Roles are independent from scope:

- `runtime`
- `library`
- `development-tool`
- `application`
- `package-manager`
- `plugin`

Current role mapping:

- mise tools: runtime
- Homebrew formulae: library
- Homebrew casks: application
- npm development dependencies: development tool
- other npm packages: library
- manager binaries: package manager

npm dependency classes:

- `runtime`
- `development`
- `optional`
- `peer`
- `transitive`
- `extraneous`

Class precedence for manifest overlap is development, optional, peer, then runtime. An installed scoped npm package absent from manifest is extraneous. Nested packages are transitive.

Dependency classes are descriptive metadata, not profile filters. Project inventory uses `npm ls --package-lock-only --json` and does not require `node_modules`. CLI vulnerability `--transitive` includes the full npm inventory graph.

## Inspection behavior

Inspection runs managers concurrently with bounded concurrency. Commands within one manager remain serialized unless adapter later declares safe parallelism. Output merges deterministically.

Successful manager results survive failures from other managers or scopes. Status can be available, unavailable, disabled, unsupported, partial, or error.

`packages doctor` checks executable availability and tested version range without registry or freshness queries. For npm lockfiles v2 and v3, it checks that root lock metadata contains each direct manifest dependency. Older formats report unknown. It is read-only.

Freshness and vulnerability are separate:

- normal outdated check uses manager-native network behavior
- Homebrew runs `brew update` before `brew outdated --json=v2 --greedy` so latest metadata is refreshed; refresh failure fails that operation without using stale results
- apt uses `dpkg-query` for inventory and `apt-get --simulate` for outdated checks against locally cached lists, not live latest versions; no automatic list refresh or privilege escalation during inspection unless explicitly configured via `run_as`. Users explicitly run `sudo apt-get update` separately before inspection when needed, or on Linux without APT `run_as`, press `c` (Update package cache, currently APT) on TUI package scopes/results to preview the cache-refresh command (`y` approves; `n`/Esc cancels). Execution uses no shell; sudo handles passwords through the controlling TTY, or root runs apt-get directly. Success restarts the TUI and refreshes outdated/security results; failure or cancellation shows a notice and leaves local inventory only
- apt has no Debian security identity mapping; coverage remains unknown, not clean
- offline mode reports installed inventory with freshness unknown
- vulnerability scan uses exact installed versions and explicit ecosystem mappings
- unmapped package is unknown, not clean

`packages outdated --check` exit codes:

- `0`: current and inspection succeeded
- `1`: updates found
- `2`: inspection failed

## Update safety

CLI update previews by default. Mutation requires `--apply` and explicit scope or profile selection. TUI uses marked/all-package selection, command preview, and confirmation before applying.

Plans include:

- manager executable and arguments
- scope and working directory
- selected packages
- owning files
- confidence
- impact metadata
- recovery commands when manager supports them
- state hash bound to commands, executable, scope, impact, recovery data, and package state (not the manager version string)

Unattended update also requires `--yes` and accepted plan hashes. Stale or unselected hashes fail.

Project mutation requires a matching registered project with `allow_manifest_changes = true` (`AllowManifestChanges`), default false. CLI `--allow-dirty` and `--allow-unversioned` do not bypass this permission. Current Git guard checks the whole repository: any uncommitted changes block mutation without `--allow-dirty`, and non-Git projects need `--allow-unversioned`. TUI grants neither override.

CLI mutations run sequentially and fail fast by default. `--best-effort` continues independent manager groups. Cancellation stops new commands. Journal records per-plan outcomes, changed files, interruption, verification failures, and recovery commands. Secrets and machine identifiers are redacted.

CLI manager version outside tested range permits inspection but blocks mutation unless explicit override.

TUI uses shared plan/execution APIs but not the CLI accepted-hash, journal, post-update verification, or manager-version guard flow. It rechecks project safety before applying, runs commands sequentially with terminal I/O, retains per-package failures while continuing subsequent commands, and restarts to refresh installed/latest/security state.

## Output contracts

JSON output is schema-versioned. Snapshot metadata includes timestamp, platform, CLI version, adapter versions, and manager versions. It excludes hostname, username, home path, and unrelated machine identifiers.

Human output stays grouped by manager and actionable condition. Partial and unknown states remain visible.