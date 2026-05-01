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

	PathGlobs       []string
	ContentPatterns []string
	TrailerPatterns []string
	MessagePatterns []string
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
