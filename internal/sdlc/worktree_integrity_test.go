package sdlc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrityRespectsRegisteredWorktreeBoundaries(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	target := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command(git, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		command.Dir = dir
		command.Env = gitWorktreeEnvironment()
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run(target, "init")
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	run(target, "add", ".")
	run(target, "commit", "-m", "fixture")
	nested := filepath.Join(target, "arbitrary location", "child")
	run(target, "worktree", "add", "--detach", nested, "HEAD")
	if _, err := Verify(target); err != nil {
		t.Fatalf("parent rejected registered worktree: %v", err)
	}
	if _, err := Verify(nested); err != nil {
		t.Fatalf("child cannot verify itself: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "SDLC.md"), []byte("drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(nested); err == nil {
		t.Fatal("child drift went undetected")
	}
	if _, err := Verify(target); err != nil {
		t.Fatalf("child drift leaked across boundary: %v", err)
	}

	// A copied marker is not registration, even if it names a real Git directory.
	marker, err := os.ReadFile(filepath.Join(nested, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(target, ".worktrees", "unregistered")
	if err := os.MkdirAll(fake, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{".git": marker, "SDLC.md": []byte("shadow")} {
		if err := os.WriteFile(filepath.Join(fake, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Verify(target); err == nil || !strings.Contains(err.Error(), ".worktrees/unregistered/SDLC.md") {
		t.Fatalf("fake marker bypassed detection: %v", err)
	}
	if err := os.RemoveAll(fake); err != nil {
		t.Fatal(err)
	}

	other := t.TempDir()
	run(other, "init")
	if err := os.WriteFile(filepath.Join(nested, ".git"), []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, inherited := range []string{"none", "parent", "other", "config"} {
		t.Run("replaced-marker/"+inherited, func(t *testing.T) {
			switch inherited {
			case "parent":
				t.Setenv("GIT_DIR", filepath.Join(target, ".git"))
			case "other":
				t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
				t.Setenv("GIT_WORK_TREE", target)
			case "config":
				t.Setenv("GIT_CONFIG_COUNT", "1")
				t.Setenv("GIT_CONFIG_KEY_0", "core.worktree")
				t.Setenv("GIT_CONFIG_VALUE_0", target)
			}
			if shadows := shadowArtifacts(target); len(shadows) != 2 {
				t.Fatalf("replaced marker hid artifacts: %v", shadows)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(nested, ".git"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("valid-marker/foreign-context", func(t *testing.T) {
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		t.Setenv("GIT_WORK_TREE", other)
		t.Setenv("GIT_COMMON_DIR", filepath.Join(other, ".git"))
		if _, err := Verify(target); err != nil {
			t.Fatalf("inherited Git context changed worktree detection: %v", err)
		}
	})

	// A stale registry entry alone must not suppress inspection.
	if err := os.Remove(filepath.Join(nested, ".git")); err != nil {
		t.Fatal(err)
	}
	if shadows := shadowArtifacts(target); len(shadows) != 2 {
		t.Fatalf("stale registration hid artifacts: %v", shadows)
	}
}

func TestShadowScanWithoutGitStillFindsArtifacts(t *testing.T) {
	target := t.TempDir()
	nested := filepath.Join(target, ".worktrees", "unregistered")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "SDLC.md"), []byte("shadow"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	shadows := shadowArtifacts(target)
	if len(shadows) != 1 || shadows[0] != ".worktrees/unregistered/SDLC.md" {
		t.Fatalf("missing Git hid artifacts: %v", shadows)
	}
}
