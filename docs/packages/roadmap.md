# Package subsystem roadmap

Work in small tested commits. Finish one item before starting next.

## Next

Choose one item from Mutation gaps, Platform gaps, or Product gaps. Adapter expansion baseline is complete.

## Adapter expansion

Completed adapters:

- Mac App Store (`mas`): global inventory, outdated checks, guarded updates, CLI preview, unsupported scope, and failure fixtures.
- `uv`: global tool inventory, outdated checks, selected-tool updates, and failure fixtures.
- `pipx`: global inventory, outdated checks, selected-package updates, and failure fixtures.
- Poetry: project inventory, outdated checks, selected-package updates, and failure fixtures.
- pnpm: project inventory, outdated checks, selected-package updates, and failure fixtures.
- Cargo: global installed-crate and project lockfile inventory, crates.io checks/updates, and failure fixtures; no plugin. Global checks use `cargo info`’s compiler-compatible selected version, not its `latest` annotation. `.crates.toml` is authoritative for owned binaries, reconciled with `.crates2.json` install settings. No actual installs is an empty success; nonempty installs with missing/incomplete options fail closed. Versioned `cargo install` preserves root, owned binaries, features, default-feature setting, profile, and target. Registry/source overrides fail closed; Git, path, and alternate-registry installs are not update candidates. Project checks and updates require standalone projects with both `Cargo.toml` and `Cargo.lock`; workspace and path-dependency projects are rejected. Checks probe `cargo update name@current --dry-run`; each update uses `cargo update name@current --precise target`, binding the native dry-run’s range-compatible target. Duplicate consolidation reported only as `Removing` is skipped when no target can be established. Preflight rejects stale project manifests, lockfiles, Cargo configuration/environment, and changed global install settings or binary ownership. Project updates leave `Cargo.toml` unchanged but may change `Cargo.lock` and transitive dependencies; `allow_manifest_changes = true` and Git guards still apply. Inspection may access the network and write index/cache metadata; dry runs do not write the lockfile. Text parsing targets Cargo 1.99 and fails closed on unfamiliar output.
- Yarn: project inventory, latest-tag checks, selected-package upgrades for non-workspace projects, and failure fixtures.
- Bun: project inventory, actionable range-compatible outdated checks, selected-package updates, and failure fixtures.
- Go: journal-backed global tool inventory, command ownership, structured freshness, exact install updates, recovery, and failure fixtures; `go.mod` project/workspace inventory and outdated checks, native OSV identities including versioned replacements, offline cache-miss errors, and independent discovery alongside mise. Pinned `go get` project updates use manifest/Git guards, hash `go.mod` and `go.sum`, and leave the global journal unchanged; no automatic project rollback. Local replacements remain a security coverage gap.
- apt: global inventory via `dpkg-query` and cached-list outdated checks via `apt-get --simulate`; no project operations or automated updates. No automatic list refresh or privilege escalation during inspection unless explicitly configured via `run_as`; users explicitly run `sudo apt-get update` separately, or on Linux without APT `run_as`, press `c` (Update package cache, currently APT) on TUI package scopes/results to preview the cache-refresh command (`y` approves; `n`/Esc cancels). No shell; sudo handles passwords through the controlling TTY, or root runs apt-get directly. Success restarts the TUI and refreshes outdated/security results; failure or cancellation shows a notice and leaves local inventory only. Offline freshness and Debian security mapping remain unknown.

Each adapter requires:

- stable canonical ID and aliases
- tested version range
- capability matrix
- inventory fixtures
- outdated fixtures
- update planning and unsupported outcomes
- auth, network, parser, and changed-output failure fixtures
- role, scope, and ownership mapping

Do not add several managers in one slice. Start with one manager whose structured output covers inventory and outdated checks.

## Mutation gaps

- Align TUI with CLI re-query/accepted-plan, mutation-journal, and post-update verification safeguards; TUI preview/apply already exists.
- Install command and planning.
- Remove command, dependency impact, and explicit unknown safety state.
- Constraint-preserving versus latest/major update modes.
- Prerelease channel entry.
- Parent package-manager-first upgrades with child-plan recomputation.
- Project-declared post-update test, lint, and typecheck commands with content-hash trust.
- Broader plan confidence from native dry-run support.

## Platform gaps

- Native Windows command execution and path handling.
- Windows mise and npm adapter tests.
- Additional Linux system manager adapters such as dnf and pacman; apt project operations, automated updates, and Debian security identity mapping remain unsupported.
- Distinct `system`, `user`, and `global-config` ownership semantics.

## Product gaps

- Minimal top-level `audit` command that combines outdated, security, and collision findings.
- Reconcile current `packages` subcommands with intended command surface.
- Final product and executable naming checks.
- Release readiness, changelogs, and first tagged releases.
- External adapter protocol only after built-in boundary proves stable.

## Deferred by design

- Duplicating native package manifests or version intent in central configuration. Profiles remain project groups and manager enablement, with manifest-change permission for updates.

- Registry search.
- Broad PATH or package-directory scanning.
- Automatic rollback promises.
- General manager cache.
- External plugins before built-in adapter needs stabilize.