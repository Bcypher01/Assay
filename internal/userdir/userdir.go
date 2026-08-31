// Package userdir resolves the per-user directories Assay writes to when the
// operator gives no explicit path.
//
// It is wiring-only. Nothing that participates in a run may call it: a run is a
// pure function of (IR document, data snapshot, seed), and a path that depends
// on the host environment is not part of that tuple. Only cmd/ resolves these,
// and every one of them is overridable by a flag.
package userdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// appName is the directory Assay owns inside each per-user base directory.
const appName = "assay"

// HomeEnv overrides every base directory at once. It exists for containers and
// for operators who want all Assay state in one place; deploy/api.Dockerfile
// passes explicit flags instead and does not rely on it.
const HomeEnv = "ASSAY_HOME"

// Cache returns the directory for regenerable state — data that can be deleted
// at any time and refetched. sub is appended to the Assay-owned directory.
func Cache(sub string) (string, error) {
	if home := os.Getenv(HomeEnv); home != "" {
		return filepath.Join(home, sub), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(base, appName, sub), nil
}

// Data returns the directory for state that must survive a cache wipe — study
// history and immutable event snapshots. sub is appended to the Assay-owned
// directory.
func Data(sub string) (string, error) {
	if home := os.Getenv(HomeEnv); home != "" {
		return filepath.Join(home, sub), nil
	}
	base, err := dataBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName, sub), nil
}

// dataBase implements the XDG data directory, which the standard library does
// not expose. On macOS and Windows it is the same base os.UserConfigDir uses.
func dataBase() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir, nil
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".local", "share"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user data directory: %w", err)
	}
	return base, nil
}
