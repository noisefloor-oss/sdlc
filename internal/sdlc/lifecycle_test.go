package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanIsDeterministicAndAdoptionVerifies(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "harnesses@0.3", Harnesses: []HarnessObservation{}}
	first, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := CanonicalBytes(first.Plan)
	secondBytes, _ := CanonicalBytes(second.Plan)
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("identical inputs produced different plans")
	}
	if err := Adopt(target, first.Outputs); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(target, "SDLC.md")
	if err := os.WriteFile(policy, []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err == nil {
		t.Fatal("expected drift to be detected")
	}
}

func TestAdoptRejectsSymlinkedManagedParent(t *testing.T) {
	target := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(target, ".sdlc")); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, testManagedOutputs()); err == nil {
		t.Fatal("expected symlinked managed parent to fail")
	}
	if _, err := os.Stat(filepath.Join(external, "project.yaml")); !os.IsNotExist(err) {
		t.Fatal("adoption wrote through symlinked parent")
	}
}

func TestAdoptRejectsSymlinkedManagedLeaf(t *testing.T) {
	target := t.TempDir()
	external := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(external, []byte("unchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(target, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, testManagedOutputs()); err == nil {
		t.Fatal("expected symlinked managed leaf to fail")
	}
	content, err := os.ReadFile(external)
	if err != nil || string(content) != "unchanged\n" {
		t.Fatalf("external target changed: %q, %v", content, err)
	}
}

func TestAdoptRejectsParentSwapAtRenameBoundary(t *testing.T) {
	target := t.TempDir()
	external := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	err = writeManagedPathWithHook(root, target, ".sdlc/project.yaml", []byte("{}\n"), func() {
		if err := os.Rename(filepath.Join(target, ".sdlc"), filepath.Join(target, ".sdlc-original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(target, ".sdlc")); err != nil {
			t.Fatal(err)
		}
	})
	if err == nil {
		t.Fatal("expected final-boundary parent swap to fail")
	}
	if _, err := os.Stat(filepath.Join(external, "project.yaml")); !os.IsNotExist(err) {
		t.Fatal("adoption escaped the anchored project root")
	}
}

func testManagedOutputs() map[string][]byte {
	return map[string][]byte{
		"AGENTS.md": []byte("agents\n"), "SDLC.md": []byte("sdlc\n"),
		".sdlc/project.yaml": []byte("{}\n"), ".sdlc/adoption.json": []byte("{}\n"),
	}
}

func TestMalformedAdoptionReceiptFailsCleanly(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		field string
		value any
	}{
		{name: "outputs array", field: "outputs", value: []any{}},
		{name: "missing inputs", field: "inputs", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := t.TempDir()
			inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "harnesses@0.3", Harnesses: []HarnessObservation{}}
			built, err := BuildPlan(target, root, testAnswers(), &inventory)
			if err != nil {
				t.Fatal(err)
			}
			if err := Adopt(target, built.Outputs); err != nil {
				t.Fatal(err)
			}
			var receipt map[string]any
			if err := json.Unmarshal(built.Outputs[".sdlc/adoption.json"], &receipt); err != nil {
				t.Fatal(err)
			}
			receipt[test.field] = test.value
			payload, err := CanonicalBytes(receipt)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, ".sdlc", "adoption.json"), payload, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(target); err == nil {
				t.Fatal("expected malformed receipt to fail verification")
			}
			status := OnboardingStatus(target)
			if status["state"] != "repair-required" {
				t.Fatalf("got onboarding state %v, want repair-required", status["state"])
			}
		})
	}
}
