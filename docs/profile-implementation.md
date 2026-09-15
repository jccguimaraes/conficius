# Package profile implementation

- [x] Add strict config schema version 2 while retaining version 1 reads until next major CLI release.
- [x] Add dry-run `config migrate`; `--apply` writes atomically beside a collision-safe timestamped backup.
- [x] Add profile selection and `config resolve`, including verbose source provenance.
- [ ] Add explicit global and canonical project scopes, registered projects, current-project detection, and npm workspaces.
- [ ] Add bounded concurrent read-only checks with deterministic output and five-minute subprocess timeouts.
- [ ] Add scoped inventory, outdated checks, exact desired-version drift, and JSON schema version 2.
- [ ] Add multi-project OSV checks with request deduplication and configurable security scope.
- [ ] Add typed mise bump, Homebrew greedy, and npm global-prefix policies.
- [ ] Add guarded scoped updates, dirty and unversioned project checks, revalidation, and redacted XDG mutation logs.
- [ ] Add separate `cmd/tui` after profile, drift, and vulnerability operations stabilize.
- [ ] Add native Windows mise/npm adapters before profiles are described as cross-platform.

Each slice needs deterministic integration tests using fake manager executables. Live manager checks remain optional smoke tests, not CI gates.
