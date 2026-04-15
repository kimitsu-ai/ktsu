package hub

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// expandHome replaces a leading ~/ with the user's home directory.
// If the home directory cannot be determined, logs a warning and returns path unchanged.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("WARNING: expandHome: cannot resolve home directory: %v; using path as-is: %s", err, path)
		return path
	}
	return filepath.Join(home, path[2:])
}
