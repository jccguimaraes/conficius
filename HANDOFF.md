# Handoff

## State

Package work is complete through current signed commit:

`4d72a3f fix(tui): count actionable unknown findings`

Working tree was clean after this commit.

Required checks passed:

```sh
go test -race ./... --cover
golangci-lint run
go build ./cmd/cli ./cmd/tui
git diff --check
```

Tests use fake executables and temporary manifests. Do not use live package-manager mutations in tests.

## Read by task

- Product behavior and safety: [`docs/packages/design.md`](docs/packages/design.md)
- Code boundaries: [`docs/packages/architecture.md`](docs/packages/architecture.md)
- Implemented state: [`docs/packages/status.md`](docs/packages/status.md)
- Deferred work: [`docs/packages/roadmap.md`](docs/packages/roadmap.md)
- Domain terms: [`CONTEXT.md`](CONTEXT.md)

Start with roadmap. Read only linked document needed for current slice.

## Recent signed commits

```text
4d72a3f fix(tui): count actionable unknown findings
aa491aa fix(tui): hide unactionable vulnerability rows
fd40cb9 feat(security): scan Cargo crates with OSV
cf82be3 feat(packages): inventory Cargo lockfiles
9a334a9 fix(tui): hide unmapped security count
7cd857b fix(doctor): skip non-npm projects
1de8315 fix(packages): gate npm project checks
8f68a2b fix(tui): retain inspection failures
5ed8beb fix(packages): stabilize TUI inspection
e1ad16c test(packages): cover go update execution
839483a docs(packages): complete go adapter
```

## Current package behavior

### TUI

- Concurrent inventory and freshness queries clone `QueryOptions.Required`. No shared map write.
- TUI keeps package-inspection failure guidance after security scan completes.
- Optional unavailable managers do not fill status bar.
- Raw multiline manager errors become `conficius packages doctor` guidance.
- Primary status hides unmapped security count.
- Vulnerability list hides clean, advisoryless, and ruleless unknown findings.
- Primary unknown count includes only visible unknown findings with advisory and unsupported EUVD rule.

### Rust and Cargo

- `Cargo.toml` creates current-project scope alongside global scope.
- npm project inventory, freshness, and doctor checks run only when `package.json` exists.
- npm project inventory uses `npm ls --package-lock-only --json`, so absent `node_modules` does not cause `ELSPROBLEMS`. Global inventory still reads installed packages.
- Cargo project inventory reads registry-sourced dependencies from `Cargo.lock`.
- Cargo root package and path packages are excluded from project inventory.
- Cargo crates have native OSV identity: `crates.io` plus crate name.
- Cargo outdated and update remain unsupported.

### EUVD

- EUVD product entries without `product_version` do not emit unknown or vulnerable findings.
- This removes false GoReleaser `EUVD-2024-0363` / `CVE-2024-23840` results for current releases.

### Go tools

- Go installed-binary ownership comes only from XDG journal records.
- Import command:

  ```sh
  conficius packages go-tools import MODULE VERSION COMMAND...
  ```

- Manager inspection uses `go version`; generic `--version` is invalid for Go.
- Freshness uses `go list -m -json MODULE@latest`.
- One update plan per module runs `go install MODULE@VERSION`.
- Successful verification rewrites journal version.

## Decisions

- No top-level combined `audit` command. Keep commands separate to avoid duplicate network scans:

  ```sh
  conficius packages outdated
  conficius packages vulnerabilities
  conficius packages doctor
  ```

- Unknown security state is not healthy.
- Doctor remains read-only.
- Use one small tested slice per signed commit.
- Update this file, `docs/packages/status.md`, and `docs/packages/roadmap.md` before each package commit.

## Next work

Pick one roadmap item. Best candidates:

1. Cargo freshness only after verified stable structured Cargo output. Otherwise keep unsupported.
2. Add safe OSV identities for other native package inventories.
3. Package mutation gaps: install/remove planning, dependency impact, update modes.
4. Platform gaps: Windows support and Linux system managers.
5. Product and release readiness.

Do not start several adapter expansions in one slice.
