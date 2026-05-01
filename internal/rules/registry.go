package rules

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu       sync.Mutex
	registry = map[string]Rule{}
)

func Register(r Rule) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[r.ID]; dup {
		panic(fmt.Sprintf("duplicate rule id: %s", r.ID))
	}
	registry[r.ID] = r
}

func DefaultRegistry() map[string]Rule {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]Rule, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func All() []Rule {
	reg := DefaultRegistry()
	out := make([]Rule, 0, len(reg))
	for _, r := range reg {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Get(id string) (Rule, bool) {
	mu.Lock()
	defer mu.Unlock()
	r, ok := registry[id]
	return r, ok
}
