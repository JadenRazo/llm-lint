package gitscan

import (
	"testing"
)

// FuzzExtractTrailers fuzzes the paragraph-aware trailer extractor with
// arbitrary commit messages. extractTrailers is the load-bearing parser
// behind LLM003 / LLM012; a panic on adversarial input would crash the
// whole scan. Seeds cover the documented happy paths plus tricky edge
// cases (empty msg, CRLF line endings, multi-paragraph trailer-only,
// trailers-mixed-with-prose, bare colons).
//
// Run locally:
//
//	go test ./internal/gitscan -fuzz=FuzzExtractTrailers -fuzztime=30s
func FuzzExtractTrailers(f *testing.F) {
	for _, seed := range []string{
		"",
		"\n",
		"feat: x\n\nCo-authored-by: Claude <a@b>\n",
		"feat: x\n\nbody line\n\nCo-authored-by: Claude <a@b>\nSigned-off-by: Bob <b@c>\n",
		"feat: x\n\nbody line that is\nactually multi-line\n\nCo-authored-by: Claude <a@b>\n",
		"feat: x\nCo-authored-by: Claude <a@b>\n",
		"feat: x\r\n\r\nCo-authored-by: Claude <a@b>\r\n",
		":\n",
		"A:\n\nB:\n",
		strDup("A: x\n", 100),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		// Must not panic. We don't assert on the return value — the contract
		// is locked by the gitscan integration tests; here we only protect
		// against crashes.
		_ = extractTrailers(msg)
	})
}

func strDup(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
