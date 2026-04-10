package hub

import (
	"fmt"
	"os"
	"strings"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

// UpdateOpts configures a hub update operation.
type UpdateOpts struct {
	LockPath string
	Latest   bool // also update pinned version entries (registry not yet implemented)
	DryRun   bool
}

// Update re-resolves all entries in ktsuhub.lock.yaml.
// Mutable entries (installed from a branch) are fetched and their SHA updated.
// Pinned entries are skipped unless --latest is passed (registry update not yet implemented).
func Update(opts UpdateOpts) error {
	lock, err := config.LoadHubLock(opts.LockPath)
	if err != nil {
		return fmt.Errorf("load lock: %w", err)
	}

	changed := false
	for i, entry := range lock.Entries {
		if !entry.Mutable && !opts.Latest {
			continue
		}
		if !entry.Mutable && opts.Latest {
			fmt.Printf("WARN  %s is pinned — registry update not yet implemented, skipping\n", entry.Name)
			continue
		}

		// Mutable entry: fetch and pull latest
		cacheDir := expandHome(entry.Cache)

		if opts.DryRun {
			fmt.Printf("would update %s (mutable, current SHA: %s)\n", entry.Name, entry.SHA)
			continue
		}

		if err := gitFetch(cacheDir); err != nil {
			return fmt.Errorf("fetch %s: %w", entry.Name, err)
		}
		// Pull with ff-only; if that fails try a plain pull
		if err := gitRun(cacheDir, "merge", "--ff-only", "origin/HEAD"); err != nil {
			if pullErr := gitRun(cacheDir, "pull", "--ff-only"); pullErr != nil {
				return fmt.Errorf("pull %s: %w", entry.Name, pullErr)
			}
		}

		newSHA, err := gitRevParse(cacheDir, "HEAD")
		if err != nil {
			return fmt.Errorf("rev-parse %s: %w", entry.Name, err)
		}

		if newSHA != entry.SHA {
			oldShort := entry.SHA
			if len(oldShort) > 7 {
				oldShort = oldShort[:7]
			}
			newShort := newSHA
			if len(newShort) > 7 {
				newShort = newShort[:7]
			}
			fmt.Printf("WARN  %s@%s is a mutable branch ref.\n      SHA updated: %s → %s\n      Re-run ktsu validate to check for breaking changes.\n",
				entry.Name, entry.Ref, oldShort, newShort)
			lock.Entries[i].SHA = newSHA
			changed = true
		}
	}

	if !opts.DryRun && changed {
		return config.SaveHubLock(opts.LockPath, lock)
	}
	return nil
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}
