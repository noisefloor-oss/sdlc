package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func launcherFixture(t *testing.T) string {
	t.Helper()
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "source with spaces")
	for _, directory := range []string{"bin", "cmd/noise-sdlc", "internal"} {
		if err := os.MkdirAll(filepath.Join(fixture, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"noise-sdlc", "build-noise-sdlc", "launcher-common.sh"} {
		contents, err := os.ReadFile(filepath.Join(root, "bin", name))
		if err != nil {
			t.Fatal(err)
		}
		writeLauncherFile(t, filepath.Join(fixture, "bin", name), string(contents), 0o755)
	}
	writeLauncherFile(t, filepath.Join(fixture, "go.mod"), "module launcher-fixture\n\ngo 1.26.10\n", 0o644)
	writeLauncherFile(t, filepath.Join(fixture, "cmd/noise-sdlc/main.go"), `package main
import ("fmt"; "os"; "strings")
func main() {
 if len(os.Args)>1 && os.Args[1]=="fail" { os.Exit(7) }
 directory,_:=os.Getwd()
 fmt.Println("same-version|before|"+directory+"|"+strings.Join(os.Args[1:],"|"))
}
`, 0o644)
	return fixture
}

func writeLauncherFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func runFixture(t *testing.T, root, caller, executable string, env []string, arguments ...string) (string, error) {
	t.Helper()
	command := exec.Command(filepath.Join(root, "bin", executable), arguments...)
	command.Dir = caller
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func buildFixture(t *testing.T, root string) {
	t.Helper()
	if output, err := runFixture(t, root, t.TempDir(), "build-noise-sdlc", nil); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}
}

func TestLauncherFreshPrebuiltPreservesCallerAndArgumentsWithoutBuilding(t *testing.T) {
	root := launcherFixture(t)
	buildFixture(t, root)
	caller := t.TempDir()
	output, err := runFixture(t, root, caller, "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"}, "relative target", "literal;$value")
	if err != nil {
		t.Fatalf("verified prebuilt unexpectedly built: %v\n%s", err, output)
	}
	want := "same-version|before|" + caller + "|relative target|literal;$value"
	if strings.TrimSpace(output) != want {
		t.Fatalf("caller/arguments changed: %q", output)
	}
}

func TestLauncherDetectsContentChangeWithoutVersionOrMtimeChange(t *testing.T) {
	root := launcherFixture(t)
	buildFixture(t, root)
	source := filepath.Join(root, "cmd/noise-sdlc/main.go")
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeLauncherFile(t, source, strings.Replace(string(contents), "|before|", "|after|", 1), 0o644)
	if err := os.Chtimes(source, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", nil)
	if err != nil || !strings.Contains(output, "same-version|after|") {
		t.Fatalf("stale executable selected: %v\n%s", err, output)
	}
}

func TestLauncherFingerprintsSymlinkedInputs(t *testing.T) {
	root := launcherFixture(t)
	target := filepath.Join(t.TempDir(), "input.txt")
	writeLauncherFile(t, target, "before", 0o644)
	if err := os.Symlink(target, filepath.Join(root, "internal", "input.txt")); err != nil {
		t.Fatal(err)
	}
	buildFixture(t, root)
	writeLauncherFile(t, target, "after", 0o644)
	output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"})
	if err == nil || strings.Contains(output, "same-version|") {
		t.Fatalf("changed symlink input was missed: %v\n%s", err, output)
	}
}

func TestLauncherFingerprintsBundledResources(t *testing.T) {
	root := launcherFixture(t)
	for _, name := range []string{"resources.go", "catalog/harnesses.json", "docs/schemas/harness-catalog.schema.json"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeLauncherFile(t, path, "before", 0o644)
		buildFixture(t, root)
		writeLauncherFile(t, path, "after", 0o644)
		output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"})
		if err == nil || strings.Contains(output, "same-version|") {
			t.Fatalf("changed bundled input %s was missed: %v\n%s", name, err, output)
		}
	}
}

func TestLauncherRejectsChangedInputSetOrUnverifiedPrebuilt(t *testing.T) {
	for _, scenario := range []string{"added", "removed", "binary-replaced", "receipt-missing", "receipt-malformed"} {
		t.Run(scenario, func(t *testing.T) {
			root := launcherFixture(t)
			extra := filepath.Join(root, "internal", "input.txt")
			if scenario == "removed" {
				writeLauncherFile(t, extra, "build input", 0o644)
			}
			buildFixture(t, root)
			switch scenario {
			case "added":
				writeLauncherFile(t, extra, "new build input", 0o644)
			case "removed":
				if err := os.Remove(extra); err != nil {
					t.Fatal(err)
				}
			case "binary-replaced":
				writeLauncherFile(t, filepath.Join(root, "bin/noise-sdlc-go"), "#!/bin/sh\necho WRONG-BINARY\n", 0o755)
			case "receipt-missing":
				if err := os.Remove(filepath.Join(root, "bin/noise-sdlc-go.receipt")); err != nil {
					t.Fatal(err)
				}
			case "receipt-malformed":
				writeLauncherFile(t, filepath.Join(root, "bin/noise-sdlc-go.receipt"), "not a receipt\n", 0o644)
			}
			// If fallback compilation fails, no stale/unverified binary may run.
			output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"})
			if err == nil || strings.Contains(output, "same-version|") || strings.Contains(output, "WRONG-BINARY") {
				t.Fatalf("unverified binary executed: %v\n%s", err, output)
			}
			output, err = runFixture(t, root, t.TempDir(), "noise-sdlc", nil)
			if err != nil || !strings.Contains(output, "same-version|before|") {
				t.Fatalf("source fallback failed: %v\n%s", err, output)
			}
		})
	}
}

func TestLauncherStandaloneAndPartialSourceLayouts(t *testing.T) {
	root := launcherFixture(t)
	for _, name := range []string{"go.mod", "cmd", "internal"} {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeLauncherFile(t, filepath.Join(root, "bin/noise-sdlc-go"), "#!/bin/sh\nprintf '%s\\n' standalone\n", 0o755)
	output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"})
	if err != nil || strings.TrimSpace(output) != "standalone" {
		t.Fatalf("standalone prebuilt failed: %v\n%s", err, output)
	}
	writeLauncherFile(t, filepath.Join(root, "go.mod"), "module partial\n", 0o644)
	output, err = runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"GOFLAGS=-invalid-launcher-test-flag"})
	if err == nil || strings.Contains(output, "standalone") {
		t.Fatalf("partial source was treated as standalone: %v\n%s", err, output)
	}
}

func TestLauncherCleansTemporaryBuildOnCommandFailure(t *testing.T) {
	root := launcherFixture(t)
	scratch := t.TempDir()
	output, err := runFixture(t, root, t.TempDir(), "noise-sdlc", []string{"TMPDIR=" + scratch}, "fail")
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 7 {
		t.Fatalf("command exit lost: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary artifacts remain: %v, %v", entries, err)
	}
}

func TestRuntimeVersionJSONIdentifiesTheRunningExecutable(t *testing.T) {
	output, err := captureStdout(t, func() error { return run([]string{"--version", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		SchemaVersion    int    `json:"schemaVersion"`
		Version          string `json:"version"`
		Executable       string `json:"executable"`
		ExecutableDigest string `json:"executableDigest"`
		GoVersion        string `json:"goVersion"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(result.Executable)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Version != sdlc.Version || result.GoVersion == "" || result.ExecutableDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(contents)) {
		t.Fatalf("wrong runtime provenance: %s", output)
	}
}
