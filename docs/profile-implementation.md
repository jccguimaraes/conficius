# Package profile implementation

- [x] Add strict config schema version 2 while retaining version 1 reads until next major CLI release.
- [x] Add dry-run `config migrate`; `--apply` writes atomically beside a collision-safe timestamped backup.
- [x] Add profile selection and `config resolve`, including verbose source provenance.
- [x] Add explicit global and canonical project scopes, registered projects, current-project detection, and npm workspaces.
- [x] Add five-minute subprocess timeouts and fixed bounded concurrency with deterministic merging.
- [x] Add exact desired-version drift and allowlist/denylist filtering to scoped outdated checks. JSON package output uses schema version 2.
- [x] Add multi-project OSV checks with request deduplication and configurable security scope.
- [x] Add typed mise bump, Homebrew greedy, and npm global-prefix policies.
- [x] Add guarded scoped updates, dirty and unversioned project checks, revalidation, and redacted XDG mutation logs.
- [x] Add separate preview-only `cmd/tui` after profile, drift, and vulnerability operations stabilize.
- [ ] Add native Windows mise/npm adapters before profiles are described as cross-platform.

Each slice needs deterministic integration tests using fake manager executables. Live manager checks remain optional smoke tests, not CI gates.
