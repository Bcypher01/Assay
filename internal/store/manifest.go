package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bcypher01/assay/internal/core"
)

// Manifest describes one Parquet bar file: what it holds, where it came from,
// and the content hash that pins its bytes. It is persisted beside the file as
// <YYYY-MM>.manifest.json and is the unit a study's data snapshot is built
// from.
type Manifest struct {
	Symbol string  `json:"symbol"`
	TF     core.TF `json:"tf"`
	Month  string  `json:"month"` // "2026-08"

	Rows  int64     `json:"rows"`
	First core.Time `json:"first"`
	Last  core.Time `json:"last"`

	// ContentHash is the hex SHA-256 of the Parquet file's bytes.
	ContentHash string `json:"contentHash"`

	Source        string `json:"source"`        // venue the bars were built from
	BuildVersion  string `json:"buildVersion"`  // pipeline build that produced them
	SchemaVersion int    `json:"schemaVersion"` // on-disk format version
}

// MarshalJSON writes TF as its canonical string name rather than a bare
// integer, so a manifest stays legible and stable if the enum's numeric values
// ever shift.
func (m Manifest) MarshalJSON() ([]byte, error) {
	type alias struct {
		Symbol        string    `json:"symbol"`
		TF            string    `json:"tf"`
		Month         string    `json:"month"`
		Rows          int64     `json:"rows"`
		First         core.Time `json:"first"`
		Last          core.Time `json:"last"`
		ContentHash   string    `json:"contentHash"`
		Source        string    `json:"source"`
		BuildVersion  string    `json:"buildVersion"`
		SchemaVersion int       `json:"schemaVersion"`
	}
	return json.Marshal(alias{
		Symbol:        m.Symbol,
		TF:            m.TF.String(),
		Month:         m.Month,
		Rows:          m.Rows,
		First:         m.First,
		Last:          m.Last,
		ContentHash:   m.ContentHash,
		Source:        m.Source,
		BuildVersion:  m.BuildVersion,
		SchemaVersion: m.SchemaVersion,
	})
}

// UnmarshalJSON parses the string timeframe form written by MarshalJSON.
func (m *Manifest) UnmarshalJSON(b []byte) error {
	type alias struct {
		Symbol        string    `json:"symbol"`
		TF            string    `json:"tf"`
		Month         string    `json:"month"`
		Rows          int64     `json:"rows"`
		First         core.Time `json:"first"`
		Last          core.Time `json:"last"`
		ContentHash   string    `json:"contentHash"`
		Source        string    `json:"source"`
		BuildVersion  string    `json:"buildVersion"`
		SchemaVersion int       `json:"schemaVersion"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	tf, err := core.ParseTF(a.TF)
	if err != nil {
		return fmt.Errorf("store: manifest timeframe: %w", err)
	}
	m.Symbol = a.Symbol
	m.TF = tf
	m.Month = a.Month
	m.Rows = a.Rows
	m.First = a.First
	m.Last = a.Last
	m.ContentHash = a.ContentHash
	m.Source = a.Source
	m.BuildVersion = a.BuildVersion
	m.SchemaVersion = a.SchemaVersion
	return nil
}

// encode returns the deterministic JSON encoding of the manifest. Struct field
// order is fixed, so the bytes are stable for identical input.
func (m Manifest) encode() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// SnapshotHash returns a stable hex hash over a set of manifests, identifying
// exactly the data a study consumed. It is order-independent: the manifests are
// sorted by (symbol, timeframe, month) before hashing, so the same set in any
// order yields the same hash.
//
// Only data-identifying fields feed the hash. Source and BuildVersion describe
// provenance, not content: two pipeline builds that emit byte-identical bars
// share a ContentHash and must share a SnapshotHash, so those fields are
// excluded.
func SnapshotHash(ms []Manifest) string {
	sorted := make([]Manifest, len(ms))
	copy(sorted, ms)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.TF != b.TF {
			return a.TF < b.TF
		}
		return a.Month < b.Month
	})

	h := sha256.New()
	for i := range sorted {
		m := &sorted[i]
		// A fixed, delimiter-separated line per manifest. The record separator
		// bytes cannot appear in any field, so records are unambiguous.
		_, _ = fmt.Fprintf(h, "%s\x1f%s\x1f%s\x1f%d\x1f%d\x1f%d\x1f%s\x1f%d\x1e",
			m.Symbol, m.TF.String(), m.Month,
			m.Rows, int64(m.First), int64(m.Last),
			m.ContentHash, m.SchemaVersion)
	}
	return hex.EncodeToString(h.Sum(nil))
}
