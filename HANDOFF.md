# Handoff

## State

Package audit and update work is active on `main`. Latest completed code commit:

`e1ad16c test(packages): cover go update execution`

Package subsystem checks passed after that commit:

```sh
go test -race ./... --cover
golangci-lint run
go build ./cmd/cli ./cmd/tui
git diff --check
```

## Read by task

- Product behavior or safety rule: [`docs/packages/design.md`](docs/packages/design.md)
- Code boundary or data flow: [`docs/packages/architecture.md`](docs/packages/architecture.md)
- Implemented state: [`docs/packages/status.md`](docs/packages/status.md)
- Missing work and next slice: [`docs/packages/roadmap.md`](docs/packages/roadmap.md)
- Domain vocabulary: [`CONTEXT.md`](CONTEXT.md)

Do not load all package documents by default. Start with roadmap, then open one linked document needed for task.

## Next slice

Choose one roadmap item. Current working tree contains uncommitted TUI and security fixes:

- clone `QueryOptions.Required` before manifest checks so concurrent TUI inventory and outdated queries cannot write same map;
- retain vulnerability scan errors and inspection failure guidance in final TUI status;
- hide optional unavailable managers and replace raw multiline inspection errors with `conficius packages doctor` guidance;
- recognize `Cargo.toml` as project scope evidence;
- skip npm project inventory, freshness, and doctor checks unless that scope contains `package.json`;
- ignore EUVD product entries with no version rule. This removes false GoReleaser `EUVD-2024-0363` / `CVE-2024-23840` findings for current versions;
- hide unmapped security count from primary TUI status. CLI and report data retain it.

## Working tree guard

Do not discard current uncommitted package fixes.