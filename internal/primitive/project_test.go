package primitive

import (
	"errors"
	"testing"

	"github.com/bcypher01/assay/internal/core"
)

// projectSeries builds a bar series of n bars of tf starting at from. Only the
// timestamps matter to Project; the prices are placeholders.
func projectSeries(tf core.TF, from core.Time, n int) *core.Series {
	s := core.NewSeries("TEST", tf, n)
	for i := 0; i < n; i++ {
		t := from + core.Time(int64(i)*tf.Millis())
		s.Append(t, 100, 101, 99, 100, 1)
	}
	return s
}

// levels returns a KindLevel value holding one distinct price per bar so that
// a projected index is identifiable from its value alone.
func levels(n int) Value {
	v := Value{Kind: KindLevel, Valid: core.NewBitset(n), Level: make([]core.Price, n)}
	for i := 0; i < n; i++ {
		v.Level[i] = core.Price(1000 + i)
		v.Valid.Set(i)
	}
	return v
}

func TestProjectCoincidentClose(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	coarse := projectSeries(core.TFH4, base, 3) // closes at 04:00, 08:00, 12:00
	fine := projectSeries(core.TFM15, base, 48) // 00:00 through 11:45
	got, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.Len() != fine.Len() {
		t.Fatalf("projected length = %d, want %d", got.Len(), fine.Len())
	}
	// Bar 15 opens 03:45 and closes 04:00, the same instant the first H4 bar
	// closes. That close is fully determined by data at or before 04:00, so it
	// is visible here. Delaying it to bar 16 is the one-bar lag this join
	// exists to avoid.
	if got.Valid.Get(14) {
		t.Fatal("the bar closing at 03:45 must not see an H4 bar that closes at 04:00")
	}
	if !got.Valid.Get(15) || got.Level[15] != 1000 {
		t.Fatalf("coincident close: valid=%v level=%d, want the first coarse bar", got.Valid.Get(15), got.Level[15])
	}
	// The value is then held across the fine bars inside the next coarse bar.
	for i := 15; i < 31; i++ {
		if !got.Valid.Get(i) || got.Level[i] != 1000 {
			t.Fatalf("bar %d: valid=%v level=%d, want the first coarse bar held", i, got.Valid.Get(i), got.Level[i])
		}
	}
	if !got.Valid.Get(31) || got.Level[31] != 1001 {
		t.Fatalf("bar 31 closes at 08:00: level=%d, want the second coarse bar", got.Level[31])
	}
}

func TestProjectWarmupIsInvalidNotZero(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	coarse := projectSeries(core.TFH4, base, 2)
	fine := projectSeries(core.TFM15, base, 32)
	got, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	for i := 0; i < 15; i++ {
		if got.Valid.Get(i) {
			t.Fatalf("bar %d is before the first coarse close and must be invalid", i)
		}
	}
	if got.Valid.Count() != fine.Len()-15 {
		t.Fatalf("valid count = %d, want %d", got.Valid.Count(), fine.Len()-15)
	}
}

func TestProjectStaleness(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	// One H4 bar, then a gap: nothing closes again for the rest of the day.
	coarse := projectSeries(core.TFH4, base, 1)
	fine := projectSeries(core.TFM15, base, 48)
	got, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// The default bound is one coarse period plus one entry bar: the coarse bar
	// closes at 04:00, so the bar closing at 08:15 is the last one it governs.
	last := 32 // opens 08:00, closes 08:15
	if !got.Valid.Get(last) {
		t.Fatal("a coarse value must survive its own period plus one entry bar")
	}
	if got.Valid.Get(last + 1) {
		t.Fatal("a coarse value past the staleness bound must report invalid, not persist")
	}
	held, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{Hold: true})
	if err != nil {
		t.Fatalf("Project held: %v", err)
	}
	if !held.Valid.Get(fine.Len() - 1) {
		t.Fatal("Hold must keep the value usable indefinitely")
	}
	bounded, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{MaxAge: core.Hour})
	if err != nil {
		t.Fatalf("Project bounded: %v", err)
	}
	if !bounded.Valid.Get(19) || bounded.Valid.Get(24) { // 05:00 close, then 06:00
		t.Fatalf("an explicit MaxAge was not honoured: %v %v", bounded.Valid.Get(19), bounded.Valid.Get(24))
	}
}

func TestProjectPreservesSourceInvalidity(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	coarse := projectSeries(core.TFH4, base, 3)
	fine := projectSeries(core.TFM15, base, 48)
	v := levels(coarse.Len())
	v.Valid.Clear(1)
	got, err := Project(v, coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	for i := 31; i < 47; i++ {
		if got.Valid.Get(i) {
			t.Fatalf("bar %d maps to an invalid coarse bar and must stay invalid", i)
		}
	}
}

func TestProjectKinds(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	coarse := projectSeries(core.TFH4, base, 2)
	fine := projectSeries(core.TFM15, base, 32)
	valid := func(n int) *core.Bitset {
		b := core.NewBitset(n)
		for i := 0; i < n; i++ {
			b.Set(i)
		}
		return b
	}
	boolValue := Value{Kind: KindBool, Valid: valid(2), Bool: core.NewBitset(2)}
	boolValue.Bool.Set(0)
	got, err := Project(boolValue, coarse, fine, StalePolicy{})
	if err != nil || !got.Bool.Get(15) || got.Bool.Get(31) {
		t.Fatalf("bool projection: %v %v %v", err, got.Bool.Get(15), got.Bool.Get(31))
	}
	zone := Value{Kind: KindZone, Valid: valid(2), Zone: []Zone{{Low: 10, High: 20}, {Low: 30, High: 40}}}
	got, err = Project(zone, coarse, fine, StalePolicy{})
	if err != nil || got.Zone[15].High != 20 || got.Zone[31].Low != 30 {
		t.Fatalf("zone projection: %v %+v %+v", err, got.Zone[15], got.Zone[31])
	}
	number := Value{Kind: KindNumber, Valid: valid(2), Number: []core.Ratio{7, 9}}
	got, err = Project(number, coarse, fine, StalePolicy{})
	if err != nil || got.Number[15] != 7 || got.Number[31] != 9 {
		t.Fatalf("number projection: %v %d %d", err, got.Number[15], got.Number[31])
	}
	state := Value{Kind: KindState, Valid: valid(2), State: []uint32{1, 2}}
	got, err = Project(state, coarse, fine, StalePolicy{})
	if err != nil || got.State[15] != 1 || got.State[31] != 2 {
		t.Fatalf("state projection: %v %d %d", err, got.State[15], got.State[31])
	}
}

func TestProjectLadder(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	fine := projectSeries(core.TFM5, base, 24*12*2)
	for _, tf := range []core.TF{core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1} {
		coarse := projectSeries(tf, base, int(2*core.Day/core.TFDuration(tf)))
		got, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
		if err != nil {
			t.Fatalf("%s: %v", tf, err)
		}
		if got.Len() != fine.Len() || got.Valid.Len() != fine.Len() {
			t.Fatalf("%s: length %d, want %d", tf, got.Len(), fine.Len())
		}
		// The first entry bar that can see a coarse bar is the one closing at
		// the same instant as the first coarse close.
		firstValid, ok := got.Valid.Next(0)
		want := int(core.TFDuration(tf)/core.TFDuration(core.TFM5)) - 1
		if !ok || firstValid != want {
			t.Fatalf("%s: first valid bar %d (found %v), want %d", tf, firstValid, ok, want)
		}
	}
}

func TestProjectRefusesFinerSource(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	fine := projectSeries(core.TFM15, base, 8)
	coarse := projectSeries(core.TFH4, base, 2)
	// A node finer than the entry series cannot be aggregated from it and must
	// be refused rather than degraded.
	if _, err := Project(levels(fine.Len()), fine, coarse, StalePolicy{}); !errors.Is(err, ErrTimeframe) {
		t.Fatalf("projecting a finer series onto a coarser one = %v, want ErrTimeframe", err)
	}
	if _, err := Project(levels(coarse.Len()), nil, fine, StalePolicy{}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("nil source = %v, want ErrInvalidSpec", err)
	}
	if _, err := Project(levels(coarse.Len()+1), coarse, fine, StalePolicy{}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("mismatched source length = %v, want ErrInvalidValue", err)
	}
}

func TestProjectIsDeterministic(t *testing.T) {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	coarse := projectSeries(core.TFH4, base, 6)
	fine := projectSeries(core.TFM15, base, 96)
	first, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	second, err := Project(levels(coarse.Len()), coarse, fine, StalePolicy{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	for i := 0; i < fine.Len(); i++ {
		if first.Valid.Get(i) != second.Valid.Get(i) || first.Level[i] != second.Level[i] {
			t.Fatalf("bar %d differs between two identical projections", i)
		}
	}
}

func BenchmarkProject(b *testing.B) {
	base := core.MustParseTime("2020-01-01T00:00:00Z")
	fine := projectSeries(core.TFM15, base, 350_000)
	coarse := projectSeries(core.TFH4, base, 350_000/16)
	v := levels(coarse.Len())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Project(v, coarse, fine, StalePolicy{}); err != nil {
			b.Fatal(err)
		}
	}
}
