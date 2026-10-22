package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func TestInstalledBinaryWithoutSourceTree(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := sdlc.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := sdlc.Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	for _, directory := range []string{"cmd", "internal", "catalog", "docs/schemas"} {
		if err := os.CopyFS(filepath.Join(source, directory), os.DirFS(filepath.Join(root, directory))); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum", "resources.go"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(t.TempDir(), "noise-sdlc")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/noise-sdlc")
	build.Dir = source
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	evidenceData, err := os.ReadFile(filepath.Join(root, "evidence", "sdlc-self.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(evidenceData, &evidence); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(t.TempDir(), "evidence-v1.json")
	evidence["schemaVersion"] = 1
	delete(evidence, "operations")
	evidence["update"].(map[string]any)["rollback"] = "unknown"
	writeInstalledJSON(t, legacy, evidence)
	current := filepath.Join(t.TempDir(), "evidence-v2.json")
	if err := os.WriteFile(current, evidenceData, 0o600); err != nil {
		t.Fatal(err)
	}
	review := filepath.Join(t.TempDir(), "review.json")
	writeInstalledJSON(t, review, reviewEvidenceFixture(t))
	t.Setenv("NOISE_SDLC_SOURCE_ROOT", "")
	for _, scenario := range []string{"empty", "catalog", filepath.Join("noise-sdlc", "catalog"), "ancestor"} {
		t.Run(scenario, func(t *testing.T) {
			caller := t.TempDir()
			if scenario != "empty" {
				directory := scenario
				if scenario == "ancestor" {
					directory = "catalog"
				}
				decoy := filepath.Join(caller, directory, "harnesses.json")
				if err := os.MkdirAll(filepath.Dir(decoy), 0o755); err != nil {
					t.Fatal(err)
				}
				writeInstalledJSON(t, decoy, map[string]any{"decoy": true})
				if scenario == "ancestor" {
					caller = filepath.Join(caller, "child")
					if err := os.Mkdir(caller, 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			runInstalled := func(arguments ...string) map[string]any {
				t.Helper()
				command := exec.Command(binary, arguments...)
				command.Dir = caller
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %v\n%s", arguments, err, output)
				}
				var result map[string]any
				if err := json.Unmarshal(output, &result); err != nil {
					t.Fatalf("%v: invalid JSON: %v\n%s", arguments, err, output)
				}
				return result
			}
			if result := runInstalled("check-catalog"); result["catalog"] != "valid" || result["liveCertification"] != "not-run" {
				t.Fatalf("unexpected catalog validation: %v", result)
			}
			if result := runInstalled("sync", "export"); result["catalogDigest"] != wantDigest {
				t.Fatalf("catalog digest = %v, want %s", result["catalogDigest"], wantDigest)
			}
			for version, path := range map[int]string{1: legacy, 2: current} {
				if result := runInstalled("check-evidence", path); result["validation"] != "valid" || result["recordSchemaVersion"] != float64(version) {
					t.Fatalf("unexpected evidence validation: %v", result)
				}
			}
			if result := runInstalled("review", "compare", review); result["status"] != "match" || result["approvalProof"] != "not-established" {
				t.Fatalf("unexpected review comparison: %v", result)
			}
		})
	}
}

func writeInstalledJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
