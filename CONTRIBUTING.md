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

## Releasing

Pushing a `v*` tag runs `release.yml`, which builds and signs the binaries,
publishes the GitHub release and the `ghcr.io` image, then publishes seven npm
packages: one per platform, plus the `@jadenrazo/llm-lint` parent that resolves
the right binary through `optionalDependencies`.

### Publish authentication

Two supported modes; `npm/scripts/check-npm-auth.mjs` detects which is in use and
fails at the *top* of the release job if it will not work.

1. **Trusted publishing (OIDC)** — preferred. Nothing expires. Configure it once
   per package on npmjs.com (*Settings → Trusted Publisher → GitHub Actions*,
   repository `JadenRazo/llm-lint`, workflow `release.yml`, and again for
   `publish-npm.yml`), then delete the `NPM_TOKEN` secret. No workflow change is
   needed: an absent secret selects this mode. Requires npm ≥ 11.5.1 and
   `permissions: id-token: write`, both already in place.
2. **`NPM_TOKEN` secret** — a granular access token with read+write on the
   `@jadenrazo` scope. These have a maximum lifetime, and an expired one is the
   reason v0.4.0 shipped to GitHub but never to npm: the registry answers an
   unauthenticated publish with a bare `404`, so the failure reads as "package not
   found" rather than "your credential died."

### If the npm half fails

Do not move the tag and do not re-run goreleaser — the GitHub release, the
container image and the sigstore transparency-log entries already exist and
cannot be withdrawn. Replay only the npm step, from the archives attached to the
release:

```sh
gh workflow run publish-npm.yml -f tag=v1.2.3
```

Every archive is checksum-verified against the release's own `checksums.txt`
before it is repackaged, already-published versions are skipped, and the run
finishes by installing the package from the public registry and running it.

### What guards this

| Check | Where | Catches |
|---|---|---|
| `check-manifests.mjs` | `ci.yml` | the platform matrix, `optionalDependencies`, the bin dispatch table and goreleaser's build matrix disagreeing; a bot raising the published `engines.node` floor |
| `smoke-build.mjs` | `ci.yml` | `build.mjs` breaking, discovered on a PR instead of mid-release |
| `check-npm-auth.mjs` | `release.yml`, `publish-npm.yml`, `release-health.yml` | a dead credential, before anything irreversible runs |
| `verify-published.mjs` | after every publish | a release that uploads cleanly but cannot actually be installed |
| `check-release-sync.mjs` | `release-health.yml`, weekly | GitHub advertising a release npm has never heard of |

`release-health.yml` keeps a single issue labelled `release-health` up to date and
closes it automatically once everything is green, rather than filing a new one
each week.

**`engines.node` in `npm/package.json` is a consumer support floor, not a build
toolchain pin.** Raising it drops every user below that version. Dependency bots
do not know the difference, so `check-manifests.mjs` fails the build if the floor
moves past Node 18; raising it deliberately means editing that constant in the
same commit and cutting a major version.
