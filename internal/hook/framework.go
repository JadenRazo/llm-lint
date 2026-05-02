package hook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// pcConfig and friends are the minimal shape needed to FIND an existing
// llm-lint entry. We intentionally do not marshal+rewrite the file — that
// would lose comments and reorder keys. We use this struct for inspection
// and surgical text edits for mutation.
type pcConfig struct {
	Repos []pcRepo `json:"repos"`
}

type pcRepo struct {
	Repo  string   `json:"repo"`
	Rev   string   `json:"rev,omitempty"`
	Hooks []pcHook `json:"hooks"`
}

type pcHook struct {
	ID string `json:"id"`
}

func frameworkPath(repoRoot string) string {
	return filepath.Join(repoRoot, ConfigFile)
}

// inspectFramework reports whether a llm-lint entry exists in
// .pre-commit-config.yaml and returns its rev field.
func inspectFramework(repoRoot string) (present bool, rev string, err error) {
	path := frameworkPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	if isMultiDocYAML(data) {
		return false, "", fmt.Errorf("multi-document %s not supported; edit manually", ConfigFile)
	}

	var cfg pcConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false, "", fmt.Errorf("parse %s: %w", path, err)
	}
	for _, r := range cfg.Repos {
		if matchesLLMLintRepo(r.Repo) {
			for _, h := range r.Hooks {
				if h.ID == "llm-lint" {
					return true, r.Rev, nil
				}
			}
		}
	}
	return false, "", nil
}

func installFramework(opts InstallOptions) (Status, error) {
	rev, note, err := resolveRev(opts)
	if err != nil {
		return Status{}, err
	}
	st := Status{
		FrameworkPath:   frameworkPath(opts.RepoRoot),
		PreCommitOnPath: hasPreCommitFramework(),
	}
	if note != "" {
		st.Notes = append(st.Notes, note)
	}

	path := frameworkPath(opts.RepoRoot)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return st, err
	}

	if errors.Is(err, os.ErrNotExist) || len(bytes.TrimSpace(data)) == 0 {
		// Brand-new file: write the canonical shape.
		out := minimalFrameworkConfig(rev)
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			return st, err
		}
		st.State = StateFramework
		st.FrameworkRev = rev
		return st, nil
	}

	if isMultiDocYAML(data) {
		return st, fmt.Errorf("multi-document %s not supported; edit manually", ConfigFile)
	}

	present, existingRev, err := inspectFramework(opts.RepoRoot)
	if err != nil {
		return st, err
	}

	if !present {
		// Append a new repo block.
		out := appendCanonicalEntry(string(data), rev)
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			return st, err
		}
		st.State = StateFramework
		st.FrameworkRev = rev
		return st, nil
	}

	if existingRev == rev {
		st.State = StateFramework
		st.FrameworkRev = rev
		st.Notes = append(st.Notes, "already installed")
		return st, nil
	}

	// Surgical rev-line replacement.
	out, ok := replaceRev(string(data), rev)
	if !ok {
		st.Notes = append(st.Notes, "could not update rev (entry uses YAML anchor or unusual shape); update manually")
		st.State = StateFramework
		st.FrameworkRev = existingRev
		return st, nil
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return st, err
	}
	st.State = StateFramework
	st.FrameworkRev = rev
	return st, nil
}

// uninstallFramework removes the llm-lint repo block from the config file.
// Best-effort surgical removal (preserves comments around other repos);
// falls back to leaving the file untouched if the structure is unusual.
func uninstallFramework(repoRoot string) error {
	path := frameworkPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if isMultiDocYAML(data) {
		return fmt.Errorf("multi-document %s not supported; edit manually", ConfigFile)
	}
	out, ok := removeLLMLintEntry(string(data))
	if !ok {
		// Nothing to remove.
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// matchesLLMLintRepo accepts the canonical URL plus common variants
// (trailing slash, .git suffix). Case-insensitive.
func matchesLLMLintRepo(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.EqualFold(s, RepoURL)
}

func minimalFrameworkConfig(rev string) string {
	return fmt.Sprintf(`repos:
  - repo: %s
    rev: %s
    hooks:
      - id: llm-lint
`, RepoURL, rev)
}

func appendCanonicalEntry(existing, rev string) string {
	if !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	// If the file lacks a top-level `repos:` key, write a fresh minimal config.
	if !regexp.MustCompile(`(?m)^repos\s*:`).MatchString(existing) {
		return existing + minimalFrameworkConfig(rev)
	}
	block := fmt.Sprintf(`  - repo: %s
    rev: %s
    hooks:
      - id: llm-lint
`, RepoURL, rev)
	return existing + block
}

// llmLintBlockRe matches the start of the llm-lint repo block. Case-insensitive
// match on the URL with optional trailing /.git/ slash.
var llmLintBlockRe = regexp.MustCompile(
	`(?im)^(\s*-\s+repo\s*:\s*)https?://github\.com/JadenRazo/llm-lint(?:\.git)?/?\s*$`,
)

// revLineRe matches a `rev:` line anywhere; the caller bounds the search.
var revLineRe = regexp.MustCompile(`(?m)^(\s*rev\s*:\s*)\S+(\s*)$`)

// replaceRev surgically replaces the rev: value within the llm-lint block.
// Returns (new, true) on success, ("", false) if the structure is unusual
// (e.g. anchor/alias, missing rev within the block).
func replaceRev(content, newRev string) (string, bool) {
	loc := llmLintBlockRe.FindStringIndex(content)
	if loc == nil {
		return "", false
	}
	// Find the next sibling repo (same or shallower indent) or EOF.
	end := nextSiblingRepoOrEOF(content, loc[1])

	block := content[loc[1]:end]
	idx := revLineRe.FindStringSubmatchIndex(block)
	if idx == nil {
		return "", false
	}
	prefix := block[idx[2]:idx[3]]
	suffix := block[idx[4]:idx[5]]
	newBlock := block[:idx[0]] + prefix + newRev + suffix + block[idx[1]:]
	return content[:loc[1]] + newBlock + content[end:], true
}

// removeLLMLintEntry deletes the entire `- repo: <llm-lint>` block from
// content. Returns (new, true) on removal, (content, false) if no entry.
func removeLLMLintEntry(content string) (string, bool) {
	loc := llmLintBlockRe.FindStringIndex(content)
	if loc == nil {
		return content, false
	}
	// Block extends from start of the matching `- repo:` line to the next
	// sibling `- repo:` (same indent) or EOF.
	startLine := lineStart(content, loc[0])
	end := nextSiblingRepoOrEOF(content, loc[1])
	return content[:startLine] + content[end:], true
}

// nextSiblingRepoOrEOF finds the next `- repo:` line at the same indent
// as the one starting at `from`, or EOF.
func nextSiblingRepoOrEOF(content string, from int) int {
	lines := strings.SplitAfter(content[from:], "\n")
	siblingRe := regexp.MustCompile(`^\s*-\s+repo\s*:`)
	pos := from
	for _, ln := range lines {
		if siblingRe.MatchString(ln) {
			return pos
		}
		pos += len(ln)
	}
	return len(content)
}

func lineStart(content string, idx int) int {
	if idx == 0 {
		return 0
	}
	if i := strings.LastIndex(content[:idx], "\n"); i >= 0 {
		return i + 1
	}
	return 0
}

// isMultiDocYAML returns true if content has more than one YAML document
// (i.e. `---` appears at column 0 on a non-first line).
func isMultiDocYAML(data []byte) bool {
	lines := bytes.Split(data, []byte{'\n'})
	count := 0
	for _, ln := range lines {
		s := strings.TrimRight(string(ln), " \t\r")
		if s == "---" {
			count++
		}
	}
	return count > 1
}
