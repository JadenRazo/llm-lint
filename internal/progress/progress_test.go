package progress

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a goroutine-safe wrapper around bytes.Buffer so the render
// loop and the test goroutine can read/write without racing.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// newEnabledForTest bypasses the isTTY check so the rendering path can be
// exercised with a bytes-backed writer.
func newEnabledForTest(w *syncBuf) *Reporter {
	return &Reporter{w: w, enabled: true}
}

// non-TTY writer should make every method a no-op (no output, no goroutine).
func TestDisabledWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, true)
	if r.Enabled() {
		t.Fatal("expected disabled for non-file writer")
	}
	r.Phase("files")
	for i := 0; i < 100; i++ {
		r.IncFile()
	}
	r.SetCommits(10, 100)
	r.Done()
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestExplicitDisable(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, false)
	if r.Enabled() {
		t.Fatal("expected disabled when enabled=false")
	}
	r.Phase("files")
	r.IncFile()
	r.Done()
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestHumanInt(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		1:        "1",
		999:      "999",
		1000:     "1,000",
		12345:    "12,345",
		1234567:  "1,234,567",
		-1234567: "-1,234,567",
	}
	for in, want := range cases {
		if got := humanInt(in); got != want {
			t.Errorf("humanInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanDur(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{50 * time.Millisecond, "<1s"},
		{1 * time.Second, "1s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{125 * time.Second, "2m5s"},
	}
	for _, c := range cases {
		if got := humanDur(c.in); got != c.want {
			t.Errorf("humanDur(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderBar(t *testing.T) {
	if got := renderBar(0, 10); got != "[----------]" {
		t.Errorf("0%%: got %q", got)
	}
	if got := renderBar(1, 10); got != "[==========]" {
		t.Errorf("100%%: got %q", got)
	}
	if got := renderBar(0.5, 10); !strings.Contains(got, "=====") {
		t.Errorf("50%%: got %q (expected ~5 = chars)", got)
	}
	// Out-of-range inputs clamp instead of producing negative repeat counts.
	if got := renderBar(-0.5, 10); got != "[----------]" {
		t.Errorf("negative pct: got %q", got)
	}
	if got := renderBar(2.0, 10); got != "[==========]" {
		t.Errorf("over-100%% pct: got %q", got)
	}
}

func TestEnabledFilesPhase(t *testing.T) {
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("files")
	for i := 0; i < 250; i++ {
		r.IncFile()
	}
	// One ticker interval is 100ms; wait long enough for at least one render
	// after the initial frame so the file count is observed.
	time.Sleep(150 * time.Millisecond)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "scanning files") {
		t.Fatalf("expected 'scanning files' in output, got %q", out)
	}
	if !strings.Contains(out, "250") {
		t.Fatalf("expected file count 250 in output, got %q", out)
	}
}

func TestEnabledGitPhaseKnownTotal(t *testing.T) {
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("git")
	r.SetCommits(50, 100)
	// Allow elapsed > 200ms so the ETA branch is exercised.
	time.Sleep(250 * time.Millisecond)
	r.SetCommits(60, 100)
	time.Sleep(150 * time.Millisecond)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "scanning git history") {
		t.Fatalf("expected 'scanning git history' in output, got %q", out)
	}
	if !strings.Contains(out, "/100 commits") {
		t.Fatalf("expected '/100 commits' in output, got %q", out)
	}
	if !strings.Contains(out, "[") || !strings.Contains(out, "=") {
		t.Fatalf("expected progress bar, got %q", out)
	}
}

func TestEnabledGitPhaseUnknownTotal(t *testing.T) {
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("git")
	r.SetCommits(7, 0)
	time.Sleep(150 * time.Millisecond)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "scanning git history... 7 commits") {
		t.Fatalf("expected unknown-total format, got %q", out)
	}
}

func TestPhaseUnknownNameRendersNothing(t *testing.T) {
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("nonsense")
	time.Sleep(150 * time.Millisecond)
	r.Done()

	if buf.String() != "" {
		t.Fatalf("expected no output for unknown phase, got %q", buf.String())
	}
}

func TestDoneIdempotent(t *testing.T) {
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("files")
	r.IncFile()
	time.Sleep(120 * time.Millisecond)
	r.Done()
	r.Done() // second call must not panic or block.
}

func TestDoneBeforePhase(t *testing.T) {
	// Calling Done before Phase should not block waiting on a ticker that
	// was never started.
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Done()
	if buf.String() != "" {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestRenderBeforePhaseNoOp(t *testing.T) {
	// Direct render() call with no phase set returns early — guards against
	// a Done()→render() race producing a nil-pointer deref.
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.render()
	if buf.String() != "" {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestPhaseTransitionShortensLine(t *testing.T) {
	// A long "files" line followed by a shorter "git" line must overwrite
	// the leftover characters from the previous frame.
	buf := &syncBuf{}
	r := newEnabledForTest(buf)
	r.Phase("files")
	for i := 0; i < 999_999; i++ {
		r.IncFile()
	}
	time.Sleep(150 * time.Millisecond)
	r.Phase("git")
	r.SetCommits(1, 5)
	time.Sleep(150 * time.Millisecond)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "scanning git history") {
		t.Fatalf("expected git phase output, got %q", out)
	}
}

func TestIsTTYWithDevNull(t *testing.T) {
	// /dev/null is a character device on Linux/macOS, so isTTY returns true
	// for it. New() therefore enables the reporter.
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer func() { _ = f.Close() }()

	if !isTTY(f) {
		t.Skip("os.DevNull is not a character device on this platform")
	}

	r := New(f, true)
	if !r.Enabled() {
		t.Fatal("expected enabled reporter for char-device writer")
	}
	r.Phase("files")
	r.IncFile()
	time.Sleep(120 * time.Millisecond)
	r.Done()
}

func TestIsTTYRejectsBuffer(t *testing.T) {
	if isTTY(&bytes.Buffer{}) {
		t.Fatal("isTTY should return false for non-*os.File writer")
	}
}
