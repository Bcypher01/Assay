package primitive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/bcypher01/assay/internal/core"
	"google.golang.org/protobuf/proto"
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
	for _, part := range [][]byte{[]byte(pack), []byte{0}, []byte(typ), []byte{0}, []byte(tf.String()), []byte{0}, []byte(snapshot), []byte{0}, b} {
		_, _ = h.Write(part)
	}
	var out ConfigHash
	copy(out[:], h.Sum(nil))
	return out, nil
}

// Evaluate constructs and evaluates p through cache. A timeframed primitive is
// passed only completed bars at its requested timeframe; an in-progress H4 bar
// is therefore unavailable while the engine is processing M5 input.
func Evaluate(ctx context.Context, p Primitive, params proto.Message, tf core.TF, s *core.Series, snapshot string, cache Cache) (Value, []Overlay, error) {
	if p == nil || s == nil {
		return Value{}, nil, ErrInvalidSpec
	}
	spec := p.Spec()
	evalSeries := s
	if spec.Timeframed {
		var err error
		evalSeries, err = CompletedSeries(s, tf)
		if err != nil {
			return Value{}, nil, err
		}
	}
	key, err := HashConfig(spec.Pack, spec.Type, params, tf, snapshot)
	if err != nil {
		return Value{}, nil, err
	}
	inst, err := p.New(params, tf)
	if err != nil {
		return Value{}, nil, err
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
		return Value{}, nil, err
	}
	return v, inst.Overlays(core.MinTime, core.MaxTime), nil
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
