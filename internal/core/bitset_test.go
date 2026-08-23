package core

import "testing"

func TestBitsetWordBoundaries(t *testing.T) {
	b := NewBitset(200)
	for _, i := range []int{0, 63, 64, 65, 127, 128, 199} {
		b.Set(i)
	}
	for _, i := range []int{0, 63, 64, 65, 127, 128, 199} {
		if !b.Get(i) {
			t.Fatalf("bit %d should be set", i)
		}
	}
	for _, i := range []int{1, 62, 66, 126, 129, 198} {
		if b.Get(i) {
			t.Fatalf("bit %d should be clear", i)
		}
	}
	if got := b.Count(); got != 7 {
		t.Fatalf("count = %d, want 7", got)
	}
}

func TestBitsetOutOfRange(t *testing.T) {
	b := NewBitset(64)
	b.Set(64)
	b.Set(-1)
	if b.Count() != 0 {
		t.Fatal("out-of-range writes must be ignored")
	}
	if b.Get(64) || b.Get(-1) {
		t.Fatal("out-of-range reads must report false")
	}
}

func TestBitsetTrailingBitsNotReported(t *testing.T) {
	// 130 bits occupies three words; bits 130..191 exist physically but must
	// never be reported by Next.
	b := NewBitset(130)
	b.words[2] = ^uint64(0) // set every bit of the final word directly
	if _, ok := b.Next(130); ok {
		t.Fatal("Next must not report bits past the logical length")
	}
}

func TestBitsetNext(t *testing.T) {
	b := NewBitset(500)
	want := []int{3, 64, 300, 499}
	for _, i := range want {
		b.Set(i)
	}
	var got []int
	for i, ok := b.Next(0); ok; i, ok = b.Next(i + 1) {
		got = append(got, i)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestBitsetResetAndResize(t *testing.T) {
	b := NewBitset(100)
	b.Set(5)
	b.Reset()
	if b.Count() != 0 || b.Len() != 100 {
		t.Fatal("Reset must clear bits and preserve length")
	}
	b.Set(5)
	b.Resize(50)
	if b.Len() != 50 || b.Count() != 0 {
		t.Fatal("Resize must clear contents and set the new length")
	}
}

func BenchmarkBitsetIterate(b *testing.B) {
	const n = 2_600_000
	bs := NewBitset(n)
	for i := 0; i < n; i += 97 { // ~26,800 set bits, a realistic signal density
		bs.Set(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for it := 0; it < b.N; it++ {
		count := 0
		for i, ok := bs.Next(0); ok; i, ok = bs.Next(i + 1) {
			count++
		}
		if count == 0 {
			b.Fatal("no bits")
		}
	}
}

func BenchmarkBitsetCount(b *testing.B) {
	bs := NewBitset(2_600_000)
	for i := 0; i < 2_600_000; i += 3 {
		bs.Set(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for it := 0; it < b.N; it++ {
		_ = bs.Count()
	}
}
