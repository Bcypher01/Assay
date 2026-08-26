package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRecordsOnlyToOptedInDirectory(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Record(context.Background(), "study_completed", map[string]any{"outcome": "success"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "study_completed") || !strings.Contains(string(b), "success") {
		t.Fatalf("event = %s", b)
	}
}
