package hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

// InstallOpts configures a hub install operation.
type InstallOpts struct {
	Target   string // git URL, github.com/owner/repo, or local path
	Ref      string // optional: tag, branch, or commit SHA
	CacheDir string // local cache root (e.g. ~/.ktsu/cache)
	LockPath string // path to ktsuhub.lock.yaml to update
	DryRun   bool
}

// Install clones a workflow package into CacheDir and writes an entry to ktsuhub.lock.yaml.
func Install(opts InstallOpts) error {
	source, cloneURL, mutable, err := resolveTarget(opts.Target, opts.Ref)
	if err != nil {
		return err
	}

	name := sourceToName(source)
	cachePath := filepath.Join(opts.CacheDir, filepath.FromSlash(name))

	if opts.DryRun {
		fmt.Printf("would install %s → %s\n", source, cachePath)
		return nil
	}

	// Clone or fetch
	if _, statErr := os.Stat(cachePath); os.IsNotExist(statErr) {
		if err := gitClone(cloneURL, cachePath); err != nil {
			return fmt.Errorf("git clone %s: %w", cloneURL, err)
		}
	} else {
		if err := gitFetch(cachePath); err != nil {
			return fmt.Errorf("git fetch: %w", err)
		}
	}

	// Checkout ref if specified
	if opts.Ref != "" {
		if err := gitCheckout(cachePath, opts.Ref); err != nil {
			return fmt.Errorf("git checkout %s: %w", opts.Ref, err)
		}
	}

	// Resolve HEAD SHA
	sha, err := gitRevParse(cachePath, "HEAD")
	if err != nil {
		return fmt.Errorf("git rev-parse HEAD: %w", err)
	}

	// Validate ktsuhub.yaml exists
	manifestPath := filepath.Join(cachePath, "ktsuhub.yaml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		_ = os.RemoveAll(cachePath)
		return fmt.Errorf("ktsuhub.yaml not found in %s — not a valid ktsuhub package", source)
	}

	// Read manifest to get workflow name/version
	manifest, err := config.LoadHubManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load ktsuhub.yaml: %w", err)
	}
	if len(manifest.Workflows) == 0 {
		return fmt.Errorf("ktsuhub.yaml declares no workflows")
	}

	entry := config.HubLockEntry{
		Name:    manifest.Workflows[0].Name,
		Version: manifest.Workflows[0].Version,
		Source:  source,
		Ref:     opts.Ref,
		SHA:     sha,
		Cache:   cachePath,
		Mutable: mutable,
	}

	// Load existing lock file or start fresh
	var lock *config.HubLockFile
	if existing, err := config.LoadHubLock(opts.LockPath); err == nil {
		lock = existing
	} else {
		lock = &config.HubLockFile{}
	}

	// Upsert entry by source
	updated := false
	for i, e := range lock.Entries {
		if e.Source == source {
			lock.Entries[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		lock.Entries = append(lock.Entries, entry)
	}

	return config.SaveHubLock(opts.LockPath, lock)
}

// resolveTarget parses a target string into (source, cloneURL, mutable).
// mutable is true when the ref is a branch name (not a version tag or SHA).
func resolveTarget(target, ref string) (source, cloneURL string, mutable bool, err error) {
	switch {
	case strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://"):
		return target, target, ref != "" && !looksLikeVersion(ref), nil
	case strings.HasPrefix(target, "github.com/"):
		url := "https://" + target + ".git"
		return target, url, ref != "" && !looksLikeVersion(ref), nil
	case strings.HasPrefix(target, "/") || strings.HasPrefix(target, "."):
		// local path (for testing and dev)
		return target, target, ref != "" && !looksLikeVersion(ref), nil
	default:
		// short form owner/name — requires registry (not yet implemented)
		return "", "", false, fmt.Errorf("registry install (short form %q) not yet implemented — use github.com/owner/repo or https://...", target)
	}
}

// sourceToName converts a source string to a safe relative directory name for the cache.
// For local paths, only the last two path components are used — note that paths sharing
// the same final two components will collide in the cache. Local paths are intended for
// development and testing only.
func sourceToName(source string) string {
	s := strings.TrimPrefix(source, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimPrefix(s, "/")
	// For local paths, use the last two path components to avoid conflicts
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") {
		parts := strings.Split(filepath.ToSlash(s), "/")
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], "/")
		}
		return parts[len(parts)-1]
	}
	return s
}

// looksLikeVersion returns true if ref looks like a semver tag (e.g. "v1.2.0", "1.2.0").
func looksLikeVersion(ref string) bool {
	r := strings.TrimPrefix(ref, "v")
	return len(r) > 0 && r[0] >= '0' && r[0] <= '9'
}

func gitClone(url, dest string) error {
	return gitRun("", "clone", url, dest)
}

func gitFetch(dir string) error {
	return gitRun(dir, "fetch")
}

func gitCheckout(dir, ref string) error {
	return gitRun(dir, "checkout", ref)
}

func gitRevParse(dir, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}
