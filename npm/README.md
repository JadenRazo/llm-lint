# @jadenrazo/llm-lint

A scanner that catches **LLM-tooling artifacts** before they ship: `CLAUDE.md`, `.claude/`, `Co-authored-by: Claude` commit trailers, `.cursorrules`, GitHub Copilot config, AI refusal text leaked into source, and more.

Native Go binary, distributed via npm using the [esbuild-style optionalDependencies pattern](https://blog.vercel.com/posts/whats-new-in-turbo-1-7-0#new-rust-binary-distribution) — no postinstall scripts, no network at install time, no Node runtime dependency at run time.

## Quick start

```bash
# One-shot scan
npx @jadenrazo/llm-lint scan

# Or install globally
npm install -g @jadenrazo/llm-lint
llm-lint scan --fail-on error
```

## What it catches

| Rule | Severity | Detects |
|---|---|---|
| `LLM001` | error | `CLAUDE.md` committed at any depth |
| `LLM002` | error | `.claude/` directory tracked in git |
| `LLM003` | error | `Co-authored-by: Claude` commit trailer |
| `LLM004` | warning | `Generated with Claude` in commit messages |
| `LLM005` | warning | `CLAUDE_NOTES.md`, `.claude.local.md`, etc. |
| `LLM006` | error | `.cursorrules`, `.cursor/`, `.cursorignore` |
| `LLM007` | warning | `.github/copilot-instructions.md` |
| `LLM008` – `LLM015` | various | Aider, Continue, Codeium, Windsurf, MCP configs, refusal text, AI markers |

Run `llm-lint rules` for the full list, or `llm-lint rules show LLM003` for any rule's full description and remediation.

## Supported platforms

| Platform | Status |
|---|---|
| Linux x64 | ✓ |
| Linux arm64 | ✓ |
| macOS x64 (Intel) | ✓ |
| macOS arm64 (Apple Silicon) | ✓ |
| Windows x64 | ✓ |
| Windows arm64 | ✓ |

## Documentation

Full docs, configuration reference, CI integration, and contributing guide live in the [GitHub repository](https://github.com/JadenRazo/llm-lint).

## License

Apache-2.0
