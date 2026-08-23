package instrument

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

//go:embed specs/instruments.json
var embeddedSpecs []byte

// ErrUnknownInstrument is returned when a symbol is not in the registry.
var ErrUnknownInstrument = errors.New("instrument: unknown symbol")

// Registry holds validated instrument specs, keyed by symbol.
type Registry struct {
	byKey map[string]*Spec
	keys  []string // sorted; iteration order must never depend on map order
}

// Load parses and validates specs from JSON.
func Load(data []byte) (*Registry, error) {
	var specs []*Spec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("instrument: parsing registry: %w", err)
	}
	r := &Registry{byKey: make(map[string]*Spec, len(specs))}
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("instrument: %s: %w", s.Symbol, err)
		}
		if _, dup := r.byKey[s.Symbol]; dup {
			return nil, fmt.Errorf("instrument: duplicate symbol %q", s.Symbol)
		}
		r.byKey[s.Symbol] = s
		r.keys = append(r.keys, s.Symbol)
	}
	sort.Strings(r.keys)
	return r, nil
}

// Default returns the registry embedded in the binary.
func Default() (*Registry, error) { return Load(embeddedSpecs) }

// MustDefault is Default for tests and fixtures.
func MustDefault() *Registry {
	r, err := Default()
	if err != nil {
		panic(err)
	}
	return r
}

// Get returns the spec for a symbol.
func (r *Registry) Get(symbol string) (*Spec, error) {
	s, ok := r.byKey[symbol]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownInstrument, symbol)
	}
	return s, nil
}

// Symbols returns every known symbol in sorted order.
func (r *Registry) Symbols() []string {
	out := make([]string, len(r.keys))
	copy(out, r.keys)
	return out
}

// Len returns the number of registered instruments.
func (r *Registry) Len() int { return len(r.keys) }
