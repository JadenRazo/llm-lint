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

### `demo-short.gif` — LinkedIn / social cut (~7.6s, 152 frames)

Trimmed from `demo.gif` — keeps Scenes 1 and 2 (artifacts land, scan surfaces findings via `npx @jadenrazo/llm-lint scan`) and drops the `rules show` drill-down. Uses **non-uniform speedup** so the substance gets more screen time than the setup typing: setup region 6×, scan/findings region 1.4×. Fits LinkedIn's tightened GIF cap. Regenerate with:

```sh
ffmpeg -y -i demo/demo.gif -filter_complex "\
  [0:v]trim=0:9.0,setpts=(PTS-STARTPTS)/6[setup];\
  [0:v]trim=9.0:17.5,setpts=(PTS-STARTPTS)/1.4[scan];\
  [setup][scan]concat=n=2:v=1:a=0,fps=20,split[a][b];\
  [a]palettegen=stats_mode=diff[p];\
  [b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
  demo/demo-short.gif
```

Tuning knobs:

- `trim=0:9.0` is the boundary between "git commit done" and "`# No install needed:` comment starts" in `demo.gif`. `trim=…:17.5` is where the static findings frame ends and Scene 3 (`rules show`) begins. If you re-record the master, re-derive both with `ffmpeg -ss N -i demo/demo.gif -frames:v 1 frame.png` until the frame matches.
- `setpts=(PTS-STARTPTS)/6` on the setup trims it from 9.0s → 1.5s. Drop to `/5` if the setup-typing feels too aggressive.
- `setpts=(PTS-STARTPTS)/1.4` on the scan trims it from 8.5s → 6.1s, leaving the static "read findings" frame on screen for ~3.5s. `/1.2` would give ~4.5s of dwell but adds ~30 frames.

Note: the master `demo.tape` itself was tightened to make this possible — typing speed dropped to 30ms/char, the Scene 1 narration comment was removed entirely (the visual story carries it), and the Scene 2 comment was shortened to `# No install needed:`. Re-introducing narration in the tape would put this back over the LinkedIn cap.

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
