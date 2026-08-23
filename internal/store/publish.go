package store

import (
	"fmt"

	"github.com/olumide/assay/internal/core"
)

// EncodeForPublish validates and encodes one month of bars, returning the
// deterministic Parquet bytes and a completed manifest for those bytes.
func EncodeForPublish(series *core.Series, meta Manifest) ([]byte, Manifest, error) {
	if series.Len() == 0 {
		return nil, Manifest{}, ErrEmptySeries
	}
	if err := series.Validate(); err != nil {
		return nil, Manifest{}, fmt.Errorf("store: invalid series: %w", err)
	}
	if meta.Symbol != series.Symbol {
		return nil, Manifest{}, fmt.Errorf("store: %w: manifest symbol %q, series symbol %q",
			ErrManifestMismatch, meta.Symbol, series.Symbol)
	}
	if meta.TF != series.TF {
		return nil, Manifest{}, fmt.Errorf("store: %w: manifest tf %s, series tf %s",
			ErrManifestMismatch, meta.TF, series.TF)
	}

	first, last, _ := series.Bounds()
	if monthKey(first) != meta.Month || monthKey(last) != meta.Month {
		return nil, Manifest{}, fmt.Errorf("store: %w: bars span %s..%s but month is %q",
			ErrManifestMismatch, monthKey(first), monthKey(last), meta.Month)
	}

	data, err := encodeSeries(series)
	if err != nil {
		return nil, Manifest{}, err
	}
	meta.Rows = int64(series.Len())
	meta.First = first
	meta.Last = last
	meta.ContentHash = contentHash(data)
	meta.SchemaVersion = SchemaVersion
	return data, meta, nil
}

// DecodePublished decodes deterministic bar bytes read from a publisher.
func DecodePublished(data []byte, symbol string, tf core.TF) (*core.Series, error) {
	series, err := decodeSeries(data, symbol, tf)
	if err != nil {
		return nil, err
	}
	if err := series.Validate(); err != nil {
		return nil, fmt.Errorf("store: invalid published series: %w", err)
	}
	return series, nil
}

// VerifyPublished checks that deterministic published bytes match their
// manifest and decode to the identity named by the manifest.
func VerifyPublished(data []byte, meta Manifest) error {
	if meta.SchemaVersion != SchemaVersion {
		return fmt.Errorf("store: %w: manifest declares version %d, want %d",
			ErrSchemaVersion, meta.SchemaVersion, SchemaVersion)
	}
	if got := contentHash(data); got != meta.ContentHash {
		return fmt.Errorf("store: %w: manifest %s, file %s",
			ErrHashMismatch, meta.ContentHash, got)
	}
	series, err := DecodePublished(data, meta.Symbol, meta.TF)
	if err != nil {
		return err
	}
	if series.Len() == 0 {
		return ErrEmptySeries
	}
	first, last, _ := series.Bounds()
	if first != meta.First || last != meta.Last || int64(series.Len()) != meta.Rows {
		return fmt.Errorf("store: %w: manifest bounds/rows do not match bytes",
			ErrManifestMismatch)
	}
	if monthKey(first) != meta.Month || monthKey(last) != meta.Month {
		return fmt.Errorf("store: %w: bars span %s..%s but month is %q",
			ErrManifestMismatch, monthKey(first), monthKey(last), meta.Month)
	}
	return nil
}
