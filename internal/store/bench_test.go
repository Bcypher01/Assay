package store

import (
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
)

// benchSeries builds n aligned M1 bars in memory for benchmarking. It does not
// depend on the testing.T helpers so it can run under a benchmark.
func benchSeries(n int) *core.Series {
	s := core.NewSeries("EURUSD", core.TFM1, n)
	base := core.MustParseTime("2020-01-01T00:00:00Z")
	step := core.TFM1.Millis()
	price := int64(108000)
	for i := 0; i < n; i++ {
		ts := base + core.Time(int64(i)*step)
		price += int64((i*2654435761)%7) - 3
		o := price
		c := price + int64((i*40503)%5) - 2
		hi, lo := o, o
		if c > hi {
			hi = c
		}
		if c < lo {
			lo = c
		}
		hi += int64((i * 15485863) % 4)
		lo -= int64((i * 12582917) % 4)
		s.Append(ts, core.Price(o), core.Price(hi), core.Price(lo), core.Price(c), int64(1000+i))
	}
	return s
}

// BenchmarkDecode2p6M measures the reader decoding 2.6M M1 bars from an
// in-memory Parquet buffer. The exit criterion is under 500ms with allocations
// proportional to the series (six typed slices plus bounded page scratch), not
// to the row count.
func BenchmarkDecode2p6M(b *testing.B) {
	const n = 2_600_000
	s := benchSeries(n)
	data, err := encodeSeries(s)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := decodeSeries(data, "EURUSD", core.TFM1)
		if err != nil {
			b.Fatal(err)
		}
		if got.Len() != n {
			b.Fatalf("decoded %d rows, want %d", got.Len(), n)
		}
	}
}

// BenchmarkEncode2p6M measures writer throughput for the same series.
func BenchmarkEncode2p6M(b *testing.B) {
	const n = 2_600_000
	s := benchSeries(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encodeSeries(s); err != nil {
			b.Fatal(err)
		}
	}
}
