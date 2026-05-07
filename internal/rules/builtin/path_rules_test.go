package builtin_test

import (
	"regexp"
	"testing"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func TestPathGlobs(t *testing.T) {
	cases := []struct {
		path string
		rule string
		want bool
	}{
		{"CLAUDE.md", "LLM001", true},
		{"docs/CLAUDE.md", "LLM001", true},
		{"src/Claude.md", "LLM001", true},
		{"README.md", "LLM001", false},

		{".claude/settings.json", "LLM002", true},
		{"app/.claude/state.json", "LLM002", true},
		{"claude/settings.json", "LLM002", false},

		{"CLAUDE_NOTES.md", "LLM005", true},
		{"docs/CLAUDE_TODO.md", "LLM005", true},
		{".claude.local.md", "LLM005", true},
		{"claude.md", "LLM005", false},

		{".cursorrules", "LLM006", true},
		{"packages/api/.cursorrules", "LLM006", true},
		{".cursor/settings.json", "LLM006", true},
		{".cursorignore", "LLM006", true},
		{"cursorrules", "LLM006", false},

		{".github/copilot-instructions.md", "LLM007", true},
		{".github/copilot/extra.md", "LLM007", true},
		{".copilotignore", "LLM007", true},
		{".github/dependabot.yml", "LLM007", false},

		{".aider.conf.yml", "LLM008", true},
		{".aider.chat.history.md", "LLM008", true},
		{"src/.aider.input.history", "LLM008", true},
		{"aider.yaml", "LLM008", false},

		{".continue/config.yml", "LLM009", true},
		{".continuerc.json", "LLM009", true},

		{".codeium/cache.bin", "LLM010", true},
		{"codeium.toml", "LLM010", true},

		{".windsurfrules", "LLM011", true},
		{".windsurf/state.json", "LLM011", true},
	}

	for _, tc := range cases {
		r, ok := rules.Get(tc.rule)
		if !ok {
			t.Fatalf("rule %s not registered", tc.rule)
		}
		matched := false
		for _, glob := range r.PathGlobs {
			ok, err := doublestar.PathMatch(glob, tc.path)
			if err != nil {
				t.Fatalf("rule %s glob %q invalid: %v", tc.rule, glob, err)
			}
			if ok {
				matched = true
				break
			}
		}
		if matched != tc.want {
			t.Errorf("rule %s on path %q: got match=%v want=%v", tc.rule, tc.path, matched, tc.want)
		}
	}
}

func TestLLM015ContentPathGlobsAndPatterns(t *testing.T) {
	r, ok := rules.Get("LLM015")
	if !ok {
		t.Fatal("LLM015 not registered")
	}
	if r.Kind != rules.KindContent {
		t.Fatalf("LLM015 kind = %s, want content", r.Kind)
	}

	pathCases := []struct {
		path string
		want bool
	}{
		{".mcp.json", true},
		{"sub/.mcp.json", true},
		{"mcp.json", false},
	}
	for _, tc := range pathCases {
		matched := false
		for _, glob := range r.ContentPathGlobs {
			ok, err := doublestar.PathMatch(glob, tc.path)
			if err != nil {
				t.Fatalf("rule %s content path glob %q invalid: %v", r.ID, glob, err)
			}
			if ok {
				matched = true
				break
			}
		}
		if matched != tc.want {
			t.Errorf("LLM015 on path %q: got match=%v want=%v", tc.path, matched, tc.want)
		}
	}

	var contentMatch bool
	for _, pat := range r.ContentPatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			t.Fatalf("LLM015 pattern %q invalid: %v", pat, err)
		}
		if re.MatchString(`{"mcpServers":{"claude-code":{"command":"claude-code"}}}`) {
			contentMatch = true
		}
		if re.MatchString(`{"mcpServers":{"local-dev":{"command":"dev-server"}}}`) {
			t.Fatalf("LLM015 pattern %q matched unrelated MCP config", pat)
		}
	}
	if !contentMatch {
		t.Fatal("LLM015 patterns did not match claude-code MCP content")
	}
}

func TestRequiredRulesRegistered(t *testing.T) {
	required := []string{
		"LLM001", "LLM002", "LLM003", "LLM004", "LLM005",
		"LLM006", "LLM007", "LLM008", "LLM009", "LLM010",
		"LLM011", "LLM012", "LLM013", "LLM014", "LLM015",
	}
	for _, id := range required {
		if _, ok := rules.Get(id); !ok {
			t.Errorf("rule %s not registered", id)
		}
	}
}

func TestRulesHaveRemediation(t *testing.T) {
	for _, r := range rules.All() {
		if r.Remediation == "" {
			t.Errorf("rule %s has no Remediation", r.ID)
		}
		if r.Description == "" {
			t.Errorf("rule %s has no Description", r.ID)
		}
		if r.Title == "" {
			t.Errorf("rule %s has no Title", r.ID)
		}
	}
}

func TestLLM003RemediationMentionsClaudeSettings(t *testing.T) {
	r, ok := rules.Get("LLM003")
	if !ok {
		t.Fatal("LLM003 not registered")
	}
	const want = "~/.claude/settings.json"
	if !contains(r.Remediation, want) {
		t.Errorf("LLM003 remediation must reference %q; got:\n%s", want, r.Remediation)
	}
	if !contains(r.Remediation, "includeCoAuthoredBy") {
		t.Errorf("LLM003 remediation must reference includeCoAuthoredBy; got:\n%s", r.Remediation)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	hn, nn := len(haystack), len(needle)
	for i := 0; i+nn <= hn; i++ {
		if haystack[i:i+nn] == needle {
			return i
		}
	}
	return -1
}
