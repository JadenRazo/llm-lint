# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-08-07

### Added

- SARIF output now includes stable `partialFingerprints` (`llmLint/v1`, the
  same sha256 identity the baseline uses), per-rule
  `properties.security-severity` (error 8.0 / warning 5.0 / info 3.0),
  per-rule `helpUri`, and `runAutomationDetails.id` (`llm-lint/scan`) so
  GitHub code scanning tracks findings across runs and buckets them by
  severity.
- `--version` flag on the root command; `llm-lint version` now prints
  `llm-lint <version> (commit <sha>, built <date>, <go version>)`, with
  `version --short` keeping the old bare-version output for scripts.
  Release builds inject `commit` and `date` via ldflags (Makefile and
  goreleaser).
- JSON report's `scanned` block gains `files_walked` and `bytes_read`, so a
  scan's cost (files visited, content read) is visible, not just what it kept.
- `baseline create/update/status/prune` show the live progress line and
  accept `--no-progress`; they also honor Ctrl-C cancellation.
- Distinct exit code `3` for stale-baseline failures (was overloaded onto `1`).
- CI: test matrix across ubuntu / windows / macos (`go test -race` everywhere,
  coverage gate on ubuntu), a `gofmt -l` check in the lint job, and extra
  golangci-lint linters (errorlint, noctx) plus gofmt/goimports formatters.

### Changed

- Shared helpers consolidated into `internal/textutil` (ShortSHA, Indent,
  IsTTY) and `internal/testutil` (Itoa, CompareGolden, InitRepo); `scan` and
  the `baseline` subcommands now share one flag-registration and
  engine-construction path (`cmd/llm-lint/scan_options.go`).

### Fixed

- Config loading no longer fails silently: strict YAML (unknown keys are
  errors), and rule IDs, categories, severities, `fail_on`, and ignore globs
  are validated at load time instead of quietly disabling rules or dropping
  findings.
- `--include`/`--exclude` rule IDs and `--fail-on` values are validated
  before scanning.
- Ignore matching uses slash-normalized `doublestar.Match`, fixing glob
  behavior on Windows.
- Command logic no longer calls `os.Exit` directly; exit codes flow through
  a typed error so deferred cleanup runs and paths are testable.

### Performance

- Content-rule regexes compile once per scan instead of per line; fixer
  pattern caches are package-level.
- `--fix-git-history scanned` loads commit metadata in a single `git log`
  pass instead of two subprocesses per commit.
- Ignored directories (e.g. `vendor/**`) are pruned from the walk instead of
  being filtered file-by-file after walking the whole subtree.
- Ctrl-C cancels in-flight walkers, git iteration, and git subprocesses via
  context propagation.

### Security

- Bumped `go-git` to v5.19.2, `golang.org/x/crypto` to v0.54.0, and
  `golang.org/x/net` to v0.57.0, clearing all 17 open Dependabot alerts
  (including 7 critical); `govulncheck` reports 0 reachable vulnerabilities.
- Dependency and CI-action refresh via Renovate (checkout v6, setup-go v6,
  setup-node v6, upload-artifact v7, codeql-action v4, docker actions v4,
  golangci-lint-action v9, goreleaser-action v7, cosign-installer v4,
  Node.js 24, go-sarif); workflows run with explicit least-privilege
  permissions and timeouts.
- **Release signing now emits a Sigstore bundle.** Releases ship
  `checksums.txt.bundle` (signature + certificate in one file) instead of the
  separate `checksums.txt.sig` and `checksums.txt.pem`. cosign v3 — pulled in
  by the `cosign-installer` v4 bump above — removed `--output-signature` and
  `--output-certificate`, leaving `--bundle` as the only way to emit
  verification material. Verify with
  `cosign verify-blob --bundle checksums.txt.bundle ... checksums.txt`; see
  [SECURITY.md](./SECURITY.md), which also documents verifying pre-v0.4.0
  releases that still use the two-file form.
