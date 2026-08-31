// Package telemetry provides deliberately local, opt-in product metrics.
// It never opens a network connection and records no strategy documents,
// prices, account values, or user identifiers.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Local appends anonymous operational events to metrics.jsonl below Dir.
// The directory is only created when the user has explicitly opted in.
type Local struct {
	path string
	mu   sync.Mutex
}

// Open creates a local-only recorder. The caller should only invoke Open for
// an explicit opt-in command-line setting.
func Open(dir string) (*Local, error) {
	if dir == "" {
		return nil, fmt.Errorf("telemetry: metrics directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("telemetry: create metrics directory: %w", err)
	}
	return &Local{path: filepath.Join(dir, "metrics.jsonl")}, nil
}

// Record satisfies rpc.EventRecorder. Values must be non-sensitive aggregate
// fields such as duration, outcome, timeframe, or error code.
func (l *Local) Record(_ context.Context, name string, fields map[string]any) error {
	if l == nil {
		return nil
	}
	event := struct {
		At     time.Time      `json:"at"`
		Name   string         `json:"name"`
		Fields map[string]any `json:"fields,omitempty"`
	}{At: time.Now().UTC(), Name: name, Fields: fields}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("telemetry: encode event: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("telemetry: open metrics file: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("telemetry: write event: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("telemetry: close metrics file: %w", err)
	}
	return nil
}
