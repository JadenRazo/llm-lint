package builtin

import "github.com/JadenRazo/llm-lint/internal/rules"

func init() {
	rules.Register(rules.Rule{
		ID:       "LLM001",
		Title:    "CLAUDE.md committed",
		Severity: rules.SevError,
		Category: rules.CatClaude,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			"CLAUDE.md",
			"**/CLAUDE.md",
			"Claude.md",
			"**/Claude.md",
			"claude.md",
			"**/claude.md",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{"CLAUDE.md"}},
		Description: "CLAUDE.md is read by Claude Code as repo-specific context. It should not ship to production codebases.",
		Remediation: `Add CLAUDE.md to .gitignore, then untrack it:

    echo 'CLAUDE.md' >> .gitignore
    git rm --cached CLAUDE.md
    git commit -m "chore: untrack CLAUDE.md"

If you want repo guidance for Claude Code, keep a local-only file at
.claude/CLAUDE.md and add .claude/ to .gitignore.`,
	})

	rules.Register(rules.Rule{
		ID:       "LLM002",
		Title:    ".claude/ directory tracked",
		Severity: rules.SevError,
		Category: rules.CatClaude,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".claude/**",
			"**/.claude/**",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".claude/"}},
		Description: "The .claude/ directory contains local Claude Code state, settings, and slash commands; it should not be committed.",
		Remediation: `Add .claude/ to .gitignore and untrack:

    echo '.claude/' >> .gitignore
    git rm -r --cached .claude/
    git commit -m "chore: untrack .claude/"`,
	})

	rules.Register(rules.Rule{
		ID:       "LLM005",
		Title:    "Claude scratchpad / notes file",
		Severity: rules.SevWarning,
		Category: rules.CatClaude,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			"CLAUDE_NOTES.md",
			"**/CLAUDE_NOTES.md",
			"CLAUDE_*.md",
			"**/CLAUDE_*.md",
			".claude.local.md",
			"**/.claude.local.md",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{"CLAUDE_*.md", ".claude.local.md"}},
		Description: "Claude scratchpad/notes files are local working memory and should not ship to production.",
		Remediation: "Add the file (or pattern `CLAUDE_*.md`) to .gitignore and `git rm --cached` it.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM006",
		Title:    ".cursorrules / .cursor/ tracked",
		Severity: rules.SevError,
		Category: rules.CatCursor,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".cursorrules",
			"**/.cursorrules",
			".cursor/**",
			"**/.cursor/**",
			".cursorignore",
			"**/.cursorignore",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".cursorrules", ".cursor/", ".cursorignore"}},
		Description: "Cursor editor rules are developer-environment specific and shouldn't ship in production repos.",
		Remediation: "Add `.cursorrules`, `.cursor/`, and `.cursorignore` to .gitignore; `git rm --cached` the existing entries.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM007",
		Title:    "GitHub Copilot config tracked",
		Severity: rules.SevWarning,
		Category: rules.CatCopilot,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".github/copilot-instructions.md",
			"**/.github/copilot-instructions.md",
			".copilotignore",
			"**/.copilotignore",
			".github/copilot/**",
			"**/.github/copilot/**",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".github/copilot-instructions.md", ".copilotignore", ".github/copilot/"}},
		Description: "Copilot configuration is editor/agent guidance and is best kept out of production repos unless your team has explicitly opted in.",
		Remediation: "If unintentional, add the path to .gitignore and `git rm --cached`. If intentional (team-shared Copilot guidance), add the rule ID to your `.llmlint.yaml` exclude list.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM008",
		Title:    "Aider artifacts tracked",
		Severity: rules.SevWarning,
		Category: rules.CatAider,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".aider*",
			"**/.aider*",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".aider*"}},
		Description: "Aider config/history files (.aider.conf.yml, .aider.chat.history.md, .aider.input.history) are local AI session state.",
		Remediation: "Add `.aider*` to .gitignore and `git rm --cached`.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM009",
		Title:    "Continue config tracked",
		Severity: rules.SevWarning,
		Category: rules.CatContinue,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".continue/**",
			"**/.continue/**",
			".continuerc.json",
			"**/.continuerc.json",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".continue/", ".continuerc.json"}},
		Description: "Continue (continue.dev) editor config is developer-environment specific.",
		Remediation: "Add `.continue/` and `.continuerc.json` to .gitignore.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM010",
		Title:    "Codeium config tracked",
		Severity: rules.SevWarning,
		Category: rules.CatCodeium,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".codeium/**",
			"**/.codeium/**",
			"codeium.toml",
			"**/codeium.toml",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".codeium/", "codeium.toml"}},
		Description: "Codeium config / cache directory is local IDE state.",
		Remediation: "Add `.codeium/` and `codeium.toml` to .gitignore.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM011",
		Title:    "Windsurf config tracked",
		Severity: rules.SevWarning,
		Category: rules.CatWindsurf,
		Kind:     rules.KindPath,
		PathGlobs: []string{
			".windsurfrules",
			"**/.windsurfrules",
			".windsurf/**",
			"**/.windsurf/**",
		},
		AutoFix:     rules.AutoFix{GitignorePatterns: []string{".windsurfrules", ".windsurf/"}},
		Description: "Windsurf editor rules and state are developer-environment specific.",
		Remediation: "Add `.windsurfrules` and `.windsurf/` to .gitignore.",
	})
}
