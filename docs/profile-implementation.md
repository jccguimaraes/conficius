# Package profile implementation

- [x] Add independent central package `config.toml` with strict schema version 2; keep dotfile configuration separate.
- [x] Add XDG/OS-native package layout discovery and preview/apply `config layout`, rejecting conflicting configs.
- [x] Retain dry-run legacy project `config migrate`; `--apply` writes atomically beside a collision-safe timestamped backup. Central package loading itself requires version 2.
- [x] Add profile selection and `config resolve`, including verbose source provenance.
- [x] Add explicit global and canonical project scopes, registered projects, current-project detection, and npm workspaces.
- [x] Add five-minute subprocess timeouts and fixed bounded concurrency with deterministic merging.
- [x] Keep native manifests/lockfiles as dependency intent and version source; profiles contain project groups and manager `enabled` settings, not package/version policy. JSON package output uses schema version 2.
- [x] Add multi-project OSV checks with request deduplication over resolved installed inventory, without profile security-scope policy.
- [x] Remove profile manager tuning, ordering, executable overrides, npm class filters, and include/exclude or allow/deny package policy. CLI update selectors remain request-local.
- [x] Refresh Homebrew metadata with `brew update` before greedy outdated inspection.
- [x] Add guarded scoped updates requiring project `allow_manifest_changes = true` (`AllowManifestChanges`), plus whole-repository dirty and unversioned checks; CLI revalidation and redacted XDG mutation logs remain.
- [x] Add separate `cmd/tui` with installed/latest/security views, `p` session profile switching, `r` config reload, and update preview/apply.
- [ ] Align TUI mutation flow with CLI re-query, accepted hashes, manager-version guards, journals, and post-update verification.
- [ ] Add native Windows mise/npm adapters before profiles are described as cross-platform.

Each slice needs deterministic integration tests using fake manager executables. Live manager checks remain optional smoke tests, not CI gates.
