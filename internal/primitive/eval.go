package primitive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/bcypher01/assay/internal/core"
)

// ConfigHash identifies one pure detection pass.
type ConfigHash [sha256.Size]byte

func (h ConfigHash) String() string { return hex.EncodeToString(h[:]) }

// HashConfig is stable across construction order and includes the pinned data
// snapshot, preventing one library version from reusing another's output.
func HashConfig(pack, typ string, params proto.Message, tf core.TF, snapshot string) (ConfigHash, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(params)
	if err != nil {
		return ConfigHash{}, fmt.Errorf("primitive: marshal params: %w", err)
	}
	h := sha256.New()
	for _, part := range [][]byte{[]byte(pack), {0}, []byte(typ), {0}, []byte(tf.String()), {0}, []byte(snapshot), {0}, b} {
		_, _ = h.Write(part)
	}
	var out ConfigHash
	copy(out[:], h.Sum(nil))
	return out, nil
}

// Evaluate constructs and evaluates p through cache. A timeframed primitive is
// passed only completed bars at its requested timeframe; an in-progress H4 bar
// is therefore unavailable while the engine is processing M5 input.
//
// The returned Value is indexed by the primitive's own timeframe, which is not
// the entry series' index space when the two differ. Callers that need it
// aligned to the entry series want EvaluateAligned.
func Evaluate(ctx context.Context, p Primitive, params proto.Message, tf core.TF, s *core.Series, snapshot string, cache Cache) (Value, []Overlay, error) {
	v, _, _, overlays, err := evaluate(ctx, p, params, tf, s, snapshot, cache)
	return v, overlays, err
}

// EvaluateAligned evaluates p and projects its output onto s's index space,
// so that a node on a coarser timeframe than the entry series can be read at
// every entry bar. Detection still happens on completed coarse bars only; the
// projection adds no information, it only makes a closed coarse result visible
// to the fine bars that follow it.
//
// The projection is memoised alongside the detection pass it derives from, so
// a sweep that reuses one coarse Value across many configurations pays for the
// join once.
func EvaluateAligned(ctx context.Context, p Primitive, params proto.Message, tf core.TF, s *core.Series, snapshot string, cache Cache, policy StalePolicy) (Value, []Overlay, error) {
	v, key, evalSeries, overlays, err := evaluate(ctx, p, params, tf, s, snapshot, cache)
	if err != nil {
		return Value{}, nil, err
	}
	if evalSeries == s {
		return v, overlays, nil
	}
	project := func() (Value, error) { return Project(v, evalSeries, s, policy) }
	if cache == nil {
		v, err = project()
	} else {
		v, err = cache.GetOrEval(projectionKey(key, s.TF, policy), project)
	}
	if err != nil {
		return Value{}, nil, err
	}
	return v, overlays, nil
}

// projectionKey derives the cache key of a projected Value from the key of the
// detection pass it was projected from. Two projections of the same coarse
// output differ only by target timeframe and staleness policy.
func projectionKey(base ConfigHash, to core.TF, policy StalePolicy) ConfigHash {
	h := sha256.New()
	_, _ = h.Write(base[:])
	hold := []byte{0}
	if policy.Hold {
		hold[0] = 1
	}
	var age [8]byte
	binary.BigEndian.PutUint64(age[:], uint64(policy.MaxAge.Millis()))
	for _, part := range [][]byte{[]byte("project"), {0}, []byte(to.String()), {0}, age[:], hold} {
		_, _ = h.Write(part)
	}
	var out ConfigHash
	copy(out[:], h.Sum(nil))
	return out
}

// evaluate runs the detection pass and reports the series it ran against, so
// that a caller wanting alignment does not aggregate the same bars twice.
func evaluate(ctx context.Context, p Primitive, params proto.Message, tf core.TF, s *core.Series, snapshot string, cache Cache) (Value, ConfigHash, *core.Series, []Overlay, error) {
	if p == nil || s == nil {
		return Value{}, ConfigHash{}, nil, nil, ErrInvalidSpec
	}
	spec := p.Spec()
	evalSeries := s
	if spec.Timeframed {
		var err error
		evalSeries, err = CompletedSeries(s, tf)
		if err != nil {
			return Value{}, ConfigHash{}, nil, nil, err
		}
	}
	key, err := HashConfig(spec.Pack, spec.Type, params, tf, snapshot)
	if err != nil {
		return Value{}, ConfigHash{}, nil, nil, err
	}
	inst, err := p.New(params, tf)
	if err != nil {
		return Value{}, ConfigHash{}, nil, nil, err
	}
	eval := func() (Value, error) {
		v, e := inst.Eval(&EvalCtx{Snapshot: snapshot}, evalSeries)
		if e != nil {
			return Value{}, e
		}
		if e = v.Validate(evalSeries.Len()); e != nil {
			return Value{}, e
		}
		return v, nil
	}
	var v Value
	if cache == nil {
		v, err = eval()
	} else {
		v, err = cache.GetOrEval(key, eval)
	}
	if err != nil {
		return Value{}, ConfigHash{}, nil, nil, err
	}
	return v, key, evalSeries, inst.Overlays(core.MinTime, core.MaxTime), nil
}

// CompletedSeries aggregates s into tf and excludes its unfinished final bar.
// It returns s directly at the same timeframe. The returned series is a new
// compact detection input and never mutates the source series.
func CompletedSeries(s *core.Series, tf core.TF) (*core.Series, error) {
	if tf == s.TF {
		return s, nil
	}
	if !tf.Coarser(s.TF) || s.TF == core.TFTick {
		return nil, ErrTimeframe
	}
	out := core.NewSeries(s.Symbol, tf, s.Len()/max(1, int(tf.Millis()/s.TF.Millis()))+1)
	for from := 0; from < s.Len(); {
		start := tf.AlignDown(s.Time[from])
		to := from + 1
		for to < s.Len() && tf.AlignDown(s.Time[to]) == start {
			to++
		}
		// A partial leading or trailing aggregate must never become visible.
		if s.Time[from] == start && s.TF.CloseOf(s.Time[to-1]) >= tf.CloseOf(start) {
			hi, lo := s.High[from], s.Low[from]
			var volume int64
			for i := from; i < to; i++ {
				hi = core.Max(hi, s.High[i])
				lo = core.Min(lo, s.Low[i])
				volume += s.Volume[i]
			}
			out.Append(start, s.Open[from], hi, lo, s.Close[to-1], volume)
		}
		from = to
	}
	return out, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
