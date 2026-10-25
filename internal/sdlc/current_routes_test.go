package sdlc

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCurrentKimiAndGLMRoutesMatchCatalogContracts(t *testing.T) {
	if Version != "0.4.1" {
		t.Fatalf("public release must report version 0.4.1, got %q", Version)
	}
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Catalog != "noise/harnesses@0.6" {
		t.Fatalf("unexpected catalog id %q", catalog.Catalog)
	}
	template, err := LoadTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if template.ID != ExecutionTemplateID {
		t.Fatalf("unexpected template id %q", template.ID)
	}

	expected := map[string]struct {
		minimum, maximum, model string
		run                     []string
	}{
		"kimi": {
			minimum: "0.39.0", maximum: "0.39.1", model: "kimi-code/k3",
			run: []string{"kimi", "--model", "{model}", "--output-format", "stream-json", "--prompt", "{prompt}"},
		},
		"omp": {
			minimum: "18.0.7", maximum: "18.0.8", model: "zai/glm-5.3",
			run: []string{"omp", "--print", "--mode", "json", "--no-session", "--approval-mode", "write", "--model", "{model}", "--thinking", "{reasoning}", "--cwd", "{workspace}", "{prompt}"},
		},
		"opencode": {
			minimum: "1.18.23", maximum: "1.18.24", model: "opencode-go/glm-5.3",
			run: []string{"opencode", "run", "--auto", "--format", "json", "--model", "{model}", "--variant", "{reasoning}", "--dir", "{workspace}", "{prompt}"},
		},
		"pi": {
			minimum: "0.84.3", maximum: "0.84.4", model: "zai/glm-5.3",
			run: []string{"pi", "--print", "--mode", "json", "--no-session", "--no-extensions", "--model", "{model}", "--thinking", "{reasoning}", "{prompt}"},
		},
	}
	for harnessID, want := range expected {
		harness := findHarnessByID(t, catalog, harnessID)
		wantRanges := 1
		if harnessID == "omp" {
			wantRanges = 2
		}
		if len(harness.Ranges) != wantRanges || harness.Ranges[0].Minimum != want.minimum || harness.Ranges[0].MaximumExclusive != want.maximum {
			t.Fatalf("%s has unexpected version range: %#v", harnessID, harness.Ranges)
		}
		if !reflect.DeepEqual(harness.Ranges[0].Run, want.run) {
			t.Fatalf("%s exact run argv drifted:\n got %#v\nwant %#v", harnessID, harness.Ranges[0].Run, want.run)
		}
		if harnessID == "omp" && (harness.Ranges[1].Minimum != "18.1.11" || harness.Ranges[1].MaximumExclusive != "18.1.12" || harness.Ranges[1].Invocation == nil) {
			t.Fatal("OMP invocation release must retain a separate exact range")
		}
		model := findModel(harness, want.model)
		if model == nil || model.ModelFamily == "zai-glm-5.2" {
			t.Fatalf("%s lacks current model %q: %#v", harnessID, want.model, harness.Models)
		}
	}
	for _, role := range template.Roles {
		for _, candidate := range role.Candidates {
			if candidate.Model == "zai/glm-5.2" || candidate.Model == "zai-coding-plan/glm-5.2" {
				t.Fatalf("%s retained stale GLM 5.2 candidate: %#v", role.ID, candidate)
			}
		}
	}
	assertRoleCandidates(t, template, "implementation.high", []Candidate{
		{Harness: "claude", Model: "claude-opus-5"},
		{Harness: "omp", Model: "zai/glm-5.3"},
		{Harness: "codex", Model: "gpt-5.6-sol"},
		{Harness: "opencode", Model: "opencode-go/glm-5.3"},
		{Harness: "pi", Model: "zai/glm-5.3"},
	})
	assertRoleCandidates(t, template, "review.formal", []Candidate{
		{Harness: "codex", Model: "gpt-5.6-sol"},
		{Harness: "claude", Model: "claude-opus-5"},
		{Harness: "omp", Model: "zai/glm-5.3"},
		{Harness: "opencode", Model: "opencode-go/glm-5.3"},
		{Harness: "pi", Model: "zai/glm-5.3"},
	})
	for _, roleID := range []string{"implementation.bulk", "implementation.standard", "implementation.high", "review.formal", "repair"} {
		assertRoleLacksCandidate(t, template, roleID, Candidate{Harness: "kimi", Model: "kimi-code/k3"})
	}
	for _, roleID := range []string{"implementation.bulk", "implementation.standard", "implementation.high", "review.formal"} {
		assertRoleCandidate(t, template, roleID, Candidate{Harness: "omp", Model: "zai/glm-5.3"})
	}
}

func assertRoleCandidates(t *testing.T, template ExecutionTemplate, roleID string, candidates []Candidate) {
	t.Helper()
	for _, role := range template.Roles {
		if role.ID == roleID {
			if !reflect.DeepEqual(role.Candidates, candidates) {
				t.Fatalf("%s candidate order drifted:\n got %#v\nwant %#v", roleID, role.Candidates, candidates)
			}
			return
		}
	}
	t.Fatalf("missing role %s", roleID)
}

func TestCurrentInstalledVersionsAndSafeEnumerationAreRecognized(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	writeRouteProbe(t, directory, "kimi", "0.39.0", nil, "")
	writeRouteProbe(t, directory, "omp", "omp/18.0.7", []string{"models", "--json", "--no-extensions"}, `{"models":[{"selector":"zai/glm-5.3"}]}`)
	writeRouteProbe(t, directory, "opencode", "1.18.23", []string{"models", "--pure"}, "opencode-go/glm-5.3")
	writeRouteProbe(t, directory, "pi", "0.84.3", nil, "")
	t.Setenv("PATH", directory)
	declarations := []OperatorCapability{
		{Harness: "kimi", Model: "kimi-code/k3", Entitlement: "included", AllowUnverified: true},
		{Harness: "pi", Model: "zai/glm-5.3", Entitlement: "unknown", AllowUnverified: true},
	}
	inventory := DiscoverInventory(catalog, declarations, directory)
	for _, harnessID := range []string{"kimi", "omp", "opencode", "pi"} {
		observation := findHarnessObservation(t, inventory, harnessID)
		if observation.RangeStatus != "experimental" || observation.Version == nil {
			t.Fatalf("%s installed version was not matched: %#v", harnessID, observation)
		}
	}
	assertObservedModel(t, findHarnessObservation(t, inventory, "kimi"), "kimi-code/k3", "operator-declared")
	assertObservedModel(t, findHarnessObservation(t, inventory, "omp"), "zai/glm-5.3", "enumerated")
	assertObservedModel(t, findHarnessObservation(t, inventory, "opencode"), "opencode-go/glm-5.3", "enumerated")
	assertObservedModel(t, findHarnessObservation(t, inventory, "pi"), "zai/glm-5.3", "operator-declared")
}

func TestCurrentRoutesPreserveIndependentFormalReview(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	template, err := LoadTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	available := []struct{ harness, model string }{
		{"command-code", "deepseek/deepseek-v4-pro"},
		{"opencode", "opencode-go/deepseek-v4-pro"},
		{"omp", "zai/glm-5.3"},
		{"claude", "claude-opus-5"},
		{"kimi", "kimi-code/k3"},
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: catalog.Catalog}
	answers := testAnswers()
	answers.ExecutionTemplate = ExecutionTemplateID
	for _, route := range available {
		harness := findHarnessByID(t, catalog, route.harness)
		model := findModel(harness, route.model)
		if model == nil {
			t.Fatalf("missing %s/%s", route.harness, route.model)
		}
		version := harness.Ranges[0].Minimum
		inventory.Harnesses = append(inventory.Harnesses, HarnessObservation{
			ID: route.harness, Version: &version,
			Models: []ModelObservation{resolverObservation(*model, model.Entitlement)},
		})
		answers.OperatorCapabilities = append(answers.OperatorCapabilities, OperatorCapability{
			Harness: route.harness, Model: route.model, Entitlement: model.Entitlement, AllowUnverified: true,
		})
	}
	bindings := ResolveBindings(catalog, template, inventory, answers, "balanced")
	assertBinding(t, bindings, "implementation.standard", "opencode", "opencode-go/deepseek-v4-pro")
	assertBinding(t, bindings, "implementation.high", "claude", "claude-opus-5")
	assertBinding(t, bindings, "review.formal", "omp", "zai/glm-5.3")
}

func findHarnessByID(t *testing.T, catalog Catalog, id string) Harness {
	t.Helper()
	for _, harness := range catalog.Harnesses {
		if harness.ID == id {
			return harness
		}
	}
	t.Fatalf("missing harness %s", id)
	return Harness{}
}

func assertRoleCandidate(t *testing.T, template ExecutionTemplate, roleID string, candidate Candidate) {
	t.Helper()
	for _, role := range template.Roles {
		if role.ID == roleID {
			for _, actual := range role.Candidates {
				if actual == candidate {
					return
				}
			}
		}
	}
	t.Fatalf("%s lacks candidate %#v", roleID, candidate)
}

func assertRoleLacksCandidate(t *testing.T, template ExecutionTemplate, roleID string, candidate Candidate) {
	t.Helper()
	for _, role := range template.Roles {
		if role.ID != roleID {
			continue
		}
		for _, actual := range role.Candidates {
			if actual == candidate {
				t.Fatalf("%s unexpectedly routes unenforceable candidate %#v", roleID, candidate)
			}
		}
		return
	}
	t.Fatalf("missing role %s", roleID)
}

func writeRouteProbe(t *testing.T, directory, executable, version string, enumerationArgs []string, models string) {
	t.Helper()
	body := "#!/bin/sh\nif [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ]; then printf '%s\\n' '" + version + "'; exit 0; fi\n"
	if enumerationArgs != nil {
		condition := fmt.Sprintf("[ \"$#\" -eq %d ]", len(enumerationArgs))
		for index, argument := range enumerationArgs {
			condition += fmt.Sprintf(" && [ \"$%d\" = %q ]", index+1, argument)
		}
		body += "if " + condition + "; then printf '%s\\n' '" + models + "'; exit 0; fi\nexit 64\n"
	} else {
		body += "exit 2\n"
	}
	if err := os.WriteFile(filepath.Join(directory, executable), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func findHarnessObservation(t *testing.T, inventory Inventory, id string) HarnessObservation {
	t.Helper()
	for _, observation := range inventory.Harnesses {
		if observation.ID == id {
			return observation
		}
	}
	t.Fatalf("missing inventory observation %s", id)
	return HarnessObservation{}
}

func assertObservedModel(t *testing.T, observation HarnessObservation, modelID, provenance string) {
	t.Helper()
	for _, model := range observation.Models {
		if model.ID == modelID && model.Provenance == provenance {
			return
		}
	}
	t.Fatalf("%s lacks %s with provenance %s: %#v", observation.ID, modelID, provenance, observation.Models)
}

func assertBinding(t *testing.T, bindings []Binding, role, harness, model string) {
	t.Helper()
	for _, binding := range bindings {
		if binding.Role == role {
			if binding.Status != "resolved" || pointerValue(binding.Harness) != harness || pointerValue(binding.Model) != model {
				t.Fatalf("unexpected %s binding: %#v", role, binding)
			}
			return
		}
	}
	t.Fatalf("missing binding %s", role)
}
