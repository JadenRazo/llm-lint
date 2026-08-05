package rules

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

func (s Severity) Rank() int {
	switch s {
	case SevError:
		return 3
	case SevWarning:
		return 2
	case SevInfo:
		return 1
	}
	return 0
}

// Valid reports whether s is one of the known severities. A zero-Rank
// severity would silently fall outside every summary bucket and threshold,
// so config validation rejects it up front.
func (s Severity) Valid() bool { return s.Rank() > 0 }

type Category string

const (
	CatClaude   Category = "claude"
	CatCursor   Category = "cursor"
	CatCopilot  Category = "copilot"
	CatAider    Category = "aider"
	CatContinue Category = "continue"
	CatCodeium  Category = "codeium"
	CatWindsurf Category = "windsurf"
	CatGeneric  Category = "generic"
)

// AllCategories lists every known category, for config validation and docs.
func AllCategories() []Category {
	return []Category{
		CatClaude, CatCursor, CatCopilot, CatAider,
		CatContinue, CatCodeium, CatWindsurf, CatGeneric,
	}
}

// ValidCategory reports whether c is a known category.
func ValidCategory(c Category) bool {
	for _, k := range AllCategories() {
		if c == k {
			return true
		}
	}
	return false
}

type Kind string

const (
	KindPath       Kind = "path"
	KindContent    Kind = "content"
	KindGitTrailer Kind = "git_trailer"
	KindGitMessage Kind = "git_message"
)

type Rule struct {
	ID          string
	Title       string
	Severity    Severity
	Category    Category
	Kind        Kind
	Description string
	Remediation string
	AutoFix     AutoFix

	PathGlobs       []string
	ContentPatterns []string
	TrailerPatterns []string
	MessagePatterns []string
}

type AutoFix struct {
	// GitignorePatterns are appended to .gitignore for path rules before the
	// matched path is removed from the git index with `git rm --cached`.
	GitignorePatterns []string
	// RemoveLine removes content-rule lines that match the rule's patterns.
	RemoveLine bool
	// AmendLatestCommit removes matching git-message/trailer lines from HEAD.
	AmendLatestCommit bool
}

type Match struct {
	Rule      Rule
	Path      string
	Line      int
	Snippet   string
	CommitSHA string
	CommitMsg string
	Author    string
}
