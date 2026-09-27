# Handoff

## State

Package audit and update work is active on `main`. Latest completed code commit:

`af7eb77 feat(packages): show TUI collisions`

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

Add read-only `packages doctor` CLI output for manager integrity. Keep change small.

## Working tree guard

Working tree was clean after `af7eb77`.