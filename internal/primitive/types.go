package primitive

import (
	"errors"

	"github.com/Bcypher01/Assay/internal/core"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	ErrDuplicate    = errors.New("primitive: duplicate registration")
	ErrNotFound     = errors.New("primitive: not found")
	ErrInvalidSpec  = errors.New("primitive: invalid spec")
	ErrInvalidValue = errors.New("primitive: invalid value")
	ErrTimeframe    = errors.New("primitive: incompatible timeframe")
)

// Kind is the semantic shape a primitive emits.
type Kind uint8

const (
	KindBool Kind = iota
	KindLevel
	KindZone
	KindNumber
	KindState
)

// Zone is an inclusive price interval. The lifecycle fields let zone-producing
// primitives report when an area was created, expired, or mitigated without
// requiring consumers to infer that state from an overlay.
type Zone struct {
	Low, High   core.Price
	From, To    core.Time
	Side        core.Side
	Mitigated   bool
	MitigatedAt core.Time
}

// Value is a whole-series primitive output. Only the member matching Kind is
// populated. Valid, when non-nil, marks bars for which the output is usable.
type Value struct {
	Kind   Kind
	Valid  *core.Bitset
	Bool   *core.Bitset
	Level  []core.Price
	Zone   []Zone
	Number []core.Ratio
	State  []uint32
}

// Len reports the number of values held by the active representation.
func (v Value) Len() int {
	switch v.Kind {
	case KindBool:
		if v.Bool != nil {
			return v.Bool.Len()
		}
	case KindLevel:
		return len(v.Level)
	case KindZone:
		return len(v.Zone)
	case KindNumber:
		return len(v.Number)
	case KindState:
		return len(v.State)
	}
	return 0
}

// Validate checks that the output has one value per source bar.
func (v Value) Validate(n int) error {
	if v.Len() != n || (v.Valid != nil && v.Valid.Len() != n) {
		return ErrInvalidValue
	}
	if v.Kind == KindBool && v.Bool == nil {
		return ErrInvalidValue
	}
	return nil
}

// OverlayKind selects a chart annotation. Rendering belongs to the standalone
// UI; primitives only emit portable drawing data.
type OverlayKind uint8

const (
	OverlayMarker OverlayKind = iota
	OverlayLevelLine
	OverlayZoneBox
	OverlayLabel
)

// Overlay is one rendering instruction, clipped by Instance.Overlays.
type Overlay struct {
	Kind          OverlayKind
	From, To      core.Time
	Price, Price2 core.Price
	Text          string
}

// Spec is immutable primitive metadata used by the registry and UI schema.
type Spec struct {
	Pack, Type string
	Out        Kind
	Params     protoreflect.MessageDescriptor
	Timeframed bool
}

// Primitive is a factory. It must retain no state from an evaluation.
type Primitive interface {
	Spec() Spec
	New(params proto.Message, tf core.TF) (Instance, error)
}

// Instance holds one evaluation's state. Eval is called once per detection
// pass, never from the simulator's inner loop.
type Instance interface {
	Eval(ctx *EvalCtx, s *core.Series) (Value, error)
	Overlays(from, to core.Time) []Overlay
}

// EvalCtx pins the data snapshot used by the cache key.
type EvalCtx struct{ Snapshot string }

// Cache memoises immutable Values by their complete detection configuration.
type Cache interface {
	GetOrEval(key ConfigHash, f func() (Value, error)) (Value, error)
	Stats() (hits, misses int)
}
