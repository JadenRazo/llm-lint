package builtin

import "github.com/JadenRazo/llm-lint/internal/rules"

func init() {
	rules.Register(rules.Rule{
		ID:       "LLM003",
		Title:    "Co-authored-by: Claude trailer",
		Severity: rules.SevError,
		Category: rules.CatClaude,
		Kind:     rules.KindGitTrailer,
		TrailerPatterns: []string{
			// Anthropic-issued Claude email — the canonical Claude Code trailer.
			`(?i)^co-authored-by:\s*claude\b.*<[^>]*@anthropic\.com>`,
			// "Claude Code" name — catches future variants that might omit the email.
			// Deliberately does NOT match a bare `Claude <…>` since real human
			// contributors are sometimes named Claude.
			`(?i)^co-authored-by:\s*claude\s+code\b`,
		},
		AutoFix:     rules.AutoFix{AmendLatestCommit: true},
		Description: "Commit trailer attributing co-authorship to Claude. Many production teams strip these to keep `git log` author-clean.",
		Remediation: `To prevent this on future commits, edit your local Claude Code settings:

    # ~/.claude/settings.json
    {
      "includeCoAuthoredBy": false
    }

Or run:

    claude config set includeCoAuthoredBy false

To clean history, reword the offending commits with an interactive rebase
(git rebase -i <sha>^) or strip them in bulk with:

    git filter-repo --message-callback '
      return re.sub(rb"(?im)^co-authored-by:\s*claude.*\n?", b"", message)
    '`,
	})

	rules.Register(rules.Rule{
		ID:       "LLM004",
		Title:    "Claude attribution in commit message",
		Severity: rules.SevWarning,
		Category: rules.CatClaude,
		Kind:     rules.KindGitMessage,
		MessagePatterns: []string{
			`🤖 Generated with \[Claude Code\]`,
			`(?i)generated with claude`,
		},
		AutoFix:     rules.AutoFix{AmendLatestCommit: true},
		Description: "Commit message body advertises Claude generation. The same `includeCoAuthoredBy` setting gates this footer in recent Claude Code versions.",
		Remediation: "Set `includeCoAuthoredBy: false` in `~/.claude/settings.json` to stop this in future commits, then `git rebase -i` to reword existing offenders.",
	})

	rules.Register(rules.Rule{
		ID:       "LLM012",
		Title:    "Co-authored-by trailer from another AI tool",
		Severity: rules.SevWarning,
		Category: rules.CatGeneric,
		Kind:     rules.KindGitTrailer,
		TrailerPatterns: []string{
			`(?i)^co-authored-by:[^\n]*<[^>]*@(openai|cursor|codeium|aider)\.[^>]+>`,
			`(?i)^co-authored-by:\s*github\s+copilot\b`,
			`(?i)^co-authored-by:\s*copilot\s*<`,
		},
		AutoFix:     rules.AutoFix{AmendLatestCommit: true},
		Description: "Generic AI co-authorship trailer (Copilot, OpenAI, Cursor, Codeium, Aider).",
		Remediation: "Configure the relevant tool to stop appending its trailer; rebase or `git filter-repo` to clean history.",
	})
}
