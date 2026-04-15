package hub_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/hub"
)

func TestUpdate_refreshesMutableEntry(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a source repo
	repoDir := t.TempDir()
	runInDir := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	initTestRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "ktsuhub.yaml"), []byte(`workflows:
  - name: testorg/hello
    version: "1.0.0"
    entrypoint: workflows/hello.workflow.yaml
`), 0o644)
	runInDir(repoDir, "add", ".")
	runInDir(repoDir, "commit", "-m", "init")
	sha1 := runInDir(repoDir, "rev-parse", "HEAD")

	// Clone to cache
	cacheEntryDir := filepath.Join(t.TempDir(), "testorg/hello")
	if out, err := exec.Command("git", "clone", repoDir, cacheEntryDir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// Set the remote to point to our local repo for fetch to work
	// (git clone already sets origin to repoDir, so this is already set)

	// Write lock file with mutable entry
	lockPath := filepath.Join(t.TempDir(), "ktsuhub.lock.yaml")
	lock := &config.HubLockFile{
		Entries: []config.HubLockEntry{
			{
				Name:    "testorg/hello",
				Source:  repoDir,
				Ref:     "main",
				SHA:     sha1,
				Cache:   cacheEntryDir,
				Mutable: true,
			},
		},
	}
	if err := config.SaveHubLock(lockPath, lock); err != nil {
		t.Fatalf("SaveHubLock: %v", err)
	}

	// Add a second commit to source repo
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("v2"), 0o644)
	runInDir(repoDir, "add", ".")
	runInDir(repoDir, "commit", "-m", "second commit")
	sha2 := runInDir(repoDir, "rev-parse", "HEAD")

	// Run Update
	if err := hub.Update(hub.UpdateOpts{LockPath: lockPath}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	updated, err := config.LoadHubLock(lockPath)
	if err != nil {
		t.Fatalf("load lock after update: %v", err)
	}
	if updated.Entries[0].SHA == sha1 {
		t.Errorf("expected SHA to be updated from %s, still the same", sha1)
	}
	if updated.Entries[0].SHA != sha2 {
		t.Errorf("expected SHA %s, got %s", sha2, updated.Entries[0].SHA)
	}
}

func TestUpdate_skipsImmutableEntry(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "ktsuhub.lock.yaml")
	lock := &config.HubLockFile{
		Entries: []config.HubLockEntry{
			{
				Name:    "kyle/pinned",
				Source:  "github.com/kyle/pinned",
				Version: "1.0.0",
				SHA:     "pinned-sha",
				Cache:   "/tmp/fake-cache",
				Mutable: false,
			},
		},
	}
	if err := config.SaveHubLock(lockPath, lock); err != nil {
		t.Fatalf("SaveHubLock: %v", err)
	}

	if err := hub.Update(hub.UpdateOpts{LockPath: lockPath}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := config.LoadHubLock(lockPath)
	if err != nil {
		t.Fatalf("load lock after update: %v", err)
	}
	if after.Entries[0].SHA != "pinned-sha" {
		t.Errorf("pinned entry SHA should be unchanged, got %q", after.Entries[0].SHA)
	}
}

func TestUpdate_dryRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("v1"), 0o644)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", ".")
	run("commit", "-m", "init")
	sha1 := run("rev-parse", "HEAD")

	cacheDir := filepath.Join(t.TempDir(), "testorg/hello")
	if out, err := exec.Command("git", "clone", repoDir, cacheDir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	lockPath := filepath.Join(t.TempDir(), "ktsuhub.lock.yaml")
	lock := &config.HubLockFile{
		Entries: []config.HubLockEntry{
			{Name: "testorg/hello", Source: repoDir, SHA: sha1, Cache: cacheDir, Mutable: true},
		},
	}
	if err := config.SaveHubLock(lockPath, lock); err != nil {
		t.Fatalf("SaveHubLock: %v", err)
	}

	// Add commit to source
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("v2"), 0o644)
	run("add", ".")
	run("commit", "-m", "v2")

	// DryRun should not update SHA in lock
	if err := hub.Update(hub.UpdateOpts{LockPath: lockPath, DryRun: true}); err != nil {
		t.Fatalf("Update DryRun: %v", err)
	}

	after, _ := config.LoadHubLock(lockPath)
	if after.Entries[0].SHA != sha1 {
		t.Errorf("DryRun should not have updated SHA, got %q, want %q", after.Entries[0].SHA, sha1)
	}

	// Verify the local clone's HEAD was not advanced (fetch was skipped)
	cloneHead := strings.TrimSpace(func() string {
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = cacheDir
		out, _ := cmd.Output()
		return string(out)
	}())
	if cloneHead != sha1 {
		t.Errorf("DryRun should not have advanced cache HEAD: got %s, want %s", cloneHead, sha1)
	}
}
