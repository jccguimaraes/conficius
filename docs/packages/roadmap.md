# Package subsystem roadmap

Work in small tested commits. Finish one item before starting next.

## Next

Start adapter expansion with one manager whose structured output covers inventory and outdated checks.

## Adapter expansion

Completed adapters:

- Mac App Store (`mas`): global inventory, outdated checks, guarded updates, CLI preview, unsupported scope, and failure fixtures.
- `uv`: global tool inventory, outdated checks, selected-tool updates, and failure fixtures.
- `pipx`: global inventory, outdated checks, selected-package updates, and failure fixtures.
- Poetry: project inventory, outdated checks, selected-package updates, and failure fixtures.
- pnpm: project inventory, outdated checks, selected-package updates, and failure fixtures.
- Cargo: global inventory, unsupported outdated/update outcomes, and failure fixtures.
- Yarn: project inventory, unsupported outdated/update outcomes, and failure fixtures.
- Bun: project inventory, unsupported outdated/update outcomes, and failure fixtures.

Adapter in progress:

- Go tools: validated journaled import model exists. Add read-only journal storage and import command before adapter registration.

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