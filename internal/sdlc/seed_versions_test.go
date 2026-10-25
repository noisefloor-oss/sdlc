package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	resources "github.com/noisefloor-oss/sdlc"
)

func TestSeedVersionsProduceVerifiablePinnedAdoptions(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Harnesses: []HarnessObservation{}}
	for _, id := range []string{"noise/agent@0.1", "noise/agent@0.2"} {
		t.Run(id, func(t *testing.T) {
			answers := testAnswers()
			answers.Seed = id
			target := t.TempDir()
			built, err := BuildPlan(target, root, answers, &inventory)
			if err != nil {
				t.Fatal(err)
			}
			if err := Adopt(target, built.Outputs); err != nil {
				t.Fatal(err)
			}
			verified, err := Verify(target)
			if err != nil {
				t.Fatal(err)
			}
			if verified["seed"] != id {
				t.Fatalf("adoption selected %v, want %s", verified["seed"], id)
			}
			var project map[string]any
			if err := json.Unmarshal(built.Outputs[".sdlc/project.yaml"], &project); err != nil {
				t.Fatal(err)
			}
			if project["seed"] != id {
				t.Fatalf("project seed %v, want %s", project["seed"], id)
			}
		})
	}
}

func TestSeedVersionLoadingRejectsUnknownAndSourceMismatch(t *testing.T) {
	for _, id := range []string{"noise/agent@0.3", "../../seed", ""} {
		if _, err := LoadSeedVersion("", id); err == nil {
			t.Fatalf("accepted unknown seed %q", id)
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "catalog", "packs")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	legacy, err := LoadSeedVersion("", "noise/agent@0.1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "noise-agent-0.2.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeed(root); err == nil {
		t.Fatal("source version mismatch silently accepted")
	}
}

func TestLegacySeedDigestRemainsPinned(t *testing.T) {
	seed, err := LoadSeedVersion("", "noise/agent@0.1")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(seed)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:f51c8168bcddfb806e469c2467c2ee0fa6ab0ff9c118ce3b30870807050fd457" {
		t.Fatalf("immutable legacy seed changed: %s", digest)
	}
}

func TestCatalogCheckRejectsDamagedLegacySeed(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, resources.Files); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAll(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "catalog", "packs", "noise-agent-0.1.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAll(root); err == nil {
		t.Fatal("catalog check ignored damaged legacy seed")
	}
}
