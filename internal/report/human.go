package report

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/JadenRazo/llm-lint/internal/engine"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"
)

type HumanReporter struct {
	w      io.Writer
	closer io.Closer
	opts   Options
}

func (r *HumanReporter) Write(res *engine.Result) error {
	if r.closer != nil {
		defer r.closer.Close()
	}

	useColor := !r.opts.NoColor && os.Getenv("NO_COLOR") == "" && isTTY(r.w)
	c := newColors(useColor)

	header := fmt.Sprintf("llm-lint %s  scanned %d files + %d commits in %dms",
		r.opts.Version, res.FilesScanned, res.CommitsScanned, res.DurationMS)
	fmt.Fprintln(r.w, c.dim(header))

	if res.GitShallow {
		fmt.Fprintln(r.w, c.warn("note: shallow clone detected — git-history rules ran on partial history."))
	}
	if res.GitSkipped && res.GitSkippedNote != "" {
		fmt.Fprintln(r.w, c.dim("note: git scan skipped: "+res.GitSkippedNote))
	}
	fmt.Fprintln(r.w)

	if len(res.Findings) == 0 {
		fmt.Fprintln(r.w, c.ok("no findings"))
		return nil
	}

	grouped := groupByRule(res.Findings)
	for _, g := range grouped {
		first := g[0]
		glyph, sevText := severityGlyph(first.Severity, c)
		fmt.Fprintf(r.w, "%s %s  %-7s  %s\n", glyph, c.bold(first.RuleID), sevText, first.Title)
		for _, f := range g {
			fmt.Fprintln(r.w, "   "+c.dim("└─ ")+formatLocation(f))
		}
		if first.Remediation != "" {
			fmt.Fprintln(r.w, c.dim(indent(first.Remediation, "      ")))
		}
		fmt.Fprintln(r.w)
	}

	s := res.Summary
	total := s.Error + s.Warning + s.Info
	footer := fmt.Sprintf("%d findings  (%d errors, %d warnings, %d info)",
		total, s.Error, s.Warning, s.Info)
	fmt.Fprintln(r.w, c.bold(footer))
	return nil
}

func groupByRule(fs []findings.Finding) [][]findings.Finding {
	var groups [][]findings.Finding
	current := ""
	for _, f := range fs {
		if f.RuleID != current {
			groups = append(groups, []findings.Finding{f})
			current = f.RuleID
		} else {
			groups[len(groups)-1] = append(groups[len(groups)-1], f)
		}
	}
	return groups
}

func formatLocation(f findings.Finding) string {
	if f.Location.Kind == findings.LocCommit {
		s := fmt.Sprintf("commit %s", short(f.Location.CommitSHA))
		if f.Location.CommitMsg != "" {
			s += fmt.Sprintf(" %q", f.Location.CommitMsg)
		}
		if f.Location.Author != "" {
			s += " (" + f.Location.Author + ")"
		}
		if f.Location.Snippet != "" {
			s += "\n      " + f.Location.Snippet
		}
		return s
	}
	loc := f.Location.Path
	if f.Location.Line > 0 {
		loc = fmt.Sprintf("%s:%d", loc, f.Location.Line)
	}
	if f.Location.Snippet != "" {
		loc += "\n      " + f.Location.Snippet
	}
	return loc
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func severityGlyph(sev rules.Severity, c colors) (string, string) {
	switch sev {
	case rules.SevError:
		return c.err("✗"), c.err("error")
	case rules.SevWarning:
		return c.warn("⚠"), c.warn("warning")
	case rules.SevInfo:
		return c.info("ℹ"), c.info("info")
	}
	return "?", string(sev)
}

type sprintFn func(a ...interface{}) string

type colors struct {
	bold sprintFn
	dim  sprintFn
	err  sprintFn
	warn sprintFn
	info sprintFn
	ok   sprintFn
}

func newColors(enabled bool) colors {
	if !enabled {
		id := sprintFn(func(a ...interface{}) string {
			return fmt.Sprint(a...)
		})
		return colors{bold: id, dim: id, err: id, warn: id, info: id, ok: id}
	}
	return colors{
		bold: color.New(color.Bold).SprintFunc(),
		dim:  color.New(color.Faint).SprintFunc(),
		err:  color.New(color.FgRed, color.Bold).SprintFunc(),
		warn: color.New(color.FgYellow).SprintFunc(),
		info: color.New(color.FgCyan).SprintFunc(),
		ok:   color.New(color.FgGreen, color.Bold).SprintFunc(),
	}
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}
