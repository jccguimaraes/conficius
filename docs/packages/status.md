# Package subsystem status

## Implemented

### Profiles and scopes

- Independent central package `config.toml` with strict schema version 2; dotfile configuration is separate.
- XDG and OS-native layout discovery, conflict rejection, explicit `--config`, and preview/apply `config layout` moves.
- CLI, `CONFICIUS_PROFILE`, central config selection, and default profile resolution.
- Profiles group named project paths and manager `enabled` and optional `run_as` settings, with per-project `allow_manifest_changes` update permission.
- Global, project, Cargo, and npm workspace inspection.
- macOS/Linux `run_as`: explicit terminal-confirmed sudo login-account global inspection and supported package updates, with target-side discovery and plan revalidation; no password capture or caller fallback. TUI merges target/current-user results after foreground prefetch. Project/workspace delegation, delegated manager self-updates, and delegated APT cache refresh are blocked; Windows delegation is unsupported. APT upgrades remain unsupported. See [account requirements and privilege examples](../cli.html#manager-run-as).
- Working-directory discovery recognizes five project files: `package.json`, `Cargo.toml`, `go.mod`, `mise.toml`, and `.mise.toml`. Go modules and mise runtimes are discovered independently in combined roots.
- npm project inventory, freshness, and doctor checks run only where `package.json` exists.
- npm project inventory uses `npm ls --package-lock-only --json`; global inventory reads installed packages.
- Absolute scope paths, symlink-aware deduplication, and workspace precedence.
- Native manifests and lockfiles own dependency intent and versions; no central package/version policy, drift, manager ordering, security-scope setting, executable overrides, or npm class filters.

### Managers

- Built-in mise, Homebrew, npm, and `mas` adapters.
- `mas` uses stable Adam ID identity with global inventory, outdated checks, and guarded update plans.
- `mas` updates require admin access and use explicit inaccurate/no-minimum-OS flags.
- CLI preview and unsupported project-scope coverage for `mas`.
- `mas` authentication, network, rate-limit, and changed-output parser failure fixtures.
- Registered `uv` adapter for global tool inventory, outdated checks, and selected-tool update plans.
- `uv` authentication, network, rate-limit, and changed-output parser failure fixtures.
- Registered `pipx` adapter for global inventory, outdated checks, and selected-package update plans.
- `pipx` authentication, network, rate-limit, and changed-output parser failure fixtures.
- Registered Poetry adapter for project inventory, outdated checks, and selected-package update plans.
- Poetry authentication, network, rate-limit, and changed-output parser failure fixtures.
- Registered pnpm adapter for project inventory, outdated checks, and selected-package update plans.
- pnpm authentication, network, rate-limit, and changed-output parser failure fixtures.
- Registered Cargo adapter for global installed-crate and project `Cargo.lock` inventory with native `crates.io` OSV identities, plus crates.io-only global/project outdated checks and updates without a plugin.
- Cargo: Global checks use `cargo info`’s compiler-compatible selected version, not its `latest` annotation. `.crates.toml` is authoritative for owned binaries, reconciled with `.crates2.json` install settings. No actual installs is an empty success; nonempty installs with missing/incomplete options fail closed. Versioned `cargo install` preserves root, owned binaries, features, default-feature setting, profile, and target. Registry/source overrides fail closed; Git, path, and alternate-registry installs are not update candidates.
- Project checks and updates require standalone projects with both `Cargo.toml` and `Cargo.lock`; workspace and path-dependency projects are rejected. Checks probe `cargo update name@current --dry-run`; each update uses `cargo update name@current --precise target`, binding the native dry-run’s range-compatible target. Duplicate consolidation reported only as `Removing` is skipped when no target can be established. Preflight rejects stale project manifests, lockfiles, Cargo configuration/environment, and changed global install settings or binary ownership. Project updates leave `Cargo.toml` unchanged but may change `Cargo.lock` and transitive dependencies; `allow_manifest_changes = true` and Git guards still apply. Inspection may access the network and write index/cache metadata; dry runs do not write the lockfile. Text parsing targets Cargo 1.99 and fails closed on unfamiliar output.
- Cargo command and changed-output parser failure fixtures.
- Registered Yarn 4 adapter for project inventory, registry latest-tag checks, and selected-package `yarn up` plans. Workspace outdated/update operations are rejected; local and alias resolutions are skipped. Updates require manifest permission and Git guards.
- Yarn command and changed-output parser failure fixtures.
- Registered Bun adapter for project inventory, actionable range-compatible outdated checks, and selected-package `bun update` plans. Updates require manifest permission and Git guards.
- Validated Go tool import records with canonical module path, semantic version, and command ownership.
- Read-only Go tool journal parser rejects unsupported schemas and ambiguous command ownership.
- `packages go-tools import` records explicit Go module/version/command ownership without running `go install`.
- Registered Go adapter with journal-backed global inventory and exposed-command ownership. Missing journal means empty inventory.
- Go project/workspace inventory uses `go list -m -json -mod=readonly all` with `GOPROXY=off`, `GONOPROXY=none`, `GOSUMDB=off`, and `GOTOOLCHAIN=local`; cache misses are errors.
- Go project/workspace outdated checks use `go list -m -json -mod=readonly -u all`; network access and metadata-cache writes are possible, but `go.mod` is not written. Replaced modules are excluded from outdated candidates; project updates use pinned `go get` batches with manifest permission and Git guards. Native resolution may change `go.mod`, `go.sum` and transitive dependencies; no automatic rollback is offered. Global tool installs and journal writes remain separate.
- Go native OSV inventory uses actual module path/version, including versioned replacements. Local replacements lack published identity and are omitted; omission is a security coverage gap, not a clean result.
- Go manager inspection uses `go version`; global outdated checks use structured `go list -m -json MODULE@latest` metadata.
- Go updates run one exact `go install module@version` plan per tool, write verified versions to journal, and use fake-executable execution coverage.
- Bun command and changed-output parser failure fixtures.
- Registered apt adapter for global system inventory via `dpkg-query` and outdated checks via `apt-get --simulate` against cached lists only on Debian/Ubuntu and other apt-based Linux systems. No project operations or automated package upgrades; inspection does not refresh metadata or automatically escalate privileges unless explicitly configured via `run_as`. Users explicitly refresh lists separately with `sudo apt-get update`, or on Linux without APT `run_as`, press `c` (Update package cache, currently APT) on TUI package scopes/results to preview the command (`y` approves; `n`/Esc cancels). No shell; sudo handles passwords through the controlling TTY, or root runs apt-get directly. Success restarts the TUI and refreshes outdated/security results; failure or cancellation shows a notice and leaves local inventory only. Offline inventory has unknown freshness.
- Explicit capability declarations.
- Adapter and tested manager version reporting.
- `PATH` executable discovery; no profile executable overrides.
- Disabled, optional, required, and unavailable manager states.
- Separate executable and global profile-config evidence in manager status.
- Missing optional manager with global profile config remains unavailable and reports both facts.
- Manifest evidence from mise config, Brewfile, package.json, and go.mod; explicit `enabled = true` also requires a manager.
- Homebrew refreshes metadata with `brew update` before `brew outdated --json=v2 --greedy`; refresh errors fail the outdated operation.

### Inspection

- Bounded concurrent reads with stable output.
- Partial-result preservation.
- Offline installed inventory.
- npm direct and transitive inventory.
- npm runtime, development, optional, peer, transitive, and extraneous classes.
- Canonical package identity as manager plus native package name.
- Exposed-command ownership model, separate from command-path resolution.
- Deterministic collision candidates for distinct global package owners.
- Command-path lookup limited to collision candidates.
- CLI warning format for competing global command owners.
- npm package `bin` parsing for string and object forms.
- Direct npm package command collection from scoped and unscoped manifests.
- Native global npm package-root discovery.
- Exposed commands included in normalized installed inventory results.
- Offline CLI human and JSON collision reporting.
- Package roles and owning manifest/config metadata.
- Snapshot metadata and package audit check exit codes.
- npm classes remain inventory metadata, not profile selection filters.

### Doctor

- Read-only manager executable and version integrity checks.
- Manager integrity remains separate from package freshness.
- Untested manager versions remain available without warnings or mutation blocks.
- `packages doctor` human and schema-versioned JSON output.
- npm v2/v3 lockfile root metadata checked against direct manifest dependencies.
- Unsupported old lockfiles report unknown rather than healthy.
- Selected project and workspace checks in human and JSON doctor output.

### Security

- Exact-version OSV checks.
- EUVD integration.
- EUVD product entries without an affected-version rule do not emit unknown or vulnerable findings.
- Explicit mappings for package identities that cannot be inferred safely.
- Security scans installed inventory in resolved scopes, with no profile selected/all security-scope policy.
- Unknown mapping remains unknown; apt has no Debian security identity mapping and is not reported clean.

### Updates

- Preview-first plans with request-local CLI include/exclude selectors, not profile package policy.
- Project mutation requires matching registered project `allow_manifest_changes = true`; Git overrides do not bypass permission.
- Plan confidence, impact, recovery commands, and state hashes.
- Whole-repository dirty and unversioned project guards; CLI overrides are explicit.
- Tested manager version guard.
- Interactive and accepted-hash unattended modes.
- Sequential fail-fast execution and best-effort manager isolation.
- Timeout and cancellation handling.
- Per-plan outcomes, changed files, post-update verification, and redacted journals.

### Frontends

- CLI supports manager status, doctor checks, outdated, offline inventory, vulnerability checks, and updates.
- TUI provides installed/latest/security status, profile and scope selection, package/vulnerability details, and command-collision summaries.
- `Enter` opens package/vulnerability details; dotfile link and environment details share the same styling: bold cyan headings, dim labels/paths, default-color ordinary values, yellow warnings, and red errors. CVSS uses blue for scores above 0 and below 4, yellow from 4 to below 9, and red at 9 or higher. Details support `↑`/`↓` or `k`/`j` scrolling, `Ctrl-u`/`Ctrl-d` paging up/down, and `Esc` back to the originating list; resizing rewraps text and preserves scroll position within content bounds.
- TUI `p` selects a session profile; `r` reloads central package config and refreshes. Switching does not write config; reload errors or removed selected profiles block upgrades.
- TUI previews and applies marked/all-package updates, rechecks manifest permission and Git safety, runs terminal commands sequentially, retains failures, and restarts to refresh results.
- TUI does not use CLI accepted hashes, mutation journals, post-update verification, or manager-version override guards.
- TUI concurrent inventory and outdated queries use isolated required-manager options.
- TUI keeps inspection failure guidance after vulnerability scans, reports vulnerability scan failure, hides optional unavailable managers, and replaces raw multiline inspection errors with doctor guidance.
- TUI primary status hides unmapped security count. CLI and report data retain it.
- TUI vulnerability rows hide advisoryless diagnostics and unknown findings without unsupported EUVD rules. Primary status counts only visible rule-backed unknown findings.
- CLI and TUI summaries report disabled detected managers.

## Verification state

Repository verification commands (not a claim that they passed for the current working tree):

```sh
go test -race ./... --cover
golangci-lint run
go build ./cmd/cli ./cmd/tui
git diff --check
```

Tests use fake manager executables and temporary manifests. No live manager is required.

## Known compatibility labels

`global` remains current public scope label. Internal validation also accepts `system`, `user`, and `global-config`, but adapters currently treat them through global capability paths. Do not claim distinct ownership semantics until adapters implement them.