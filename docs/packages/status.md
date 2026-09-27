# Package subsystem status

## Implemented

### Profiles and scopes

- TOML schema version 2 with strict validation and version 1 migration.
- CLI, environment, project config, selected profile, and default resolution.
- Global, project, and npm workspace inspection.
- Absolute scope paths, symlink-aware deduplication, and workspace precedence.
- Manager and package allowlist/denylist policy.
- Exact desired-version drift.
- npm dependency-class policy.

### Managers

- Built-in mise, Homebrew, and npm adapters.
- Explicit capability declarations.
- Adapter and tested manager version reporting.
- Configured executable overrides.
- Disabled, optional, required, and unavailable manager states.
- Separate executable and global profile-config evidence in manager status.
- Missing optional manager with global profile config remains unavailable and reports both facts.
- Manifest evidence from mise config, Brewfile, and package.json.

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
- Package roles and owning manifest/config metadata.
- Snapshot metadata and package audit check exit codes.
- Active npm dependency-class filters in CLI JSON and human summaries.

### Security

- Exact-version OSV checks.
- EUVD integration.
- Explicit mappings for package identities that cannot be inferred safely.
- Configurable selected/all security scope.
- Unknown mapping remains unknown.

### Updates

- Preview-first plans with include/exclude selectors.
- Plan confidence, impact, recovery commands, and state hashes.
- Dirty and unversioned project guards.
- Tested manager version guard.
- Interactive and accepted-hash unattended modes.
- Sequential fail-fast execution and best-effort manager isolation.
- Timeout and cancellation handling.
- Per-plan outcomes, changed files, post-update verification, and redacted journals.

### Frontends

- CLI supports manager status, outdated, offline inventory, vulnerability checks, and updates.
- TUI provides read-only profile, scope, outdated, drift, and vulnerability views.
- CLI and TUI summaries report disabled detected managers.

## Verification state

Latest package slices passed:

```sh
go test -race ./... --cover
golangci-lint run
go build ./cmd/cli ./cmd/tui
git diff --check
```

Tests use fake manager executables and temporary manifests. No live manager is required.

## Known compatibility labels

`global` remains current public scope label. Internal validation also accepts `system`, `user`, and `global-config`, but adapters currently treat them through global capability paths. Do not claim distinct ownership semantics until adapters implement them.