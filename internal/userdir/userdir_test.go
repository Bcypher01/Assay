package userdir

import (
	"path/filepath"
	"testing"
)

func TestHomeEnvOverridesEveryBase(t *testing.T) {
	t.Setenv(HomeEnv, filepath.Join("/srv", "assay"))
	cache, err := Cache("cache")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/srv", "assay", "cache"); cache != want {
		t.Fatalf("Cache = %q, want %q", cache, want)
	}
	data, err := Data("history")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/srv", "assay", "history"); data != want {
		t.Fatalf("Data = %q, want %q", data, want)
	}
}

func TestDataHonoursXDGDataHome(t *testing.T) {
	t.Setenv(HomeEnv, "")
	t.Setenv("XDG_DATA_HOME", filepath.Join("/home", "trader", "share"))
	dir, err := Data("events")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home", "trader", "share", appName, "events"); dir != want {
		t.Fatalf("Data = %q, want %q", dir, want)
	}
}

func TestSubdirectoriesAreDistinct(t *testing.T) {
	t.Setenv(HomeEnv, filepath.Join("/srv", "assay"))
	history, err := Data("history")
	if err != nil {
		t.Fatal(err)
	}
	events, err := Data("events")
	if err != nil {
		t.Fatal(err)
	}
	if history == events {
		t.Fatalf("history and events resolved to the same directory %q", history)
	}
}
