package fscache

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// GetCacheDir returns the cache directory for the given service and id
// The directory is created if it does not exist
func GetCacheDir(service string, id string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		slog.Error("Error getting user cache dir", err)
		return "", fmt.Errorf("error getting user cache dir: %w", err)
	}

	dir := filepath.Join(cacheDir, "omnivorous", service, id)

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		slog.Error("Error creating cache dir", err)
		return "", fmt.Errorf("error creating cache dir: %w", err)
	}

	return dir, nil
}
