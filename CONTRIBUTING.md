# Contributing to llm-lint

Thanks for helping catch LLM artifacts before they ship.

## Development setup

- Go **1.26+** (CI pins the exact toolchain in `.github/workflows/ci.yml`)
- `make` and `git`
- Optional: [golangci-lint](https://golangci-lint.run/) v2.x for `make lint`,
  Node.js 24 for the npm packaging targets

```bash
git clone https://github.com/JadenRazo/llm-lint
cd llm-lint
make build          # bin/llm-lint with version/commit/date ldflags
make test           # go test -race with coverage profile
make lint           # golangci-lint run (requires golangci-lint installed)
make fmt            # gofmt -s -w . && go mod tidy
```

Other useful targets: `make cover` (HTML coverage report), `make run`
(build + self-scan), `make docker`, `make npm-build` / `make npm-test`
(npm shim packaging), `make clean`.

## Tests and linting

Every PR must be green on:

```bash
gofmt -l cmd internal   # no output
go build ./...
go vet ./...
go test ./...
```

CI additionally runs `go test -race` on ubuntu, windows, and macos, a
coverage gate (ubuntu), golangci-lint, govulncheck, and a dogfood self-scan.

Golden-file tests live under `cmd/llm-lint/testdata/` and
`internal/report/testdata/`. After an intentional output change, regenerate
with:

```bash
go test ./... -update
```

and review the golden diffs as carefully as code.

## Adding a rule

1. Pick the next free `LLM###` ID (`llm-lint rules` shows the current set).
2. Rules are plain structs — see `Rule` in `internal/rules/rule.go` for the
   fields (ID, Title, Severity, Category, Kind, Description, Remediation,
   optional AutoFix, and the per-kind pattern lists).
3. Register the rule in the matching file under `internal/rules/builtin/`
   (`path_rules.go`, `content_rules.go`, `git_trailer_rules.go`) — each file's
   `init()` calls `rules.Register`, and duplicate IDs panic at startup.
4. Write `Description` and `Remediation` strings; remediation must be
   concrete and actionable (commands, file paths, settings keys).
5. Add a fixture under `testdata/` and a table-driven test in the matching
   `_test.go`.
6. `go test ./...` must pass before opening a PR.

### Rule-ID stability policy

Rule IDs are a public contract: they appear in configs (`rules:`,
`--include`/`--exclude`), baselines, and SARIF uploads.

- IDs are **never reused or renumbered**.
- A removed rule's ID is **tombstoned**: it stays reserved forever and is
  never assigned to a different check.
- Changing what an existing rule matches is allowed; changing its meaning
  entirely requires a new ID.

## Architecture

```
cmd/llm-lint/  →  internal/engine/  →  internal/scanner/    (filesystem walk + path/content rules)
                                  ↳   internal/gitscan/     (commit history + trailer/message rules)
                                  ↳   internal/report/      (human / json / sarif / github)
internal/rules/     — rule definitions, registered via init()
internal/config/    — .llmlint.yaml schema + override resolution
internal/baseline/  — accepted-findings snapshots + stable fingerprints
internal/fixer/     — --fix / --fix-preview (files, .gitignore, index, commit messages)
internal/hook/      — pre-commit hook install (native + pre-commit framework)
internal/progress/  — transient TTY progress line
```

## Commits and PRs

- Conventional-commit style subjects (`fix:`, `feat:`, `perf:`, `refactor:`,
  `security:`, `chore:`) — the release changelog is generated from them.
- Keep PRs focused; include verification output (build/vet/test) in the PR
  description.
