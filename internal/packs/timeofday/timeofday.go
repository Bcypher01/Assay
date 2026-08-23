package timeofday

import (
	"fmt"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"google.golang.org/protobuf/proto"
)

func init() {
	primitive.MustRegister(Session{})
	primitive.MustRegister(DayOfWeek{})
	primitive.MustRegister(TimeOfDayRange{})
	primitive.MustRegister(BarOfSession{})
}

type Session struct{}

func (Session) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "session", Out: primitive.KindBool, Params: (*assayv1.SessionParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (Session) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.SessionParams)
	if !ok || x.StartMinuteUtc > 1439 || x.EndMinuteUtc > 1439 {
		return nil, fmt.Errorf("timeofday: invalid session")
	}
	return session{x.StartMinuteUtc, x.EndMinuteUtc}, nil
}

type session struct{ s, e uint32 }

func (x session) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i, t := range s.Time {
		m := uint32(t.Std().Hour()*60 + t.Std().Minute())
		if (x.s <= x.e && m >= x.s && m < x.e) || (x.s > x.e && (m >= x.s || m < x.e)) {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (session) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type DayOfWeek struct{}

func (DayOfWeek) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "day_of_week", Out: primitive.KindBool, Params: (*assayv1.DayOfWeekParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (DayOfWeek) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.DayOfWeekParams)
	if !ok {
		return nil, fmt.Errorf("timeofday: invalid days")
	}
	return dow{x.Days}, nil
}

type dow struct{ days []uint32 }

func (x dow) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i, t := range s.Time {
		for _, d := range x.days {
			if d <= 6 && uint32(t.Std().Weekday()) == d {
				b.Set(i)
			}
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (dow) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// TimeOfDayRange is the explicit custom UTC session primitive. It has the
// same half-open semantics as Session, but a separate schema for UI clarity.
type TimeOfDayRange struct{}

func (TimeOfDayRange) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "time_of_day_range", Out: primitive.KindBool, Params: (*assayv1.TimeOfDayRangeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (TimeOfDayRange) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.TimeOfDayRangeParams)
	if !ok || x.StartMinuteUtc > 1439 || x.EndMinuteUtc > 1439 {
		return nil, fmt.Errorf("timeofday: invalid time range")
	}
	return session{x.StartMinuteUtc, x.EndMinuteUtc}, nil
}

// BarOfSession returns the one-based position within the configured UTC
// session, resetting to zero outside it. A number is valid for every bar.
type BarOfSession struct{}

func (BarOfSession) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "bar_of_session", Out: primitive.KindNumber, Params: (*assayv1.BarOfSessionParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (BarOfSession) New(p proto.Message, tf core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.BarOfSessionParams)
	if !ok || x.StartMinuteUtc > 1439 || x.EndMinuteUtc > 1439 {
		return nil, fmt.Errorf("timeofday: invalid session")
	}
	return barOfSession{x.StartMinuteUtc, x.EndMinuteUtc, tf}, nil
}

type barOfSession struct {
	s, e uint32
	tf   core.TF
}

func (x barOfSession) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Ratio, s.Len())
	v := core.NewBitset(s.Len())
	count := int64(0)
	for i, t := range s.Time {
		m := uint32(t.Std().Hour()*60 + t.Std().Minute())
		inside := (x.s <= x.e && m >= x.s && m < x.e) || (x.s > x.e && (m >= x.s || m < x.e))
		if inside {
			count++
			o[i] = core.RatioFromInt(count)
		} else {
			count = 0
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
}
func (barOfSession) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
