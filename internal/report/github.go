package report

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

// FormatGitHub emits GitHub Actions workflow command annotations on stdout
// plus an optional Markdown summary appended to GITHUB_STEP_SUMMARY and an
// optional sticky PR comment via the REST API.
const FormatGitHub Format = "github"

// stickyMarker is the HTML-comment marker used to find/edit the existing
// sticky PR comment on subsequent runs. Same string at top and bottom of
// the comment body.
const stickyMarker = "<!-- llm-lint-sticky -->"

// annotationMaxBytes is the GitHub-imposed hard limit on workflow command
// message length. We cut at 4080 with a 16-byte truncation suffix so the
// final encoded message stays comfortably below the cap.
const annotationMaxBytes = 4096

const truncationSuffix = "…[truncated]"

// GitHubOptions controls the github reporter. Tests inject httpClient and
// apiBaseURL via the (unexported) fields below by going through the
// package-level setters in export_test.go.
type GitHubOptions struct {
	PRComment     bool
	PRCommentMode string // "sticky" (default) | "append"
	Token         string
	Repo          string
	PRNumber      int

	// test-only injection points
	httpClient httpDoer
	apiBaseURL string
}

// GitHubReporter is the layer-A annotation emitter. It also writes the
// step-summary Markdown if GITHUB_STEP_SUMMARY is set, and posts a sticky
// PR comment if PRComment is enabled.
type GitHubReporter struct {
	w      io.Writer
	closer io.Closer
	opts   Options

	env    func(string) string
	stderr io.Writer
	now    func() time.Time
}

func newGitHubReporter(w io.Writer, closer io.Closer, opts Options) *GitHubReporter {
	return &GitHubReporter{
		w:      w,
		closer: closer,
		opts:   opts,
		env:    os.Getenv,
		stderr: os.Stderr,
		now:    time.Now,
	}
}

// Write emits all three layers in order: annotations to r.w, summary to
// $GITHUB_STEP_SUMMARY, sticky PR comment via API.
func (r *GitHubReporter) Write(res *engine.Result) error {
	if r.closer != nil {
		defer r.closer.Close()
	}

	if r.opts.GitHub.PRComment {
		mode := r.opts.GitHub.PRCommentMode
		if mode != "" && mode != "sticky" && mode != "append" {
			return fmt.Errorf("invalid pr-comment-mode %q (want sticky|append)", mode)
		}
	}

	r.emitAnnotations(res)

	body := buildSummaryMarkdown(res, r.opts.Version)

	if path := r.env("GITHUB_STEP_SUMMARY"); path != "" {
		if err := appendToFile(path, body); err != nil {
			fmt.Fprintf(r.stderr, "llm-lint: failed to write GITHUB_STEP_SUMMARY: %v\n", err)
		}
	} else if r.env("GITHUB_ACTIONS") == "true" {
		fmt.Fprintln(r.stderr, "llm-lint: GITHUB_STEP_SUMMARY not set, skipping workflow step summary")
	}

	if r.opts.GitHub.PRComment {
		if err := r.postPRComment(body); err != nil {
			fmt.Fprintf(r.stderr, "llm-lint: PR comment skipped: %v\n", err)
		}
	}

	return nil
}

// emitAnnotations writes one workflow command per file-located finding,
// sorted by (file, line, ruleID) for grouping in PR review UIs.
func (r *GitHubReporter) emitAnnotations(res *engine.Result) {
	var fs []findings.Finding
	for _, f := range res.Findings {
		if f.Location.Kind != findings.LocFile || f.Location.Path == "" {
			continue
		}
		fs = append(fs, f)
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Location.Path != fs[j].Location.Path {
			return fs[i].Location.Path < fs[j].Location.Path
		}
		if fs[i].Location.Line != fs[j].Location.Line {
			return fs[i].Location.Line < fs[j].Location.Line
		}
		return fs[i].RuleID < fs[j].RuleID
	})

	for _, f := range fs {
		level := AnnotationLevel(f.Severity)
		file := EscapeAnnotationProperty(f.Location.Path)
		line := f.Location.Line
		if line < 1 {
			line = 1
		}
		title := EscapeAnnotationProperty(f.RuleID + ": " + f.Title)

		var bodyParts []string
		if f.Description != "" {
			bodyParts = append(bodyParts, f.Description)
		}
		if f.Location.Snippet != "" {
			bodyParts = append(bodyParts, "", f.Location.Snippet)
		}
		if f.Remediation != "" {
			bodyParts = append(bodyParts, "", f.Remediation)
		}
		body := strings.Join(bodyParts, "\n")
		msg := truncateEncoded(EscapeAnnotationMessage(body), annotationMaxBytes)

		_, _ = fmt.Fprintf(r.w, "::%s file=%s,line=%d,col=1,title=%s::%s\n",
			level, file, line, title, msg)
	}
}

// EscapeAnnotationMessage encodes the message-body portion of a workflow
// command. Order matters: % must be encoded first, then \r and \n. Comma
// and colon pass through in message context.
func EscapeAnnotationMessage(s string) string {
	if !strings.ContainsAny(s, "%\r\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			b.WriteString("%25")
		case '\r':
			b.WriteString("%0D")
		case '\n':
			b.WriteString("%0A")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// EscapeAnnotationProperty encodes a property-block value (e.g. file=…,
// title=…). In addition to the message escapes, comma and colon must be
// encoded because they delimit the property block.
func EscapeAnnotationProperty(s string) string {
	if !strings.ContainsAny(s, "%\r\n,:") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			b.WriteString("%25")
		case '\r':
			b.WriteString("%0D")
		case '\n':
			b.WriteString("%0A")
		case ':':
			b.WriteString("%3A")
		case ',':
			b.WriteString("%2C")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// AnnotationLevel maps a rule severity to the GitHub Actions annotation level.
func AnnotationLevel(s rules.Severity) string {
	switch s {
	case rules.SevError:
		return "error"
	case rules.SevWarning:
		return "warning"
	case rules.SevInfo:
		return "notice"
	default:
		return "notice"
	}
}

// truncateEncoded cuts an already-encoded message at a UTF-8-safe AND
// percent-escape-safe boundary, appending truncationSuffix.
func truncateEncoded(s string, max int) string {
	if len(s) < max {
		return s
	}
	target := max - len(truncationSuffix)
	if target <= 0 {
		return truncationSuffix[:max]
	}
	cut := target
	if cut > len(s) {
		cut = len(s)
	}
	// Walk back continuation bytes (10xxxxxx) so cut lands on a UTF-8
	// boundary; if the byte just before cut is a multi-byte lead, step
	// back one more to fully exclude the partial rune.
	for cut > 0 && (s[cut-1]&0xC0 == 0x80) {
		cut--
	}
	if cut > 0 {
		b := s[cut-1]
		if b&0xE0 == 0xC0 || b&0xF0 == 0xE0 || b&0xF8 == 0xF0 {
			cut--
		}
	}
	// Walk back past an in-progress percent escape (% or %X with X being hex).
	if cut >= 1 && s[cut-1] == '%' {
		cut--
	} else if cut >= 2 && s[cut-2] == '%' && isHex(s[cut-1]) {
		cut -= 2
	}
	if cut < 0 {
		cut = 0
	}
	return s[:cut] + truncationSuffix
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'A' && b <= 'F') || (b >= 'a' && b <= 'f')
}

// AutoDetectFormat picks the format. User wins (formatChanged=true).
// Otherwise, when running under GITHUB_ACTIONS=true, default to github.
func AutoDetectFormat(env func(string) string, formatChanged bool, current string) string {
	if formatChanged {
		return current
	}
	if env("GITHUB_ACTIONS") != "true" {
		return current
	}
	return string(FormatGitHub)
}

// buildSummaryMarkdown assembles the workflow-summary Markdown. The same
// body is reused for the sticky PR comment (with marker comments wrapping
// it).
func buildSummaryMarkdown(res *engine.Result, version string) string {
	var b strings.Builder
	b.WriteString("## llm-lint findings\n\n")
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(&b, "llm-lint v%s scanned %d files and %d commits in %dms.\n",
		version, res.FilesScanned, res.CommitsScanned, res.DurationMS)
	if res.GitShallow {
		b.WriteString("\nNote: shallow clone — only commits in the local fetch were considered.\n")
	}
	if res.GitSkipped && res.GitSkippedNote != "" {
		fmt.Fprintf(&b, "\nNote: git scan skipped (%s).\n", res.GitSkippedNote)
	}

	if len(res.Findings) == 0 {
		b.WriteString("\nNo findings.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "\n**%d findings** (%d errors, %d warnings, %d info)\n",
		len(res.Findings), res.Summary.Error, res.Summary.Warning, res.Summary.Info)

	b.WriteString("\n### By rule\n\n")
	b.WriteString("| Rule | Category | Severity | Count | Title |\n")
	b.WriteString("|------|----------|----------|------:|-------|\n")
	type ruleStat struct {
		ID, Cat, Sev, Title string
		Count               int
		SevRank             int
	}
	statByID := map[string]*ruleStat{}
	for _, f := range res.Findings {
		st, ok := statByID[f.RuleID]
		if !ok {
			st = &ruleStat{
				ID:      f.RuleID,
				Cat:     string(f.Category),
				Sev:     string(f.Severity),
				Title:   f.Title,
				SevRank: f.Severity.Rank(),
			}
			statByID[f.RuleID] = st
		}
		st.Count++
	}
	stats := make([]*ruleStat, 0, len(statByID))
	for _, s := range statByID {
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].SevRank != stats[j].SevRank {
			return stats[i].SevRank > stats[j].SevRank
		}
		return stats[i].ID < stats[j].ID
	})
	for _, s := range stats {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %s |\n",
			s.ID, s.Cat, s.Sev, s.Count, escapeMarkdownCell(s.Title))
	}

	// Top files (file findings only)
	pathCounts := map[string]int{}
	for _, f := range res.Findings {
		if f.Location.Kind == findings.LocFile && f.Location.Path != "" {
			pathCounts[f.Location.Path]++
		}
	}
	if len(pathCounts) > 0 {
		type pathStat struct {
			Path  string
			Count int
		}
		ps := make([]pathStat, 0, len(pathCounts))
		for p, c := range pathCounts {
			ps = append(ps, pathStat{p, c})
		}
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Count != ps[j].Count {
				return ps[i].Count > ps[j].Count
			}
			return ps[i].Path < ps[j].Path
		})
		if len(ps) > 10 {
			ps = ps[:10]
		}
		b.WriteString("\n### Top files\n\n")
		b.WriteString("| File | Findings |\n|------|---------:|\n")
		for _, p := range ps {
			fmt.Fprintf(&b, "| `%s` | %d |\n", escapeBacktickPath(p.Path), p.Count)
		}
	}

	// Commit findings (no inline-annotation surface for these)
	var commitFindings []findings.Finding
	for _, f := range res.Findings {
		if f.Location.Kind == findings.LocCommit {
			commitFindings = append(commitFindings, f)
		}
	}
	if len(commitFindings) > 0 {
		b.WriteString("\n### Commit findings\n\n")
		b.WriteString("These don't surface as inline annotations.\n\n")
		for _, f := range commitFindings {
			sha := f.Location.CommitSHA
			if len(sha) > 7 {
				sha = sha[:7]
			}
			fmt.Fprintf(&b, "- `%s` %s — `%s` (%s)\n",
				sha, f.Title,
				escapeMarkdownCell(f.Location.CommitMsg),
				escapeMarkdownCell(f.Location.Author))
		}
	}

	// Per-rule remediation block, deduped
	seenRule := map[string]bool{}
	var remediation strings.Builder
	for _, f := range res.Findings {
		if seenRule[f.RuleID] {
			continue
		}
		seenRule[f.RuleID] = true
		first := strings.SplitN(f.Remediation, "\n", 2)[0]
		fmt.Fprintf(&remediation, "- **%s**: %s\n", f.RuleID, first)
	}
	if remediation.Len() > 0 {
		b.WriteString("\n### Remediation\n\nRun `llm-lint scan` locally to reproduce. Per-rule remediation:\n\n")
		b.WriteString(remediation.String())
	}

	b.WriteString("\n---\n<sub>Generated by [llm-lint](https://github.com/JadenRazo/llm-lint) v")
	b.WriteString(version)
	b.WriteString(".</sub>\n")
	return b.String()
}

func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func escapeBacktickPath(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.WriteString(f, content)
	return err
}

// postPRComment attempts to post (or edit) a sticky PR comment. Errors are
// printed to stderr and swallowed — layer C must never fail the build.
func (r *GitHubReporter) postPRComment(body string) error {
	c, err := newPRClient(r.opts.GitHub, r.env)
	if err != nil {
		return err
	}
	wrapped := stickyMarker + "\n\n" + body + "\n" + stickyMarker + "\n"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if r.opts.GitHub.PRCommentMode == "append" {
		return c.postAppend(ctx, wrapped)
	}
	return c.postSticky(ctx, wrapped)
}

