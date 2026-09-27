# Package audit and update design

## Purpose

Conficius inspects package state across machines, projects, and workspaces. It reports uncertainty instead of treating missing data as healthy state. Mutations start from reviewed plans and use each manager's native commands.

## Supported release scope

Current adapters:

- `mise`: global and project runtime inventory, outdated checks, and updates.
- Homebrew: global formula and cask inventory, outdated checks, and updates.
- npm: global, project, and workspace inventory, outdated checks, and updates.

Current frontends:

- CLI: inspection, vulnerability scans, planning, and guarded updates.
- TUI: read-only package, drift, and vulnerability views.

Other managers in original product plan remain future work. See [roadmap.md](roadmap.md).

## Scope model

Normalized scope kinds are:

- `system`
- `user`
- `global-config`
- `global` for current compatibility
- `project`
- `workspace`

Project and workspace scopes require paths. Global kinds reject paths. Query paths become absolute. Physical roots reached through symlinks are queried once. If same root is both project and workspace, workspace label wins.

CLI presents machine-owned scopes as global where distinction does not affect behavior.

## Manager discovery

Manager status reports executable evidence separately from global profile-config evidence. Project manifest evidence comes from these files:

| Evidence | Manager |
|---|---|
| `mise.toml`, `.mise.toml` | mise |
| `Brewfile` | Homebrew |
| `package.json` | npm |

Manifest evidence makes manager required. Missing required manager is error. Optional manager without executable is unavailable warning. Global profile config does not make manager required, and warning identifies config evidence. Explicitly disabled manager remains visible as disabled. Disabled manager required by manifest is configuration error.

Configured executable paths override `PATH` discovery. Path must exist, be regular executable file, and return manager version when possible.

## Package model

Each normalized package can carry:

- canonical identity composed from manager and native package name
- installed, requested, allowed, and latest versions when known
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

Project profile can select npm classes. `--transitive` explicitly returns full npm graph and overrides configured class filtering for that request.

## Inspection behavior

Inspection runs managers concurrently with bounded concurrency. Commands within one manager remain serialized unless adapter later declares safe parallelism. Output merges deterministically.

Successful manager results survive failures from other managers or scopes. Status can be available, unavailable, disabled, unsupported, partial, or error.

Manager integrity checks executable availability and tested version range without registry or freshness queries.

Freshness and vulnerability are separate:

- normal outdated check uses manager-native network behavior
- offline mode reports installed inventory with freshness unknown
- vulnerability scan uses exact installed versions and explicit ecosystem mappings
- unmapped package is unknown, not clean

`packages outdated --check` exit codes:

- `0`: current and inspection succeeded
- `1`: updates or configured drift found
- `2`: inspection failed

## Update safety

Update command previews by default. Mutation requires `--apply` and explicit scope or profile selection.

Plans include:

- manager executable and arguments
- scope and working directory
- selected packages
- owning files
- confidence
- impact metadata
- recovery commands when manager supports them
- state hash bound to manager versions and package state

Unattended update also requires `--yes` and accepted plan hashes. Stale or unselected hashes fail.

Project mutation checks relevant manifests and lockfiles. Dirty or unversioned ownership files block mutation unless explicit override. Unrelated repository changes can remain.

Mutations run sequentially and fail fast by default. `--best-effort` continues independent manager groups. Cancellation stops new commands. Journal records per-plan outcomes, changed files, interruption, verification failures, and recovery commands. Secrets and machine identifiers are redacted.

Manager version outside tested range permits inspection but blocks mutation unless explicit override.

## Output contracts

JSON output is schema-versioned. Snapshot metadata includes timestamp, platform, CLI version, adapter versions, and manager versions. It excludes hostname, username, home path, and unrelated machine identifiers.

Human output stays grouped by manager and actionable condition. Partial and unknown states remain visible.