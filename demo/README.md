# demo/ - launch material

Short, shareable proof that the tool works.

## `record.sh` - terminal GIF/MP4 for the README hero

Produces `demo/demo.gif` and `demo/demo.mp4` (~12 seconds) showing a Claude trailer and tracked AI helper files, `--fix-preview` summarizing the safe changes it would make, `--fix` applying those changes, and a clean follow-up scan.

```sh
bash demo/record.sh
```

Runtime requirements:

- [`vhs`](https://github.com/charmbracelet/vhs) - `go install github.com/charmbracelet/vhs@latest`
- [`ttyd`](https://github.com/tsl0922/ttyd) - `apt install ttyd` / `brew install ttyd`
- `ffmpeg` - `apt install ffmpeg` / `brew install ffmpeg`

To tweak timing, theme, or scene order, edit [`demo.tape`](demo.tape) and re-run. See the [vhs reference](https://github.com/charmbracelet/vhs#vhs-command-reference) for syntax.

If `vhs`/`ttyd` are not available, regenerate the checked-in assets with the deterministic renderer:

```sh
python demo/render-autofix-demo.py
```

## `demo.mp4` - social/video cut

The MP4 is generated from the same short terminal sequence as the README GIF, with fast-start metadata and `yuv420p` pixels for broad preview/upload compatibility.

## `seed-demo-pr.sh` - GitHub PR with red CI checks

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

1. **`demo.gif`** in the README hero - shows preview, apply, and a clean scan in the terminal.
2. **PR Checks tab screenshot** - the `dogfood` job failing red with annotations on the offending files.
3. **Code Scanning tab screenshot** - `llm-lint` SARIF results listed alongside CodeQL/Scorecard, with severity, rule descriptor, and remediation rendered by GitHub.

The SARIF upload step in `ci.yml`'s `dogfood` job runs `if: always()`, so findings show up in Code Scanning even when the gate fails.
