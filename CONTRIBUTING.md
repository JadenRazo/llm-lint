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

`npm/scripts/check-npm-auth.mjs` checks auth configuration before release side
effects. It is a preflight, not proof that npm will accept a publish. In OIDC
mode it checks local prerequisites; it cannot verify npm's package-specific
publisher registrations, permissions or activation status. Token identity checks
also do not prove package write access. A dry run does not exercise the OIDC
publish exchange or activate a publisher.

1. **Trusted publishing (OIDC)** — preferred. Requires Node.js ≥ 22.14.0,
   npm ≥ 11.5.1, GitHub-hosted runners and `permissions: id-token: write`. npm exchanges the
   GitHub identity for short-lived credentials during an actual publish. Each
   package needs a separate registration for each workflow that publishes it.
2. **`NPM_TOKEN` secret** — temporary token compatibility. A nonempty secret
   selects token mode in `release.yml` and `release-health.yml`; recovery uses
   the explicit `auth` input (`oidc` by default, or `token`). A stale secret can
   therefore keep release and health checks in token mode even after publishers
   are registered. Token expiry, 2FA policy and package permissions can still
   prevent publication.

When tokenless, `release-health.yml` removes only setup-node's exact literal
`//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}` from the exported
`NPM_CONFIG_USERCONFIG`. It preserves registry settings and leaves other auth
entries for the validator to inspect. It does not fall back to a HOME config;
missing paths or files remain the validator's responsibility. `release.yml` and
`publish-npm.yml` retain their broader `_authToken` cleanup and HOME fallback.
Token mode retains the existing auth configuration.

#### Trusted publisher checklist

On npmjs.com, open each package's **Settings → Trusted publishing**. Configure
GitHub Actions with owner `JadenRazo`, repository `llm-lint`, and the exact
workflow filename, including `.yml` (not the full path). npm supports multiple
publishers per package, so retain `publish-npm.yml` and add `release.yml`.
Do not register `release-health.yml`: it never publishes.

Track both registrations for all seven packages; a checked cell means the
registration's fields, allowed actions and activation status have been reviewed:

| Package | `release.yml` | `publish-npm.yml` |
|---|---|---|
| `@jadenrazo/llm-lint` | [ ] | [ ] |
| `@jadenrazo/llm-lint-linux-x64` | [ ] | [ ] |
| `@jadenrazo/llm-lint-linux-arm64` | [ ] | [ ] |
| `@jadenrazo/llm-lint-darwin-x64` | [ ] | [ ] |
| `@jadenrazo/llm-lint-darwin-arm64` | [ ] | [ ] |
| `@jadenrazo/llm-lint-win32-x64` | [ ] | [ ] |
| `@jadenrazo/llm-lint-win32-arm64` | [ ] | [ ] |

For every registration, enable direct `npm publish`; stage-only permission does
not cover these workflows. Check the environment name against the executing
job: these jobs declare no GitHub environment, so leave it blank. If an environment
is added later, the registration must match it exactly. Verify each registration
is active, or newly created and still within its activation window; saving the
form does not verify that it works.

Create or recreate registrations near the authorized release. Each new publisher
must complete its first successful publish within **2 days** or it expires and
must be deleted and recreated. An already-published version skipped by recovery
does not activate a registration. Coordinate a real, authorized new-version
publish for each workflow's registrations; one workflow's success does not
validate the other workflow's registrations.

Migration order: adopt the auth fixes on `main`, verify the package checklist,
then remove the stale GitHub `NPM_TOKEN` secret so release and health select OIDC.
Complete the separately authorized new-version publish and confirm successful
OIDC publication for all seven packages. Keep account 2FA enabled throughout.
Only after OIDC is confirmed, set each package's Publishing access to **Require
two-factor authentication and disallow tokens**, then revoke obsolete publish
tokens. These are maintainer actions, not effects of running the preflight.
See npm's [trusted publishing guidance](https://docs.npmjs.com/trusted-publishers/)
for current registration and activation rules.

### If the npm half fails

Do not move the tag and do not re-run goreleaser — the GitHub release, the
container image and the sigstore transparency-log entries already exist and
cannot be withdrawn. Replay only the npm step, from the archives attached to the
release. Run recovery from the patched `main` workflow for older tags:

```sh
gh workflow run publish-npm.yml --ref main -f tag=v1.2.3 -f auth=oidc
```

`--ref main` selects the executing workflow revision. Its immutable
`github.workflow_sha` supplies the workflow policy check and the maintained
install verifier (`verify-published.mjs` and its `platforms.mjs` dependency).
The verifier modules are preserved before checking out the release tag. The `tag` input selects
the released archives and package source, including the npm shim and README.
All other packaging and auth scripts, including `check-npm-auth.mjs`,
`build.mjs` and `publish.mjs`, come from that selected tag. Running recovery from
`main` does not update those historical scripts. The provenance workflow ref
records `main` in this case. Use a tag as the workflow ref only when that tag
already contains the required workflow and verifier fixes; re-running a
historical workflow does not pick up changes adopted on `main`.

Every archive is checksum-verified against the release's own `checksums.txt`
before it is repackaged, already-published versions are skipped, and the run
finishes by installing the package from the public registry and running it.

### What guards this

| Check | Where | Catches |
|---|---|---|
| `check-manifests.mjs` | `ci.yml` | the platform matrix, `optionalDependencies`, the bin dispatch table and goreleaser's build matrix disagreeing; a bot raising the published `engines.node` floor |
| `smoke-build.mjs` | `ci.yml` | `build.mjs` breaking, discovered on a PR instead of mid-release |
| `check-npm-auth.mjs` | `release.yml`, `publish-npm.yml`, `release-health.yml` | invalid auth configuration before release side effects; does not prove package publish access or activate trusted publishers |
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
