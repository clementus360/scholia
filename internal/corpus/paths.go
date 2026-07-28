package corpus

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDataDir is where the seed source files live, relative to the repo root.
const DefaultDataDir = "data"

// sentinelFile is present in a real data directory and nowhere else. Matching on
// it prevents resolution from settling on some unrelated "data" folder that
// happens to sit in a parent directory.
const sentinelFile = "BSB.json"

// ResolveDataDir locates the seed source files.
//
// Priority: SCHOLIA_DATA_DIR, then a "data" directory containing BSB.json in the
// working directory or any ancestor. Walking upwards means `go run ./cmd/seed`
// works from anywhere in the repo, which is how it is usually invoked.
func ResolveDataDir() (string, error) {
	if override := os.Getenv("SCHOLIA_DATA_DIR"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			abs = override
		}
		if _, err := os.Stat(filepath.Join(abs, sentinelFile)); err != nil {
			return "", fmt.Errorf("SCHOLIA_DATA_DIR=%s does not contain %s", abs, sentinelFile)
		}
		return abs, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine working directory: %w", err)
	}

	probeDir := cwd
	for {
		candidate := filepath.Join(probeDir, DefaultDataDir)
		if _, err := os.Stat(filepath.Join(candidate, sentinelFile)); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(probeDir)
		if parent == probeDir {
			break
		}
		probeDir = parent
	}

	return "", fmt.Errorf(
		"could not find a data directory containing %s, searched from %s upwards; set SCHOLIA_DATA_DIR",
		sentinelFile, cwd)
}
