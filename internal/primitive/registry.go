package primitive

import (
	"fmt"
	"sort"
	"sync"
)

// Registry owns primitive registration. A Registry is preferable to package
// globals in tests and embedded applications; DefaultRegistry serves packs.
type Registry struct {
	mu    sync.RWMutex
	byKey map[string]Primitive
}

func NewRegistry() *Registry { return &Registry{byKey: make(map[string]Primitive)} }

var DefaultRegistry = NewRegistry()

func registryKey(pack, typ string) string { return pack + "\x00" + typ }

// Register adds p unless its (pack,type) pair is already occupied.
func (r *Registry) Register(p Primitive) error {
	if p == nil {
		return ErrInvalidSpec
	}
	s := p.Spec()
	if s.Pack == "" || s.Type == "" || s.Params == nil || s.Out > KindState {
		return ErrInvalidSpec
	}
	k := registryKey(s.Pack, s.Type)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byKey[k]; exists {
		return fmt.Errorf("%w: %s/%s", ErrDuplicate, s.Pack, s.Type)
	}
	r.byKey[k] = p
	return nil
}

// MustRegister is intended for pack init functions.
func MustRegister(p Primitive) {
	if err := DefaultRegistry.Register(p); err != nil {
		panic(err)
	}
}

func (r *Registry) Get(pack, typ string) (Primitive, error) {
	r.mu.RLock()
	p, ok := r.byKey[registryKey(pack, typ)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, pack, typ)
	}
	return p, nil
}

// Specs returns a stable, UI-friendly ordered registry listing.
func (r *Registry) Specs() []Spec {
	r.mu.RLock()
	out := make([]Spec, 0, len(r.byKey))
	for _, p := range r.byKey {
		out = append(out, p.Spec())
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pack == out[j].Pack {
			return out[i].Type < out[j].Type
		}
		return out[i].Pack < out[j].Pack
	})
	return out
}
