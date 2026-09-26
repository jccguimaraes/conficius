# Package subsystem roadmap

Work in small tested commits. Finish one item before starting next.

## Next

1. **Add ownership and collision reporting.**
   - Model canonical package identity and exposed commands.
   - Resolve commands only for collision candidates.
   - Warn for competing global owners.
   - Never classify valid local/global copies as duplicates.

2. **Add doctor checks.**
   - Verify manager environment integrity separately from freshness.
   - Detect safe lockfile/manifest inconsistencies.
   - Keep doctor read-only.

## Adapter expansion

Original target managers still missing:

- Mac App Store (`mas`)
- `uv`
- Poetry
- `pipx`
- Cargo
- Go tool installs and journaled imports
- pnpm
- Yarn
- Bun

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
- Linux system manager adapters such as apt, dnf, and pacman.
- Distinct `system`, `user`, and `global-config` ownership semantics.

## Product gaps

- Minimal top-level `audit` command that combines outdated, drift, security, and collision findings.
- Reconcile current `packages` subcommands with intended command surface.
- Final product and executable naming checks.
- Release readiness, changelogs, and first tagged releases.
- External adapter protocol only after built-in boundary proves stable.

## Deferred by design

- Registry search.
- Broad PATH or package-directory scanning.
- Automatic rollback promises.
- General manager cache.
- External plugins before built-in adapter needs stabilize.