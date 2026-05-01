package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

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
}
