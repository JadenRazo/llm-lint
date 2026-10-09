# llm-lint

Build a configurable repository-policy scanner with useful findings and predictable
CLI output. A finding is a policy signal, not proof of authorship. Follow the
repository's configured rules; do not export this project's policy to other repos.

## Task map

| Change | Source and verification |
| --- | --- |
| Rules or false positives | `internal/rules/builtin/`, `internal/config/`, `testdata/`, matching unit tests |
| Filesystem, index or history scanning | `internal/scanner/`, `internal/gitscan/`, boundary/symlink/concurrency tests |
| CLI, baseline or reports | `cmd/llm-lint/`, `internal/baseline/`, `internal/report/`, CLI/report golden files |
| Fixes and hooks | `internal/fixer/`, `internal/hook/`, `cmd/llm-lint/hook.go` |
| Packaging and releases | `npm/scripts/`, `npm/package.json`, `.goreleaser.yaml`, `.github/workflows/` |

Preserve configurable severity, exclusions, baseline matching and stale-entry
handling. Rule IDs are stable: never reuse or renumber them; see `CONTRIBUTING.md`.
Keep JSON/SARIF shapes and exit codes compatible: 0 success, 1 findings at the
threshold, 2 internal/configuration error, 3 failing stale baseline. Test clean
and triggering cases together; intentional output changes require reviewed
golden diffs, not automatic acceptance of snapshots.

`scan --fix-preview` is the non-writing preview. `--fix` can change files,
`.gitignore`, the index and commit messages; history mode defaults to `latest`
and `scanned` broadens it. Do not run a fix, install a hook, rewrite history or
post `--pr-comment` output without authorization covering that action. Preserve
required attribution, licensing, co-author credit and disclosures as the README
requires; configure marker rules instead of removing required notices.

## Verification and delivery

Use `go.mod` for Go (currently 1.26.6); npm consumers have a separate Node floor
in `npm/package.json`. With existing tools/dependencies, choose focused checks:

```sh
go test ./internal/rules/... ./internal/config/...
go test ./internal/scanner/... ./internal/gitscan/...
go test ./cmd/llm-lint/... ./internal/report/... ./internal/baseline/...
go test ./internal/fixer/... ./internal/hook/...
go vet ./...
make test
node npm/scripts/check-manifests.mjs
bash scripts/check-workflow-pins.test.sh
```

For code delivery, match the affected CI checks, including platform/output and
packaging checks where relevant. Docs-only edits need source/diff validation.
Do not use `make fmt` as a read-only check: it also runs `go mod tidy`.

PR/main workflows run checks and upload coverage/security results; dependency
review may comment on failed PRs, and main also publishes Scorecard results.
Pushing `v*` tags triggers GitHub/GHCR/npm release publication and signing.
Manual npm recovery publishes from checksum-verified release archives; do not
move a released tag to repair npm delivery. Scheduled release health can write
issues. These effects are separate from local checks.

Docs explain behavior and limits first. Follow the PR template and include the
problem, result and checks actually run. Use the conventional commit subjects in
`CONTRIBUTING.md`; report local changes, release uploads and install verification
as distinct outcomes.
