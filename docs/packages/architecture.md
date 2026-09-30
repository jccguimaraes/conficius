# Package subsystem architecture

## Module map

| Path | Responsibility |
|---|---|
| `internal/config` | Strict TOML schema, profiles, scope discovery, npm workspaces, package policy, drift |
| `internal/packagemanager/adapter.go` | Adapter contract, capabilities, tested versions, impact metadata |
| `internal/packagemanager/{mise,homebrew,npm}.go` | Native commands, structured parsing, manager-specific planning |
| `internal/packagemanager/packagemanager.go` | Discovery, scoped outdated queries, normalized records, plans, execution, verification |
| `internal/packagemanager/installed.go` | Installed inventory, Go import records, package roles, npm graph/classes, ownership metadata |
| `internal/packagemanager/concurrent.go` | Bounded read concurrency and stable merge |
| `internal/packagemanager/errors.go` | Operation error categories and redaction boundaries |
| `internal/security` | Inventory-to-evidence mapping and vulnerability report assembly |
| `internal/vulnerability`, `internal/euvd` | External vulnerability clients and parsers |
| `cmd/cli/packages.go` | Package command wiring, output, profile filtering, mutation guards |
| `cmd/cli/update_log.go` | Redacted XDG mutation journal |
| `cmd/tui` | Read-only presentation over shared package/config APIs |

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
  -> apply profile/package/dependency-class policy
  -> render human or JSON output
```

Manifest discovery happens before manager commands. Contradictions, missing required managers, excluded required managers, and unreadable manifest evidence fail explicitly.

## Inventory and outdated models

Inventory uses `InstalledPackage` plus normalized exposed commands. It contains exact installed version, role, ownership, npm class, and transitive flag. `npm root --global` locates global package manifests. Direct npm package manifests provide exposed-command metadata. Missing manifests do not fail inventory.

Go-installed binaries do not retain canonical module and version ownership. `GoToolImport` validates journaled module, semantic version, and command names. `packages go-tools import MODULE VERSION COMMAND...` writes schema-versioned XDG state records. Go global inventory and exposed-command ownership read this journal only. They do not scan binaries or infer ownership.

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
7. Apply configured class filter unless CLI explicitly requests full transitive graph.

Outdated output lacks class data. Core joins package names against owning manifest before filtering.

## Plans and execution

`PlanUpdates` converts filtered records into manager/scope groups. Each plan stores immutable command data and state hash. Native dry-run confidence can be exact; derived plans are estimated; unknown plans require override before bulk mutation.

Execution:

1. Re-query state.
2. Rebuild plans.
3. Check accepted hashes for unattended mode.
4. Validate manager versions and project files.
5. Write initial journal.
6. Run plans sequentially.
7. Stop on failure unless best effort permits independent continuation.
8. Query state again and verify outcomes.
9. Finish journal with outcomes and changed files.

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