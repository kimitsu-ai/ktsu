package hub_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/hub"
)

func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
}

func TestInstall_fromLocalPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	manifest := `workflows:
  - name: testorg/hello
    version: "1.0.0"
    entrypoint: workflows/hello.workflow.yaml
`
	os.WriteFile(filepath.Join(repoDir, "ktsuhub.yaml"), []byte(manifest), 0o644)
	os.MkdirAll(filepath.Join(repoDir, "workflows"), 0o755)
	os.WriteFile(filepath.Join(repoDir, "workflows", "hello.workflow.yaml"), []byte(`kind: workflow
name: hello
version: "1.0.0"
pipeline: []
`), 0o644)

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.CombinedOutput()
	}
	run("add", ".")
	run("commit", "-m", "init")

	cacheDir := t.TempDir()
	lockPath := filepath.Join(t.TempDir(), "ktsuhub.lock.yaml")

	err := hub.Install(hub.InstallOpts{
		Target:   repoDir,
		CacheDir: cacheDir,
		LockPath: lockPath,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	lock, err := config.LoadHubLock(lockPath)
	if err != nil {
		t.Fatalf("LoadHubLock: %v", err)
	}
	if len(lock.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lock.Entries))
	}
	if lock.Entries[0].Source != repoDir {
		t.Errorf("expected source %q, got %q", repoDir, lock.Entries[0].Source)
	}
	if lock.Entries[0].SHA == "" {
		t.Error("expected non-empty SHA")
	}
	if lock.Entries[0].Name != "testorg/hello" {
		t.Errorf("expected name testorg/hello, got %q", lock.Entries[0].Name)
	}
}

func TestInstall_missingManifest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello"), 0o644)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.CombinedOutput()
	}
	run("add", ".")
	run("commit", "-m", "init")

	err := hub.Install(hub.InstallOpts{
		Target:   repoDir,
		CacheDir: t.TempDir(),
		LockPath: filepath.Join(t.TempDir(), "ktsuhub.lock.yaml"),
	})
	if err == nil {
		t.Fatal("expected error for missing ktsuhub.yaml")
	}
}

func TestInstall_dryRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// DryRun should not clone anything or create the lock file
	lockPath := filepath.Join(t.TempDir(), "ktsuhub.lock.yaml")
	err := hub.Install(hub.InstallOpts{
		Target:   "github.com/example/repo",
		CacheDir: t.TempDir(),
		LockPath: lockPath,
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("DryRun Install returned error: %v", err)
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Error("DryRun should not have created the lock file")
	}
}

func TestInstall_shortFormReturnsError(t *testing.T) {
	err := hub.Install(hub.InstallOpts{
		Target:   "owner/repo",
		CacheDir: t.TempDir(),
		LockPath: filepath.Join(t.TempDir(), "ktsuhub.lock.yaml"),
	})
	if err == nil {
		t.Fatal("expected error for short-form registry target")
	}
}
