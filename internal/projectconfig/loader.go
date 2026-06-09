package projectconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxSize = 64 * 1024 // 64KB

// Load looks for forge.md in dir and returns its parsed config.
// Returns (nil, nil) if forge.md does not exist.
func Load(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, "forge.md")

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("projectconfig: reading forge.md: %w", err)
	}

	if info.Size() > maxSize {
		return nil, fmt.Errorf("projectconfig: forge.md exceeds maximum size of 64KB")
	}

	data, err := os.ReadFile(path)
	loadedAt := time.Now()
	if err != nil {
		return nil, fmt.Errorf("projectconfig: reading forge.md: %w", err)
	}

	// TODO: hot-reloading forge.md (watch for file changes) would hook in here.
	// TODO: load additional config files (e.g. .forgeignore) alongside forge.md here.

	return &ProjectConfig{
		Path:     path,
		Raw:      strings.TrimRight(string(data), " \t\r\n"),
		LoadedAt: loadedAt,
	}, nil
}
