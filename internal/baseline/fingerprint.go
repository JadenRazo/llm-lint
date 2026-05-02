package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

// fingerprintSep is the byte separator inside the hash payload. NUL is
// safe because no path or rule ID can legitimately contain it; it makes
// injection collisions impossible (there's no escape needed).
const fingerprintSep = byte(0x00)

// Fingerprint returns "sha256:<hex>" for f. Composition rules (per rule kind):
//
//   - path findings (LLM001/2/5/6/7/8/9/10/11/15):
//     sha256(rule_id || NUL || "path"    || NUL || slash_path)
//   - content findings (LLM013/14):
//     sha256(rule_id || NUL || "content" || NUL || normalized_match)
//   - trailer findings (LLM003/12):
//     sha256(rule_id || NUL || "trailer" || NUL || sha)
//   - message findings (LLM004):
//     sha256(rule_id || NUL || "message" || NUL || sha)
//
// Severity, description, and remediation are deliberately NOT in the
// fingerprint — they are tool-owned config and presentation, not
// finding identity. A team that downgrades LLM013 from info to warning
// should not need to regenerate the baseline.
//
// Returns "" for findings whose rule is unknown to the registry. The
// caller (Apply / BuildEntries) treats empty as "skip".
func Fingerprint(f findings.Finding) string {
	rule, ok := rules.Get(f.RuleID)
	if !ok {
		return ""
	}
	var payload []byte
	switch rule.Kind {
	case rules.KindPath:
		payload = composePayload(f.RuleID, "path", filepath.ToSlash(f.Location.Path))
	case rules.KindContent:
		payload = composePayload(f.RuleID, "content", normalizeContent(f.Location.Snippet))
	case rules.KindGitTrailer:
		payload = composePayload(f.RuleID, "trailer", f.Location.CommitSHA)
	case rules.KindGitMessage:
		payload = composePayload(f.RuleID, "message", f.Location.CommitSHA)
	default:
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func composePayload(ruleID, kind, body string) []byte {
	out := make([]byte, 0, len(ruleID)+len(kind)+len(body)+3)
	out = append(out, ruleID...)
	out = append(out, fingerprintSep)
	out = append(out, kind...)
	out = append(out, fingerprintSep)
	out = append(out, body...)
	return out
}

// normalizeContent collapses CRLF to LF and trims surrounding whitespace
// so trivial line-ending or whitespace differences don't churn baselines.
// Note: scanner truncates snippets to 200 chars deterministically, so
// truncation is part of the fingerprint payload — changing scanner.trim()
// is a baseline-breaking change.
func normalizeContent(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

// locationLabel returns the on-disk Location label for a finding, derived
// from the rule kind (NOT from f.Location.Kind, which collapses trailer
// and message into "commit").
func locationLabel(f findings.Finding) string {
	rule, ok := rules.Get(f.RuleID)
	if !ok {
		return ""
	}
	switch rule.Kind {
	case rules.KindPath, rules.KindContent:
		return locFile
	case rules.KindGitTrailer:
		return locTrailer
	case rules.KindGitMessage:
		return locMessage
	default:
		return ""
	}
}
