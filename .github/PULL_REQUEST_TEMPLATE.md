## Summary

<!-- What changes and why. Keep to 1-3 sentences focused on intent, not mechanics. -->

## How verified

<!-- Commands run locally. e.g.:
- make test
- make lint
- go run ./cmd/llm-lint scan --fail-on error
-->

## Risk and rollback

<!-- Blast radius and how to revert if this breaks something. -->

## Checklist

- [ ] Tests added or updated (table-driven, deterministic, no network)
- [ ] README rule table updated (only if a rule was added)
- [ ] `make lint` passes locally
- [ ] `go run ./cmd/llm-lint scan --fail-on error` passes (self-scan)
- [ ] For new rules: followed the canonical recipe in `CLAUDE.md` (positive + negative fixtures, concrete `Remediation`)
- [ ] No secrets, tokens, or local paths in the diff
