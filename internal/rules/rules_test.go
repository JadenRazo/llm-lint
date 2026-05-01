package rules_test

import (
	"sync"
	"testing"

	"github.com/JadenRazo/llm-lint/internal/rules"
)

func TestSeverity_Rank(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sev  rules.Severity
		want int
	}{
		{rules.SevError, 3},
		{rules.SevWarning, 2},
		{rules.SevInfo, 1},
		{"", 0},
		{"none", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := c.sev.Rank(); got != c.want {
			t.Errorf("Severity(%q).Rank() = %d, want %d", c.sev, got, c.want)
		}
	}

	// Ordering invariant relied on by engine.ExceedsThreshold.
	if rules.SevError.Rank() <= rules.SevWarning.Rank() {
		t.Error("error must rank above warning")
	}
	if rules.SevWarning.Rank() <= rules.SevInfo.Rank() {
		t.Error("warning must rank above info")
	}
}

func TestRegister_DuplicateIDPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register() with a duplicate ID must panic")
		}
	}()
	r := rules.Rule{ID: "TEST_DUP_ID_FOR_PANIC", Title: "x", Kind: rules.KindPath}
	rules.Register(r)
	rules.Register(r) // panics
}

func TestRegister_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	// Register N unique rules concurrently. The registry uses a mutex; this
	// test runs under -race in CI and would surface a forgotten mutex.
	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "TEST_CONCURRENT_" + string(rune('A'+(i/26))) + string(rune('A'+(i%26)))
			rules.Register(rules.Rule{ID: id, Title: "x", Kind: rules.KindPath})
		}(i)
	}
	wg.Wait()
}

func TestAll_SortedByID(t *testing.T) {
	t.Parallel()
	all := rules.All()
	if len(all) == 0 {
		t.Skip("no rules registered (init() not imported by this test binary)")
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID < all[i-1].ID {
			t.Errorf("rules.All() must return sorted; %s < %s at index %d", all[i].ID, all[i-1].ID, i)
		}
	}
}

func TestGet_UnknownReturnsFalse(t *testing.T) {
	t.Parallel()
	if _, ok := rules.Get("NEVER_REGISTERED_LLM999XYZ"); ok {
		t.Error("Get() must return ok=false for unknown ID")
	}
}

func TestDefaultRegistry_ReturnsACopy(t *testing.T) {
	t.Parallel()
	a := rules.DefaultRegistry()
	b := rules.DefaultRegistry()
	if &a == &b {
		t.Error("DefaultRegistry must return a fresh map each call")
	}
	// Mutating the returned map must not affect a subsequent call.
	a["INJECTED"] = rules.Rule{ID: "INJECTED"}
	c := rules.DefaultRegistry()
	if _, ok := c["INJECTED"]; ok {
		t.Error("mutation of returned map leaked into the registry")
	}
}
