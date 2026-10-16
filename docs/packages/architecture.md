# Package subsystem architecture

## Module map

| Path | Responsibility |
|---|---|
| `internal/config` | Independent central package TOML, project-group profiles, scope discovery, npm workspaces |
| `internal/packagemanager/adapter.go` | Adapter contract, capabilities, tested versions, impact metadata |
| `internal/packagemanager/{mise,homebrew,npm,mas,uv,pipx,poetry,pnpm,cargo,yarn,bun,go,apt}.go` | Native commands, structured parsing, manager-specific planning |
| `internal/packagemanager/packagemanager.go` | Discovery, scoped outdated queries, normalized records, plans, execution, verification |
| `internal/packagemanager/installed.go` | Installed inventory, Go import records, package roles, npm graph/classes, ownership metadata |
| `internal/packagemanager/concurrent.go` | Bounded read concurrency and stable merge |
| `internal/packagemanager/operation_error.go`, `internal/packagemanager/redact.go` | Operation error categories and redaction boundaries |
| `internal/security` | Inventory-to-evidence mapping and vulnerability report assembly |
| `internal/vulnerability`, `internal/euvd` | External vulnerability clients and parsers |
| `cmd/cli/packages.go` | Package command wiring, output, scope selection, mutation guards |
| `cmd/cli/update_log.go` | Redacted XDG mutation journal |
| `cmd/tui` | Installed/latest/security views, profile switching, config reload, update preview and apply |

## Dependency direction

```text
cmd/cli ─┐
         ├─> internal/config
cmd/tui ─┤
         ├─> internal/packagemanager ─> internal/config
         └─> internal/security ─> internal/vulnerability + internal/euvd
```

Frontends do not define package semantics. They resolve user input, call shared modules, and render typed results.

## Adapter boundary

Built-in adapters implement one internal interface. Adapter owns:

- canonical manager name
- tested manager version range
- capability declaration
- inventory and outdated commands
- output parser
- package command metadata parsing
- update arguments
- project-owned files
- impact and recovery metadata

Core owns:

- scope normalization
- manager discovery status
- concurrency
- partial-result retention
- filtering and record ordering
- plan grouping and hashing
- mutation sequencing
- cancellation
- verification

Adding manager should change adapter registration and fixtures, not core behavior.

## Query flow

```text
CLI/TUI flags
  -> load and resolve profile
  -> resolve global/project/workspace scopes
  -> normalize and deduplicate scopes
  -> infer required managers from manifests
  -> inspect executables and versions
  -> check adapter capability per scope
  -> run read operations
  -> retain successes and classify failures
  -> normalize records and ownership
  -> render human or JSON output
```

Package configuration loads through `config.LoadPackages`, separately from dotfile configuration. Profiles contain named project paths and manager `enabled` settings only, plus per-project manifest-change permission. Native manifests and lockfiles own dependency intent and versions; central config does not duplicate package lists or version policy.

Working-directory scope discovery recognizes five project files: `package.json`, `Cargo.toml`, `go.mod`, `mise.toml`, and `.mise.toml`. Go module evidence is independent of mise runtime evidence, so combined roots can query both managers.

Manifest discovery happens before manager commands. Contradictions, missing required managers, disabled required managers, and unreadable manifest evidence fail explicitly.

## Inventory and outdated models

Inventory uses `InstalledPackage` plus normalized exposed commands. It contains exact installed version, role, ownership, npm class, and transitive flag. `npm root --global` locates global package manifests. Direct npm package manifests provide exposed-command metadata. Missing manifests do not fail inventory.

Go-installed binaries do not retain canonical module and version ownership. `GoToolImport` validates journaled module, semantic version, and command names. `packages go-tools import MODULE VERSION COMMAND...` writes schema-versioned XDG state records. Go global inventory and exposed-command ownership read this journal only. They do not scan binaries or infer ownership. Freshness uses `go list -m -json MODULE@latest` and accepts only matching module paths with semantic versions. Each Go update plan runs `go install MODULE@LATEST`; successful verification writes the new exact version to journal.

Go project/workspace inspection requires `go.mod`. Inventory runs `go list -m -json -mod=readonly all` in the scope directory with `GOPROXY=off`, `GONOPROXY=none`, `GOSUMDB=off`, and `GOTOOLCHAIN=local`; missing cached metadata fails the query. Outdated inspection runs `go list -m -json -mod=readonly -u all`, allowing network access and metadata-cache writes without writing `go.mod`. Native inventory excludes main and synthetic go/toolchain entries, records direct/indirect library dependencies, and supplies OSV ecosystem `Go` with actual module path/version, including versioned replacements. Local replacements have no published identity and are omitted, leaving a security coverage gap rather than a clean result. Default outdated candidates include only direct dependencies (`Indirect == false`) and modules providing `tool` packages declared in the scope's `go.mod`. The manifest is read without modification and parsed with `modfile.Parse`; read and parse failures are reported. Tool package paths map to the longest module-path prefix ending at a path boundary in the full module graph, including indirect modules. Vulnerability inventory retains the full graph, including indirect dependencies. All replaced modules are excluded from outdated candidates. Project updates are unsupported; global Go tool journal behavior is unchanged.

apt supports global inventory through `dpkg-query` and outdated inspection through `apt-get --simulate` against cached package lists only. Inspection never refreshes lists or escalates privileges; users can refresh separately with `sudo apt-get update` or on Linux, press `c` (Update package cache, currently APT) on TUI package scopes/results. The TUI previews the command; `y` approves, `n`/Esc cancels. It executes `sudo -- <apt-get> update --error-on=any` without a shell through the controlling TTY; sudo handles passwords, while root runs apt-get directly. Success restarts the TUI and refreshes outdated/security results; failure or cancellation shows a notice and leaves local inventory only. Offline inventory has unknown freshness. Project operations and updates are unsupported; Debian security identity mapping remains unknown.

Outdated query starts with manager-native `Tool` values. Core wraps tools in `scopedTool`, then `Records` creates normalized `Package` values. Scope, ownership, role, and npm class flow into plans.

Do not add frontend-only copies of these rules. Add normalized fields in shared types.

## npm classification

For project/workspace npm scopes:

1. Read `package.json`.
2. Build direct class map with deterministic precedence.
3. Parse npm inventory.
4. Mark direct package from map.
5. Mark unknown direct package extraneous.
6. Mark nested package transitive.
7. Include nested packages when the caller requests the transitive graph; profiles do not filter dependency classes.

Outdated output lacks class data. Core joins package names against the owning manifest for classification, not profile filtering.

## Plans and execution

`PlanUpdates` converts selected records into manager/executable/scope/directory groups, with separate Go tool plans. Each plan stores command data and a state hash. Current plans have estimated confidence; exact and unknown remain model values, not native dry-run guarantees.

CLI execution:

1. Re-query state.
2. Rebuild plans.
3. Validate accepted hashes for unattended mode (manager-version and project permission/Git guards run before re-query).
4. Keep the rebuilt plans for execution.
5. Write initial journal.
6. Run plans sequentially.
7. Stop on failure unless best effort permits independent continuation.
8. Query state again and verify outcomes.
9. Finish journal with outcomes and changed files.

Project plans require a matching registered project with `allow_manifest_changes = true` before Git safety checks. This is an update permission, not a package-selection policy.

TUI previews shared plans, rechecks project permissions/Git safety before apply, then runs commands through the controlling terminal and restarts with refreshed results. Its command loop continues after individual failures and retains failed-package selections. It does not use the CLI accepted-hash/journal/verification flow or CLI manager-version override guard.

## Error model

Operation errors preserve category:

- authentication
- network
- rate limit
- registry
- parser
- command
- timeout or cancellation through context

Errors are redacted before status, logs, or output. A failed operation does not erase successful operations.

## Tests

Adapter and core tests use temporary fake executables. Fixtures assert arguments, working directory, environment, structured output, partial failure, cancellation, and redaction.

Required repository checks:

```sh
go test -race ./... --cover
golangci-lint run
go build ./cmd/cli ./cmd/tui
git diff --check
```

Live package managers are smoke tests, not CI gates.