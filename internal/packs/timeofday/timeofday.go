package timeofday

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func init() {
	primitive.MustRegister(Session{})
	primitive.MustRegister(SessionHigh{})
	primitive.MustRegister(SessionLow{})
	primitive.MustRegister(DayOfWeek{})
	primitive.MustRegister(TimeOfDayRange{})
	primitive.MustRegister(BarOfSession{})
}

type Session struct{}

func (Session) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "session", Out: primitive.KindBool, Params: (*assayv1.SessionParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (Session) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newSession(p)
}

type session struct{ s, e uint32 }

func (x session) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i, t := range s.Time {
		if x.contains(t) {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (session) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

func newSession(p proto.Message) (session, error) {
	x, ok := p.(*assayv1.SessionParams)
	if !ok || x.StartMinuteUtc > 1439 || x.EndMinuteUtc > 1439 || x.StartMinuteUtc == x.EndMinuteUtc {
		return session{}, fmt.Errorf("timeofday: invalid session")
	}
	return session{x.StartMinuteUtc, x.EndMinuteUtc}, nil
}

func (x session) contains(t core.Time) bool {
	m := uint32(t.Std().Hour()*60 + t.Std().Minute())
	return (x.s < x.e && m >= x.s && m < x.e) || (x.s > x.e && (m >= x.s || m < x.e))
}

func (x session) key(t core.Time) int64 {
	std := t.Std()
	day := std.Unix() / (24 * 60 * 60)
	m := uint32(std.Hour()*60 + std.Minute())
	if x.s > x.e && m < x.e {
		day--
	}
	return day
}

// SessionHigh and SessionLow expose the completed configured-session range.
// Their values are invalid while the next session is forming, so a breakout
// can only reference a stable range known at the close of that session.
type SessionHigh struct{}
type SessionLow struct{}

func (SessionHigh) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "session_high", Out: primitive.KindLevel, Params: (*assayv1.SessionParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (SessionLow) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "session_low", Out: primitive.KindLevel, Params: (*assayv1.SessionParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (SessionHigh) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	s, err := newSession(p)
	if err != nil {
		return nil, err
	}
	return sessionRange{session: s, high: true}, nil
}
func (SessionLow) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	s, err := newSession(p)
	if err != nil {
		return nil, err
	}
	return sessionRange{session: s}, nil
}

type sessionRange struct {
	session session
	high    bool
}

func (x sessionRange) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	levels := make([]core.Price, s.Len())
	valid := core.NewBitset(s.Len())
	var current, completed core.Price
	active, haveCurrent, haveCompleted := false, false, false
	var currentKey int64
	for i, t := range s.Time {
		if x.session.contains(t) {
			candidate := s.Low[i]
			if x.high {
				candidate = s.High[i]
			}
			key := x.session.key(t)
			switch {
			case !active || key != currentKey:
				if active && haveCurrent {
					completed, haveCompleted = current, true
				}
				current, currentKey, active, haveCurrent = candidate, key, true, true
			case x.high:
				current = core.Max(current, candidate)
			default:
				current = core.Min(current, candidate)
			}
			continue
		}
		if active && haveCurrent {
			completed, haveCompleted = current, true
			active, haveCurrent = false, false
		}
		if haveCompleted {
			levels[i] = completed
			valid.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: levels, Valid: valid}, nil
}
func (sessionRange) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

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
