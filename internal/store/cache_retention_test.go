package store

import (
	"context"
	"os"
	"testing"

	"github.com/bcypher01/assay/internal/core"
)

func TestLocalStoreEvictsOldParquetUnderByteLimit(t *testing.T) {
	s := NewLocalStoreWithMaxBytes(t.TempDir(), 1)
	series := core.NewSeries("EURUSD", core.TFM1, 1)
	series.Append(core.MustParseTime("2025-01-02T00:00:00Z"), 1, 1, 1, 1, 1)
	if err := s.Write(context.Background(), series, Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2025-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.parquetPath("EURUSD", core.TFM1, "2025-01")); !os.IsNotExist(err) {
		t.Fatalf("parquet retained despite one-byte budget: %v", err)
	}
	if _, err := os.Stat(s.manifestPath("EURUSD", core.TFM1, "2025-01")); !os.IsNotExist(err) {
		t.Fatalf("manifest retained after eviction: %v", err)
	}
}
