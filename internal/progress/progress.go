// Package progress renders a transient status line to stderr while a long-
// running scan is in flight. It auto-disables when the writer isn't a TTY
// (so SARIF/JSON output to a redirected stderr stays clean) and exposes
// lock-free counters that scanners can update from any goroutine.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const renderInterval = 100 * time.Millisecond

// Reporter is safe for concurrent use. When disabled (non-TTY writer or
// New called with enabled=false), every method is a cheap no-op.
type Reporter struct {
	w       io.Writer
	enabled bool

	// Counters touched from worker goroutines.
	files   atomic.Int64 // files scanned in current phase
	scanned atomic.Int64 // git commits scanned
	total   atomic.Int64 // git commit total (0 = unknown)

	phase     atomic.Pointer[string]
	startedAt atomic.Pointer[time.Time]

	tickerMu sync.Mutex
	stopCh   chan struct{}
	doneCh   chan struct{}

	writeMu sync.Mutex
	lastLen int
}

// New returns a Reporter. If w is not a *os.File pointing at a terminal,
// or if enabled is false, the Reporter operates in no-op mode.
func New(w io.Writer, enabled bool) *Reporter {
	return &Reporter{
		w:       w,
		enabled: enabled && isTTY(w),
	}
}

// Enabled reports whether output will actually be rendered.
func (r *Reporter) Enabled() bool { return r != nil && r.enabled }

// Phase resets counters and starts (or continues) the render goroutine
// labelled with the new phase. Valid phase names: "files", "git".
func (r *Reporter) Phase(name string) {
	if !r.Enabled() {
		return
	}
	now := time.Now()
	r.phase.Store(&name)
	r.startedAt.Store(&now)
	r.files.Store(0)
	r.scanned.Store(0)
	r.total.Store(0)
	r.ensureTicker()
}

// IncFile bumps the file counter for the "files" phase. Cheap, lock-free.
func (r *Reporter) IncFile() {
	if !r.Enabled() {
		return
	}
	r.files.Add(1)
}

// SetCommits updates progress for the "git" phase. total may be 0 when
// the eventual count is unknown.
func (r *Reporter) SetCommits(scanned, total int) {
	if !r.Enabled() {
		return
	}
	r.scanned.Store(int64(scanned))
	r.total.Store(int64(total))
}

// Done stops the render goroutine and clears the status line. Safe to
// call multiple times.
func (r *Reporter) Done() {
	if !r.Enabled() {
		return
	}
	r.tickerMu.Lock()
	if r.stopCh != nil {
		close(r.stopCh)
		done := r.doneCh
		r.stopCh = nil
		r.doneCh = nil
		r.tickerMu.Unlock()
		<-done
	} else {
		r.tickerMu.Unlock()
	}
	r.clearLine()
}

func (r *Reporter) ensureTicker() {
	r.tickerMu.Lock()
	defer r.tickerMu.Unlock()
	if r.stopCh != nil {
		return
	}
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	go r.loop(r.stopCh, r.doneCh)
}

func (r *Reporter) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	t := time.NewTicker(renderInterval)
	defer t.Stop()
	r.render() // immediate first frame
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			r.render()
		}
	}
}

func (r *Reporter) render() {
	phasePtr := r.phase.Load()
	if phasePtr == nil {
		return
	}
	phase := *phasePtr

	startedPtr := r.startedAt.Load()
	var elapsed time.Duration
	if startedPtr != nil {
		elapsed = time.Since(*startedPtr)
	}

	var line string
	switch phase {
	case "files":
		files := r.files.Load()
		rate := float64(files) / max(elapsed.Seconds(), 0.001)
		line = fmt.Sprintf("> scanning files... %s (%s/s)",
			humanInt(files), humanInt(int64(rate)))
	case "git":
		scanned := r.scanned.Load()
		total := r.total.Load()
		if total > 0 {
			pct := float64(scanned) / float64(total)
			bar := renderBar(pct, 24)
			eta := ""
			if scanned > 0 && elapsed > 200*time.Millisecond {
				rate := float64(scanned) / elapsed.Seconds()
				if rate > 0 && scanned < total {
					remaining := time.Duration(float64(total-scanned)/rate * float64(time.Second))
					eta = " ETA " + humanDur(remaining)
				}
			}
			line = fmt.Sprintf("> scanning git history %s %d/%d commits%s",
				bar, scanned, total, eta)
		} else {
			line = fmt.Sprintf("> scanning git history... %d commits", scanned)
		}
	default:
		return
	}
	r.write(line)
}

func (r *Reporter) write(line string) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	// Pad with spaces to overwrite any longer leftover from a previous frame,
	// then carriage-return the cursor back to column 0 for the next frame.
	pad := r.lastLen - len(line)
	if pad < 0 {
		pad = 0
	}
	fmt.Fprintf(r.w, "\r%s%s", line, strings.Repeat(" ", pad))
	r.lastLen = len(line)
}

func (r *Reporter) clearLine() {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.lastLen == 0 {
		return
	}
	fmt.Fprintf(r.w, "\r%s\r", strings.Repeat(" ", r.lastLen))
	r.lastLen = 0
}

func renderBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct*float64(width) + 0.5)
	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", width-filled) + "]"
}

// humanInt formats counts with thousands grouping (1234567 -> "1,234,567").
func humanInt(n int64) string {
	if n < 0 {
		return "-" + humanInt(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	first := len(s) % 3
	if first > 0 {
		out = append(out, s[:first]...)
		if len(s) > first {
			out = append(out, ',')
		}
	}
	for i := first; i < len(s); i += 3 {
		out = append(out, s[i:i+3]...)
		if i+3 < len(s) {
			out = append(out, ',')
		}
	}
	return string(out)
}

func humanDur(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm%ds", m, s)
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
