# Package profiles and shared core

Conficius will model package work as resolved profiles over explicit global and project scopes. Manager adapters own command execution and parsing; shared internal core owns config resolution, inventory, drift, vulnerability checks, update plans, and execution. CLI and separate `conficius-tui` frontends consume typed domain values and errors, preventing either frontend from defining package semantics.

Profiles use config schema version 2 with strict unknown-field validation. Resolution precedence is CLI, environment, project config, selected global profile, then defaults. Read-only commands may inspect current project plus global installations; mutation requires explicit scope and `--apply`. Exact desired versions report drift without promising reconciliation where manager lacks exact-version support.

Initial implementation supports macOS and Linux. Schema and path syntax remain portable to native Windows, whose adapters and terminal handling are deferred. CLI releases use SemVer tags shaped `cli-vX.Y.Z`; TUI releases use UTC calendar tags shaped `tui-vYYYY.MM.revision`. Each binary has separate release workflow and changelog.
