package core

import "testing"

func buildSeries(n int) *Series {
	s := NewSeries("TEST", TFM15, n)
	base := MustParseTime("2026-01-01T00:00:00Z")
	for i := 0; i < n; i++ {
		t := base + Time(int64(i)*TFM15.Millis())
		p := Price(1000 + i)
		s.Append(t, p, p+5, p-5, p+1, int64(i))
	}
	return s
}

func TestSeriesValidate(t *testing.T) {
	s := buildSeries(10)
	if err := s.Validate(); err != nil {
		t.Fatalf("valid series rejected: %v", err)
	}

	ragged := buildSeries(5)
	ragged.Close = ragged.Close[:4]
	if err := ragged.Validate(); err != ErrRaggedSeries {
		t.Fatalf("expected ErrRaggedSeries, got %v", err)
	}

	unaligned := NewSeries("TEST", TFM15, 1)
	unaligned.Append(MustParseTime("2026-01-01T00:07:00Z"), 1, 1, 1, 1, 0)
	if err := unaligned.Validate(); err == nil {
		t.Fatal("an unaligned timestamp must be rejected")
	}

	backwards := NewSeries("TEST", TFM15, 2)
	base := MustParseTime("2026-01-01T00:15:00Z")
	backwards.Append(base, 1, 1, 1, 1, 0)
	backwards.Append(base-Time(TFM15.Millis()), 1, 1, 1, 1, 0)
	if err := backwards.Validate(); err == nil {
		t.Fatal("non-increasing timestamps must be rejected")
	}

	inverted := NewSeries("TEST", TFM15, 1)
	inverted.Append(base, 10, 5, 20, 10, 0)
	if err := inverted.Validate(); err == nil {
		t.Fatal("high below low must be rejected")
	}

	outside := NewSeries("TEST", TFM15, 1)
	outside.Append(base, 99, 20, 10, 15, 0)
	if err := outside.Validate(); err == nil {
		t.Fatal("open outside the range must be rejected")
	}
}

func TestSeriesIndex(t *testing.T) {
	s := buildSeries(100)
	target := s.Time[42]

	if i, ok := s.IndexAt(target); !ok || i != 42 {
		t.Fatalf("IndexAt = (%d, %v)", i, ok)
	}
	if _, ok := s.IndexAt(target + 1); ok {
		t.Fatal("IndexAt must require an exact match")
	}
	if i, ok := s.IndexBefore(target + 1); !ok || i != 42 {
		t.Fatalf("IndexBefore = (%d, %v)", i, ok)
	}
	if _, ok := s.IndexBefore(s.Time[0] - 1); ok {
		t.Fatal("IndexBefore before the first bar must report false")
	}
}

func TestSeriesSliceIsAView(t *testing.T) {
	s := buildSeries(100)
	v := s.Slice(s.Time[10], s.Time[20])
	if v.Len() != 10 {
		t.Fatalf("slice length = %d, want 10 (half-open)", v.Len())
	}
	if v.Time[0] != s.Time[10] {
		t.Fatal("slice must start at the requested bar")
	}

	// Mutating the parent's backing array must be visible through the view:
	// that is what makes Slice free, and what makes published series
	// immutable by contract rather than by copy.
	s.Close[12] = 999_999
	if v.Close[2] != 999_999 {
		t.Fatal("Slice returned a copy; it must return a view")
	}

	// The view must not be able to grow into the parent's later bars.
	before := s.Close[20]
	v.Close = append(v.Close, 1)
	if s.Close[20] != before {
		t.Fatal("appending to a view corrupted the parent series")
	}
}

func TestSeriesSliceEmptyRange(t *testing.T) {
	s := buildSeries(10)
	v := s.Slice(s.Time[5], s.Time[5])
	if v.Len() != 0 {
		t.Fatalf("empty range should yield an empty view, got %d", v.Len())
	}
	after := s.Slice(s.Time[9]+1, s.Time[9]+2)
	if after.Len() != 0 {
		t.Fatal("a range past the end should be empty")
	}
}

func TestSeriesBounds(t *testing.T) {
	if _, _, ok := NewSeries("T", TFM15, 0).Bounds(); ok {
		t.Fatal("an empty series has no bounds")
	}
	s := buildSeries(3)
	first, last, ok := s.Bounds()
	if !ok || first != s.Time[0] || last != s.Time[2] {
		t.Fatal("bounds mismatch")
	}
}

func BenchmarkSeriesSlice(b *testing.B) {
	s := buildSeries(500_000)
	from, to := s.Time[1000], s.Time[400_000]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Slice(from, to)
	}
}

func BenchmarkSeriesSliceInto(b *testing.B) {
	s := buildSeries(500_000)
	from, to := s.Time[1000], s.Time[400_000]
	var dst Series
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SliceInto(&dst, from, to)
	}
}

func TestSliceIntoMatchesSlice(t *testing.T) {
	s := buildSeries(1000)
	from, to := s.Time[10], s.Time[900]
	want := s.Slice(from, to)
	var got Series
	s.SliceInto(&got, from, to)
	if got.Len() != want.Len() || got.Time[0] != want.Time[0] ||
		got.Close[got.Len()-1] != want.Close[want.Len()-1] {
		t.Fatal("SliceInto disagreed with Slice")
	}
}

func BenchmarkSeriesScanClose(b *testing.B) {
	s := buildSeries(2_600_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var acc int64
		for _, c := range s.Close {
			acc += int64(c)
		}
		if acc == 0 {
			b.Fatal("empty")
		}
	}
}
