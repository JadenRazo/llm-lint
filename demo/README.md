# demo/ — launch material

Two scripts that produce shareable proof that the tool works.

## `record.sh` — terminal GIF for the README hero

Produces `demo/demo.gif` (~30 seconds, ~1-2 MB) showing a clean repo, a Claude artifact + trailer landing on it, and `llm-lint` flagging both with concrete remediation.

```sh
bash demo/record.sh
```

Runtime requirements:

- [`vhs`](https://github.com/charmbracelet/vhs) — `go install github.com/charmbracelet/vhs@latest`
- [`ttyd`](https://github.com/tsl0922/ttyd) — `apt install ttyd` / `brew install ttyd`
- `ffmpeg` — `apt install ffmpeg` / `brew install ffmpeg`

To tweak timing, theme, or scene order, edit [`demo.tape`](demo.tape) and re-run. See the [vhs reference](https://github.com/charmbracelet/vhs#vhs-command-reference) for syntax.

## `seed-demo-pr.sh` — GitHub PR with red CI checks

For screenshots of the CI gate firing in a real PR (Code Scanning tab, failing checks, inline annotations).

```sh
bash demo/seed-demo-pr.sh
```

Creates a local-only branch `demo/llm-lint-violations` with intentional violations (CLAUDE.md, .cursorrules, Claude trailer) and prints the `git push` + `gh pr create` commands to run when you're ready. The script never pushes on its own.

After capturing screenshots:

```sh
gh pr close --delete-branch <PR-number>
git checkout main
git branch -D demo/llm-lint-violations
```

## What to capture

For a launch post, three artifacts cover the surface area:

1. **`demo.gif`** in the README hero — shows scan output and remediation in the terminal.
2. **PR Checks tab screenshot** — the `dogfood` job failing red with annotations on the offending files.
3. **Code Scanning tab screenshot** — `llm-lint` SARIF results listed alongside CodeQL/Scorecard, with severity, rule descriptor, and remediation rendered by GitHub. This is the most credible-looking single image.

The SARIF upload step in `ci.yml`'s `dogfood` job runs `if: always()`, so findings show up in Code Scanning even when the gate fails — the screenshot opportunity exists on every demo PR.
