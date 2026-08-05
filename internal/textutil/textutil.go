// Package textutil holds small, dependency-free text and terminal helpers
// shared by the CLI, reporters, fixer, and progress packages. Everything
// here must stay trivial: no config, no logging, no package-level state.
package textutil

import (
	"io"
	"os"
	"strings"
)

// ShortSHA truncates a git SHA to the conventional 7-character short form.
// Inputs shorter than 8 characters are returned unchanged.
func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Indent prefixes every line of s with pad. Trailing newlines are trimmed
// first so the result never ends with a padded empty line.
func Indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// IsTTY reports whether w is an *os.File attached to a character device
// (a terminal). Any non-file writer, or a file that cannot be stat'ed,
// reports false.
func IsTTY(w io.Writer) bool {
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
