package sdlc

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBootstrapCreatesSeedOnceAndReportsPending(t *testing.T) {
	target := t.TempDir()
	prd := filepath.Join(target, "PRD.md")
	if err := os.WriteFile(prd, []byte("authoritative\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := Bootstrap(target)
	if err != nil {
		t.Fatal(err)
	}
	if created["state"] != "bootstrapped" {
		t.Fatalf("unexpected bootstrap state: %#v", created)
	}
	paths := []string{"AGENTS.md", BootstrapFilename, BootstrapReceiptPath}
	before := make(map[string][]byte, len(paths))
	for _, relative := range paths {
		content, err := os.ReadFile(filepath.Join(target, relative))
		if err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
		before[relative] = content
	}
	if !strings.HasPrefix(string(before["AGENTS.md"]), BootstrapBlockBegin) {
		t.Fatal("bootstrap block is not at byte zero")
	}
	status := OnboardingStatus(target)
	if status["state"] != "offer-selection" || status["reason"] != "bootstrap-pending" {
		t.Fatalf("unexpected onboarding status: %#v", status)
	}

	repeated, err := Bootstrap(target)
	if err != nil {
		t.Fatal(err)
	}
	if repeated["state"] != "already-bootstrapped" {
		t.Fatalf("unexpected repeat state: %#v", repeated)
	}
	for _, relative := range paths {
		after, err := os.ReadFile(filepath.Join(target, relative))
		if err != nil || string(after) != string(before[relative]) {
			t.Fatalf("repeat changed %s", relative)
		}
	}
	content, err := os.ReadFile(prd)
	if err != nil || string(content) != "authoritative\n" {
		t.Fatalf("bootstrap changed project content: %q, %v", content, err)
	}
}

func TestBootstrapPreservesExistingAgentsAndAdoptConsumesIt(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	projectAgents := "# Project instructions\n\nKeep this prose.\n"
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), []byte(projectAgents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if !planHasAction(built.Plan, BootstrapFilename, "consume-bootstrap") || !planHasAction(built.Plan, BootstrapReceiptPath, "consume-bootstrap-receipt") {
		t.Fatalf("plan does not disclose bootstrap cleanup: %#v", built.Plan["files"])
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{BootstrapFilename, BootstrapReceiptPath} {
		if _, err := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(err) {
			t.Fatalf("adoption left %s behind", relative)
		}
	}
	agents, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(agents), BlockBegin) || strings.Contains(string(agents), BootstrapBlockBegin) || !strings.Contains(string(agents), projectAgents) {
		t.Fatalf("unexpected adopted AGENTS.md: %s", agents)
	}
	if _, err := Verify(target); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAdoption(target); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
	if err != nil || string(restored) != projectAgents {
		t.Fatalf("removal did not restore project prose: %q, %v", restored, err)
	}
}

func TestBootstrapCreatedAgentsOwnershipSurvivesAdoption(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAdoption(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(target, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("remove retained AGENTS.md that bootstrap originally created")
	}
}

func TestAdoptRejectsBootstrapChangedAfterPlanning(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, BootstrapFilename), []byte("changed after plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("expected changed bootstrap to reject adoption, got %v", err)
	}
	for _, relative := range []string{"SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if _, err := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(err) {
			t.Fatalf("rejected adoption wrote %s", relative)
		}
	}
}

func TestAdoptRejectsBootstrapAddedAfterPlanning(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err == nil || !strings.Contains(err.Error(), "added after planning") {
		t.Fatalf("expected new bootstrap to invalidate the plan, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "SDLC.md")); !os.IsNotExist(err) {
		t.Fatal("rejected adoption wrote SDLC.md")
	}
}

func TestDirectAdoptionPreservesUserOwnedBootstrapFile(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	path := filepath.Join(target, BootstrapFilename)
	if err := os.WriteFile(path, []byte("user-owned bootstrap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := OnboardingStatus(target)
	if status["state"] != "offer-selection" || status["reason"] != "no-adoption" {
		t.Fatalf("user-owned bootstrap confused onboarding: %#v", status)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "user-owned bootstrap\n" {
		t.Fatalf("direct adoption changed user-owned bootstrap: %q, %v", content, err)
	}
}

func TestBootstrapRejectsDriftAndNeverRecreatesMissingState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) error
	}{
		{name: "edited document", mutate: func(target string) error {
			return os.WriteFile(filepath.Join(target, BootstrapFilename), []byte("operator edit\n"), 0o644)
		}},
		{name: "missing document", mutate: func(target string) error {
			return os.Remove(filepath.Join(target, BootstrapFilename))
		}},
		{name: "edited agents block", mutate: func(target string) error {
			path := filepath.Join(target, "AGENTS.md")
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(path, []byte(strings.Replace(string(content), "Noise SDLC onboarding", "Changed onboarding", 1)), 0o644)
		}},
		{name: "missing receipt", mutate: func(target string) error {
			return os.Remove(filepath.Join(target, BootstrapReceiptPath))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := t.TempDir()
			if _, err := Bootstrap(target); err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(target); err != nil {
				t.Fatal(err)
			}
			if _, err := Bootstrap(target); err == nil {
				t.Fatal("expected changed bootstrap state to be rejected")
			}
			status := OnboardingStatus(target)
			if status["state"] != "repair-required" || status["reason"] != "invalid-bootstrap" {
				t.Fatalf("unexpected drift status: %#v", status)
			}
		})
	}
}

func TestBootstrapRejectsExistingFileAndSymlink(t *testing.T) {
	t.Run("invalid UTF-8 agents", func(t *testing.T) {
		target := t.TempDir()
		path := filepath.Join(target, "AGENTS.md")
		original := []byte{0xff, 0xfe, '\n'}
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Bootstrap(target); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
			t.Fatalf("expected invalid UTF-8 AGENTS.md to fail, got %v", err)
		}
		content, _ := os.ReadFile(path)
		if string(content) != string(original) {
			t.Fatal("bootstrap changed invalid UTF-8 AGENTS.md")
		}
	})

	t.Run("user-owned bootstrap", func(t *testing.T) {
		target := t.TempDir()
		path := filepath.Join(target, BootstrapFilename)
		if err := os.WriteFile(path, []byte("user owned\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Bootstrap(target); err == nil {
			t.Fatal("expected user-owned BOOTSTRAP.md collision to fail")
		}
		content, _ := os.ReadFile(path)
		if string(content) != "user owned\n" {
			t.Fatal("bootstrap overwrote user-owned content")
		}
	})

	t.Run("symlinked bootstrap", func(t *testing.T) {
		target := t.TempDir()
		external := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(external, []byte("unchanged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(target, BootstrapFilename)); err != nil {
			t.Fatal(err)
		}
		if _, err := Bootstrap(target); err == nil {
			t.Fatal("expected symlinked bootstrap path to fail")
		}
		content, _ := os.ReadFile(external)
		if string(content) != "unchanged\n" {
			t.Fatal("bootstrap wrote through symlink")
		}
	})

	t.Run("symlinked receipt parent", func(t *testing.T) {
		target := t.TempDir()
		external := t.TempDir()
		if err := os.Symlink(external, filepath.Join(target, ".sdlc")); err != nil {
			t.Fatal(err)
		}
		if _, err := Bootstrap(target); err == nil {
			t.Fatal("expected symlinked receipt parent to fail")
		}
		if _, err := os.Lstat(filepath.Join(external, "bootstrap.json")); !os.IsNotExist(err) {
			t.Fatal("bootstrap wrote through symlinked receipt parent")
		}
	})
}

func TestBootstrapRejectsAdoptedTarget(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	if _, err := Bootstrap(target); err == nil {
		t.Fatal("expected adopted target to reject bootstrap")
	}
}

func TestBootstrappedPlanConformsToCommittedFileSchema(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	content, err := os.ReadFile(filepath.Join(root, "docs", "schemas", "plan.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatal(err)
	}
	fileSchema := schema["$defs"].(map[string]any)["file"].(map[string]any)
	properties := fileSchema["properties"].(map[string]any)
	pathPattern := properties["path"].(map[string]any)["pattern"].(string)
	pathExpression, err := regexp.Compile(pathPattern)
	if err != nil {
		t.Fatal(err)
	}
	actions := make(map[string]bool)
	for _, value := range properties["action"].(map[string]any)["enum"].([]any) {
		actions[value.(string)] = true
	}
	for _, file := range built.Plan["files"].([]map[string]any) {
		path := file["path"].(string)
		action := file["action"].(string)
		if !pathExpression.MatchString(path) || !actions[action] {
			t.Fatalf("generated plan file violates committed schema: path=%q action=%q", path, action)
		}
	}
}

func TestBootstrapRejectsAgentsChangeAtFinalWriteBoundary(t *testing.T) {
	target := t.TempDir()
	path := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := false
	_, err := bootstrapWithHook(target, func(stage, relative string) error {
		if !changed && stage == "before-write" && relative == "AGENTS.md" {
			changed = true
			return os.WriteFile(path, []byte("concurrent user change\n"), 0o644)
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "atomic boundary") {
		t.Fatalf("expected write-boundary conflict, got %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "concurrent user change\n" {
		t.Fatalf("bootstrap overwrote concurrent AGENTS.md: %q, %v", content, readErr)
	}
	for _, relative := range []string{BootstrapFilename, BootstrapReceiptPath} {
		if _, statErr := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(statErr) {
			t.Fatalf("rejected bootstrap wrote %s", relative)
		}
	}
}

func TestAdoptRejectsBootstrapReplacementAtCleanupBoundary(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	err = adoptWithHook(target, built.Outputs, func(stage, relative string) error {
		if !changed && stage == "before-remove" && relative == BootstrapFilename {
			changed = true
			return os.WriteFile(filepath.Join(target, relative), []byte("concurrent replacement\n"), 0o644)
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "atomic boundary") {
		t.Fatalf("expected cleanup-boundary conflict, got %v", err)
	}
	content, readErr := os.ReadFile(filepath.Join(target, BootstrapFilename))
	if readErr != nil || string(content) != "concurrent replacement\n" {
		t.Fatalf("adoption deleted concurrent bootstrap replacement: %q, %v", content, readErr)
	}
	for _, relative := range []string{"SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if _, statErr := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(statErr) {
			t.Fatalf("rejected adoption retained %s", relative)
		}
	}
}

func TestAdoptionReceiptCommitsOnlyAfterBootstrapCleanup(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("simulated interruption before commit marker")
	err = adoptWithHook(target, built.Outputs, func(stage, relative string) error {
		if stage != "before-write" || relative != ".sdlc/adoption.json" {
			return nil
		}
		for _, bootstrapPath := range []string{BootstrapFilename, BootstrapReceiptPath} {
			if _, statErr := os.Lstat(filepath.Join(target, bootstrapPath)); !os.IsNotExist(statErr) {
				t.Fatalf("commit marker staged before %s cleanup", bootstrapPath)
			}
		}
		if _, statErr := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(statErr) {
			t.Fatal("adoption receipt existed before the commit-marker write")
		}
		return interrupted
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("expected simulated interruption, got %v", err)
	}
	for _, falseFailure := range []string{"cannot remove transaction backup", "transaction backups were retained"} {
		if strings.Contains(err.Error(), falseFailure) {
			t.Fatalf("clean rollback reported false recovery residue: %v", err)
		}
	}
	for _, relative := range []string{BootstrapFilename, BootstrapReceiptPath} {
		if _, statErr := os.Lstat(filepath.Join(target, relative)); statErr != nil {
			t.Fatalf("rollback did not restore %s: %v", relative, statErr)
		}
	}
	for _, relative := range []string{"SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if _, statErr := os.Lstat(filepath.Join(target, relative)); !os.IsNotExist(statErr) {
			t.Fatalf("interrupted adoption retained %s", relative)
		}
	}
	backups, globErr := filepath.Glob(filepath.Join(target, ".noise-sdlc-*"))
	if globErr != nil || len(backups) != 0 {
		t.Fatalf("clean rollback left transaction artifacts: %v, %v", backups, globErr)
	}
	sdlcBackups, globErr := filepath.Glob(filepath.Join(target, ".sdlc", ".noise-sdlc-*"))
	if globErr != nil || len(sdlcBackups) != 0 {
		t.Fatalf("clean rollback left .sdlc transaction artifacts: %v, %v", sdlcBackups, globErr)
	}
}

func TestDirectAdoptionRejectsAgentsChangedAfterPlanning(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	path := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(path, []byte("planned prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new user prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err == nil || !strings.Contains(err.Error(), "AGENTS.md changed after planning") {
		t.Fatalf("expected stale direct-adoption plan to fail, got %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "new user prose\n" {
		t.Fatalf("direct adoption overwrote new prose: %q, %v", content, readErr)
	}
}

func TestDirectAdoptionRejectsAgentsCreatedAfterPlanning(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(path, []byte("new user file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err == nil || !strings.Contains(err.Error(), "AGENTS.md changed after planning") {
		t.Fatalf("expected newly created AGENTS.md to invalidate plan, got %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != "new user file\n" {
		t.Fatalf("direct adoption overwrote new AGENTS.md: %q, %v", content, readErr)
	}
}

func TestBootstrapRollbackPreservesConcurrentEditToInstalledAgents(t *testing.T) {
	target := t.TempDir()
	agentsPath := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("original prose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("stop after concurrent edit")
	_, err := bootstrapWithHook(target, func(stage, relative string) error {
		if stage == "before-write" && relative == BootstrapFilename {
			if writeErr := os.WriteFile(agentsPath, []byte("concurrent edit to installed agents\n"), 0o644); writeErr != nil {
				return writeErr
			}
			return interrupted
		}
		return nil
	})
	if !errors.Is(err, interrupted) || !strings.Contains(err.Error(), "rollback preserved concurrent changes") || !strings.Contains(err.Error(), "original retained at") {
		t.Fatalf("expected surfaced rollback conflict, got %v", err)
	}
	content, readErr := os.ReadFile(agentsPath)
	if readErr != nil || string(content) != "concurrent edit to installed agents\n" {
		t.Fatalf("rollback overwrote concurrent edit: %q, %v", content, readErr)
	}
	backups, globErr := filepath.Glob(filepath.Join(target, ".noise-sdlc-backup-*"))
	if globErr != nil || len(backups) != 1 {
		t.Fatalf("expected one retained original backup, got %v, %v", backups, globErr)
	}
	original, readErr := os.ReadFile(backups[0])
	if readErr != nil || string(original) != "original prose\n" {
		t.Fatalf("retained backup lost original prose: %q, %v", original, readErr)
	}
}

func planHasAction(plan map[string]any, path, action string) bool {
	for _, raw := range plan["files"].([]map[string]any) {
		if raw["path"] == path && raw["action"] == action {
			return true
		}
	}
	return false
}
