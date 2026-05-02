package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/baseline"
	"github.com/JadenRazo/llm-lint/internal/findings"
	"github.com/JadenRazo/llm-lint/internal/rules"

	_ "github.com/JadenRazo/llm-lint/internal/rules/builtin"
)

func mustFingerprint(t *testing.T, f findings.Finding) string {
	t.Helper()
	fp := baseline.Fingerprint(f)
	if fp == "" {
		t.Fatalf("fingerprint empty for rule %q", f.RuleID)
	}
	return fp
}

func TestFingerprint_StableForKnownPath(t *testing.T) {
	f := findings.Finding{
		RuleID:   "LLM001",
		Severity: rules.SevError,
		Category: rules.CatClaude,
		Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"},
	}
	fp1 := mustFingerprint(t, f)
	fp2 := mustFingerprint(t, f)
	if fp1 != fp2 {
		t.Errorf("fingerprint must be deterministic; got %s, %s", fp1, fp2)
	}
	if !strings.HasPrefix(fp1, "sha256:") {
		t.Errorf("fingerprint must be sha256:<hex>; got %s", fp1)
	}
}

func TestFingerprint_PathRenameInvalidates(t *testing.T) {
	a := findings.Finding{RuleID: "LLM001", Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"}}
	b := findings.Finding{RuleID: "LLM001", Location: findings.Location{Kind: findings.LocFile, Path: "docs/CLAUDE.md"}}
	if baseline.Fingerprint(a) == baseline.Fingerprint(b) {
		t.Errorf("path findings with different paths must have different fingerprints")
	}
}

func TestFingerprint_ContentMoveStable(t *testing.T) {
	// Same content rule, same matched snippet, different file — content
	// fingerprint is path-independent (by design), so they collide.
	a := findings.Finding{RuleID: "LLM013", Location: findings.Location{Kind: findings.LocFile, Path: "src/a.py", Snippet: "as an AI language model"}}
	b := findings.Finding{RuleID: "LLM013", Location: findings.Location{Kind: findings.LocFile, Path: "src/b.py", Snippet: "as an AI language model"}}
	if baseline.Fingerprint(a) != baseline.Fingerprint(b) {
		t.Errorf("content findings with same snippet should have same fingerprint regardless of path")
	}
}

func TestFingerprint_CRLFEqualsLF(t *testing.T) {
	a := findings.Finding{RuleID: "LLM013", Location: findings.Location{Kind: findings.LocFile, Snippet: "foo"}}
	b := findings.Finding{RuleID: "LLM013", Location: findings.Location{Kind: findings.LocFile, Snippet: "foo\r\n"}}
	c := findings.Finding{RuleID: "LLM013", Location: findings.Location{Kind: findings.LocFile, Snippet: "foo\n"}}
	if baseline.Fingerprint(a) != baseline.Fingerprint(b) {
		t.Errorf("CRLF normalization broken: %s vs %s", baseline.Fingerprint(a), baseline.Fingerprint(b))
	}
	if baseline.Fingerprint(b) != baseline.Fingerprint(c) {
		t.Errorf("LF normalization broken: %s vs %s", baseline.Fingerprint(b), baseline.Fingerprint(c))
	}
}

func TestFingerprint_TrailerByCommit(t *testing.T) {
	a := findings.Finding{RuleID: "LLM003", Location: findings.Location{Kind: findings.LocCommit, CommitSHA: "abc"}}
	b := findings.Finding{RuleID: "LLM003", Location: findings.Location{Kind: findings.LocCommit, CommitSHA: "def"}}
	if baseline.Fingerprint(a) == baseline.Fingerprint(b) {
		t.Errorf("trailer findings on different commits must differ")
	}
}

func TestFingerprint_UnknownRuleEmpty(t *testing.T) {
	f := findings.Finding{RuleID: "LLM999"}
	if got := baseline.Fingerprint(f); got != "" {
		t.Errorf("unknown rule must produce empty fingerprint; got %s", got)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	doc := baseline.New("llm-lint v1.2.3-test")
	doc.Entries = []baseline.Entry{
		{Rule: "LLM001", Fingerprint: "sha256:aaa", Location: "file", Path: "CLAUDE.md"},
		{Rule: "LLM003", Fingerprint: "sha256:bbb", Location: "git_trailer", SHA: "abc1234"},
	}
	if err := baseline.Save(doc, path); err != nil {
		t.Fatal(err)
	}
	got, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 {
		t.Errorf("Total: got %d want 2", got.Total)
	}
	if got.Version != baseline.BaselineSchemaVersion {
		t.Errorf("Version: got %d want %d", got.Version, baseline.BaselineSchemaVersion)
	}
	if got.Entries[0].Fingerprint != "sha256:aaa" {
		t.Errorf("first entry: got %v", got.Entries[0])
	}
}

func TestSave_AtomicNoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	doc := baseline.New("test")
	if err := baseline.Save(doc, path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "baseline.yaml" {
			t.Errorf("unexpected file in dir after Save: %s", e.Name())
		}
	}
}

func TestSave_DeterministicOrdering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	doc := baseline.New("test")
	doc.Entries = []baseline.Entry{
		{Rule: "LLM003", Fingerprint: "sha256:zzz"},
		{Rule: "LLM001", Fingerprint: "sha256:bbb"},
		{Rule: "LLM001", Fingerprint: "sha256:aaa"},
		{Rule: "LLM003", Fingerprint: "sha256:aaa"},
	}
	if err := baseline.Save(doc, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LLM001:sha256:aaa", "LLM001:sha256:bbb", "LLM003:sha256:aaa", "LLM003:sha256:zzz"}
	for i, e := range loaded.Entries {
		got := e.Rule + ":" + e.Fingerprint
		if got != want[i] {
			t.Errorf("entry %d: got %q want %q", i, got, want[i])
		}
	}
}

func TestLoad_VersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte("version: 999\nentries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := baseline.Load(path)
	if err == nil || !strings.Contains(err.Error(), "newer llm-lint") {
		t.Errorf("expected version-mismatch error; got %v", err)
	}
}

func TestLoad_MissingFileNilNil(t *testing.T) {
	doc, err := baseline.Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if doc != nil {
		t.Errorf("missing file: doc should be nil; got %+v", doc)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nentries: [["), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := baseline.Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error; got %v", err)
	}
}

func TestApply_MarksMatchedFindings(t *testing.T) {
	fs := []findings.Finding{
		{RuleID: "LLM001", Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"}},
		{RuleID: "LLM001", Location: findings.Location{Kind: findings.LocFile, Path: "docs/CLAUDE.md"}},
	}
	fp := baseline.Fingerprint(fs[0])
	doc := &baseline.Document{
		Version: baseline.BaselineSchemaVersion,
		Entries: []baseline.Entry{{Rule: "LLM001", Fingerprint: fp}},
	}
	stats := baseline.Apply(fs, doc)
	if stats.Matched != 1 {
		t.Errorf("matched: got %d want 1", stats.Matched)
	}
	if !fs[0].Baselined {
		t.Errorf("first finding must be baselined")
	}
	if fs[1].Baselined {
		t.Errorf("second finding (different path) must not be baselined")
	}
}

func TestApply_StaleEntries(t *testing.T) {
	fs := []findings.Finding{
		{RuleID: "LLM001", Location: findings.Location{Kind: findings.LocFile, Path: "CLAUDE.md"}},
	}
	doc := &baseline.Document{
		Version: baseline.BaselineSchemaVersion,
		Entries: []baseline.Entry{
			{Rule: "LLM001", Fingerprint: baseline.Fingerprint(fs[0])},
			{Rule: "LLM001", Fingerprint: "sha256:dead", Path: "old/path/that/no/longer/exists"},
		},
	}
	stats := baseline.Apply(fs, doc)
	if len(stats.Stale) != 1 {
		t.Errorf("stale: got %d want 1; stats=%+v", len(stats.Stale), stats)
	}
	if stats.Stale[0].Fingerprint != "sha256:dead" {
		t.Errorf("wrong stale entry: %+v", stats.Stale[0])
	}
}

func TestApply_NilDoc_NoOp(t *testing.T) {
	fs := []findings.Finding{{RuleID: "LLM001", Location: findings.Location{Path: "CLAUDE.md"}}}
	stats := baseline.Apply(fs, nil)
	if stats.Matched != 0 || stats.Total != 0 || len(stats.Stale) != 0 {
		t.Errorf("nil doc must be a no-op; got %+v", stats)
	}
	if fs[0].Baselined {
		t.Errorf("nil doc must not flip Baselined")
	}
}

func TestBuildEntries_OmitSnippets(t *testing.T) {
	fs := []findings.Finding{
		{
			RuleID:   "LLM013",
			Location: findings.Location{Kind: findings.LocFile, Path: "src/x.py", Line: 12, Snippet: "as an AI language model"},
		},
	}
	with := baseline.BuildEntries(fs, baseline.BuildOptions{IncludeSnippets: true})
	without := baseline.BuildEntries(fs, baseline.BuildOptions{IncludeSnippets: false})
	if with[0].Snippet == "" {
		t.Errorf("with snippets: expected snippet; got %+v", with[0])
	}
	if without[0].Snippet != "" {
		t.Errorf("without snippets: expected empty snippet; got %+v", without[0])
	}
}

func TestBuildEntries_SkipsUnknownRule(t *testing.T) {
	fs := []findings.Finding{
		{RuleID: "LLM001", Location: findings.Location{Path: "CLAUDE.md"}},
		{RuleID: "LLM999"},
	}
	out := baseline.BuildEntries(fs, baseline.BuildOptions{})
	if len(out) != 1 {
		t.Errorf("expected 1 entry (unknown rule skipped); got %d", len(out))
	}
}

func TestResolvePath(t *testing.T) {
	if got := baseline.ResolvePath("", "/repo"); got != "/repo/.llmlint-baseline.yaml" {
		t.Errorf("default path: got %s", got)
	}
	if got := baseline.ResolvePath("custom.yaml", "/repo"); got != "/repo/custom.yaml" {
		t.Errorf("relative path: got %s", got)
	}
	if got := baseline.ResolvePath("/abs/baseline.yaml", "/repo"); got != "/abs/baseline.yaml" {
		t.Errorf("absolute path: got %s", got)
	}
}
