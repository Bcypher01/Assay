package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"

	"github.com/Bcypher01/Assay/internal/core"
)

// SchemaVersion is the version of the on-disk bar format. It is recorded in
// every manifest; a reader refuses a file whose manifest declares a version it
// does not understand. Bump it on any change to columns, encodings, or the
// meaning of a field.
const SchemaVersion = 1

// Fixed provenance string embedded in every Parquet file. parquet-go otherwise
// derives "created by" from build info, which varies between builds and would
// make byte-for-byte output non-deterministic. Pinning it is what lets two
// writes of identical input produce identical bytes.
const (
	createdByApp     = "assay/store"
	createdByVersion = "v1"
	createdByBuild   = ""
)

// numBarColumns is the leaf-column count of the bar schema. Column order is the
// declaration order of barRow: time, open, high, low, close, volume.
const numBarColumns = 6

// barRow is the row shape the Parquet schema is derived from. It exists only to
// give parquet-go a typed schema with per-column encoding and compression; the
// hot read path never materialises it. Prices and time are plain int64 here —
// the tick and epoch-millisecond meaning lives in core, and the file carries
// raw integers so it is self-describing without the instrument spec.
//
// Encodings: delta on every column. Time is monotonically increasing, so its
// deltas are tiny and near-constant. Adjacent OHLC values move little bar to
// bar, so their deltas are small too. ZSTD then squeezes the delta streams.
type barRow struct {
	Time   int64 `parquet:"time,delta,zstd"`
	Open   int64 `parquet:"open,delta,zstd"`
	High   int64 `parquet:"high,delta,zstd"`
	Low    int64 `parquet:"low,delta,zstd"`
	Close  int64 `parquet:"close,delta,zstd"`
	Volume int64 `parquet:"volume,delta,zstd"`
}

// writerOptions returns the deterministic writer configuration. Every knob that
// could vary output bytes across builds or runs is pinned here.
func writerOptions() []parquet.WriterOption {
	return []parquet.WriterOption{
		parquet.CreatedBy(createdByApp, createdByVersion, createdByBuild),
	}
}

// encodeSeries writes s to a Parquet byte buffer using the bar schema. Output
// is deterministic: identical input yields byte-identical output.
func encodeSeries(s *core.Series) ([]byte, error) {
	n := s.Len()
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[barRow](&buf, writerOptions()...)

	// Write in bounded batches so we never build one giant []barRow. The batch
	// buffer is the only per-write scratch and is proportional to the batch,
	// not the series.
	const batch = 4096
	rows := make([]barRow, 0, batch)
	for i := 0; i < n; i++ {
		rows = append(rows, barRow{
			Time:   int64(s.Time[i]),
			Open:   int64(s.Open[i]),
			High:   int64(s.High[i]),
			Low:    int64(s.Low[i]),
			Close:  int64(s.Close[i]),
			Volume: s.Volume[i],
		})
		if len(rows) == batch {
			if _, err := w.Write(rows); err != nil {
				return nil, fmt.Errorf("store: writing parquet rows: %w", err)
			}
			rows = rows[:0]
		}
	}
	if len(rows) > 0 {
		if _, err := w.Write(rows); err != nil {
			return nil, fmt.Errorf("store: writing parquet rows: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("store: closing parquet writer: %w", err)
	}
	return buf.Bytes(), nil
}

// decodeSeries decodes a Parquet byte buffer into a *core.Series for the given
// symbol and timeframe. It decodes column-at-a-time through the typed
// Int64Reader fast path straight into preallocated slices, so allocation is
// proportional to the series, not to the row count.
func decodeSeries(data []byte, symbol string, tf core.TF) (*core.Series, error) {
	f, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("store: opening parquet file: %w", err)
	}

	n := int(f.NumRows())
	s := &core.Series{
		Symbol: symbol,
		TF:     tf,
		Time:   make([]core.Time, n),
		Open:   make([]core.Price, n),
		High:   make([]core.Price, n),
		Low:    make([]core.Price, n),
		Close:  make([]core.Price, n),
		Volume: make([]int64, n),
	}

	// One reusable scratch buffer bounded by the page read size, shared across
	// every column and page. This is the only per-row-independent allocation
	// in the decode loop.
	scratch := make([]int64, 8192)

	// offsets tracks the running write position per column across row groups.
	var offsets [numBarColumns]int

	for _, rg := range f.RowGroups() {
		chunks := rg.ColumnChunks()
		if len(chunks) != numBarColumns {
			return nil, fmt.Errorf("store: %w: got %d columns, want %d",
				ErrColumnLayout, len(chunks), numBarColumns)
		}
		for col, chunk := range chunks {
			if err := decodeColumn(col, chunk, s, &offsets[col], scratch); err != nil {
				return nil, err
			}
		}
	}

	for col, got := range offsets {
		if got != n {
			return nil, fmt.Errorf("store: column %d decoded %d of %d rows", col, got, n)
		}
	}
	return s, nil
}

// decodeColumn reads every value of one column chunk into the matching Series
// slice, advancing *off. It uses the typed Int64Reader fast path and copies
// through the shared scratch buffer, converting to the column's typed element.
func decodeColumn(col int, chunk parquet.ColumnChunk, s *core.Series, off *int, scratch []int64) error {
	pages := chunk.Pages()
	defer pages.Close()

	for {
		page, err := pages.ReadPage()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("store: reading page for column %d: %w", col, err)
		}
		vr := page.Values()
		ir, ok := vr.(parquet.Int64Reader)
		if !ok {
			return fmt.Errorf("store: %w: column %d is not int64", ErrColumnLayout, col)
		}
		for {
			m, rerr := ir.ReadInt64s(scratch)
			if m > 0 {
				if err := scatter(col, s, *off, scratch[:m]); err != nil {
					return err
				}
				*off += m
			}
			if rerr != nil {
				if rerr == io.EOF {
					break
				}
				return fmt.Errorf("store: decoding column %d: %w", col, rerr)
			}
		}
	}
}

// scatter copies a run of decoded int64s into the typed destination column at
// position off.
func scatter(col int, s *core.Series, off int, vals []int64) error {
	switch col {
	case 0:
		dst := s.Time[off : off+len(vals)]
		for i, v := range vals {
			dst[i] = core.Time(v)
		}
	case 1:
		dst := s.Open[off : off+len(vals)]
		for i, v := range vals {
			dst[i] = core.Price(v)
		}
	case 2:
		dst := s.High[off : off+len(vals)]
		for i, v := range vals {
			dst[i] = core.Price(v)
		}
	case 3:
		dst := s.Low[off : off+len(vals)]
		for i, v := range vals {
			dst[i] = core.Price(v)
		}
	case 4:
		dst := s.Close[off : off+len(vals)]
		for i, v := range vals {
			dst[i] = core.Price(v)
		}
	case 5:
		copy(s.Volume[off:off+len(vals)], vals)
	default:
		return fmt.Errorf("store: %w: column index %d", ErrColumnLayout, col)
	}
	return nil
}

// contentHash returns the hex SHA-256 of the Parquet file bytes. This is the
// value stored as Manifest.ContentHash and re-verified on read: because the
// writer is byte-deterministic, identical data yields an identical hash, and
// any flipped byte in any data page changes it. Hashing the persisted bytes
// (rather than the decoded values) means corruption is caught before decoding,
// with a typed error, independent of whether the corruption would also break
// decompression.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
