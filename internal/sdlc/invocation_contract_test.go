package sdlc

import (
	"encoding/json"
	"strings"
	"testing"
)

func testInvocationRange() VersionRange {
	return VersionRange{Invocation: &InvocationContract{
		SchemaVersion: 1,
		Actions: InvocationActions{Execution: &InvocationAction{
			Argv:             []string{"claude", "--print", "--input-format", "text", "--output-format", "text", "--model", "{model}", "--effort", "{reasoning}", "--permission-mode", "dontAsk", "--no-session-persistence"},
			WorkingDirectory: "process-cwd",
			Prompt:           InvocationPrompt{Transport: "stdin-text", MaxBytes: 262144},
			Permissions:      InvocationPermissions{Mode: "harness-policy", ExternalBoundary: "required", Limitations: []string{"The runner enforces authorized effects."}},
			Output:           InvocationOutputContract{Normalizer: "final-text-v1", MaxBytes: 1048576},
			Completion:       InvocationCompletion{SuccessExitCode: 0, RequiresFinalOutput: true, TaskCompletion: "not-established"},
		}},
	}}
}

func TestInvocationContractRejectsAmbiguousOrUnsafeActions(t *testing.T) {
	harness := Harness{ID: "claude", Executable: "claude"}
	for _, test := range []struct {
		name   string
		mutate func(*VersionRange)
	}{
		{"unknown-version", func(r *VersionRange) { r.Invocation.SchemaVersion = 2 }},
		{"no-actions", func(r *VersionRange) { r.Invocation.Actions = InvocationActions{} }},
		{"wrong-executable", func(r *VersionRange) { r.Invocation.Actions.Execution.Argv[0] = "bash" }},
		{"shell-text", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "$(provider)")
		}},
		{"control-text", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "line\nline")
		}},
		{"embedded-placeholder", func(r *VersionRange) { r.Invocation.Actions.Execution.Argv[7] = "--model={model}" }},
		{"unknown-placeholder", func(r *VersionRange) { r.Invocation.Actions.Execution.Argv[7] = "{secret}" }},
		{"unknown-option", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "--made-up")
		}},
		{"permission-bypass", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "--dangerously-skip-permissions")
		}},
		{"literal-model-override", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "--model", "other")
		}},
		{"duplicate-model", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "{model}")
		}},
		{"missing-reasoning", func(r *VersionRange) { r.Invocation.Actions.Execution.Argv[9] = "high" }},
		{"ambiguous-cwd", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "{workspace}")
		}},
		{"missing-cwd", func(r *VersionRange) { r.Invocation.Actions.Execution.WorkingDirectory = "argv" }},
		{"unknown-cwd", func(r *VersionRange) { r.Invocation.Actions.Execution.WorkingDirectory = "inherit" }},
		{"unbounded-prompt", func(r *VersionRange) { r.Invocation.Actions.Execution.Prompt.MaxBytes = 0 }},
		{"oversized-prompt", func(r *VersionRange) { r.Invocation.Actions.Execution.Prompt.MaxBytes = MaxInvocationPromptBytes + 1 }},
		{"unknown-transport", func(r *VersionRange) { r.Invocation.Actions.Execution.Prompt.Transport = "shell" }},
		{"missing-prompt-file", func(r *VersionRange) { r.Invocation.Actions.Execution.Prompt.Transport = "at-file" }},
		{"mixed-prompt-inputs", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Argv = append(r.Invocation.Actions.Execution.Argv, "@{promptFile}")
		}},
		{"no-boundary", func(r *VersionRange) { r.Invocation.Actions.Execution.Permissions.ExternalBoundary = "none" }},
		{"no-limitations", func(r *VersionRange) { r.Invocation.Actions.Execution.Permissions.Limitations = nil }},
		{"duplicate-limitations", func(r *VersionRange) {
			r.Invocation.Actions.Execution.Permissions.Limitations = []string{"same", "same"}
		}},
		{"unknown-output", func(r *VersionRange) { r.Invocation.Actions.Execution.Output.Normalizer = "all-events" }},
		{"unbounded-output", func(r *VersionRange) { r.Invocation.Actions.Execution.Output.MaxBytes = 0 }},
		{"nonzero-success", func(r *VersionRange) { r.Invocation.Actions.Execution.Completion.SuccessExitCode = 1 }},
		{"no-final-output", func(r *VersionRange) { r.Invocation.Actions.Execution.Completion.RequiresFinalOutput = false }},
		{"claims-task-completion", func(r *VersionRange) { r.Invocation.Actions.Execution.Completion.TaskCompletion = "done" }},
		{"execution-publication", func(r *VersionRange) { r.Invocation.Actions.Execution.Publication = &InvocationPublication{} }},
		{"review-without-readback", func(r *VersionRange) {
			r.Invocation.Actions.Review = r.Invocation.Actions.Execution
			r.Invocation.Actions.Execution = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			versionRange := testInvocationRange()
			test.mutate(&versionRange)
			if err := ValidateInvocationContract(harness, versionRange); err == nil {
				t.Fatal("invalid invocation contract accepted")
			}
		})
	}
}

func TestInvocationContractOptionalFieldPreservesLegacyJSON(t *testing.T) {
	if err := ValidateInvocationContract(Harness{}, VersionRange{}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(VersionRange{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "invocation") {
		t.Fatal("absent invocation changed legacy range JSON")
	}
}

func TestInvocationContractObservedCatalogRanges(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Catalog != "noise/harnesses@0.5" {
		t.Fatalf("invocation catalog lacks its new identity: %s", catalog.Catalog)
	}
	for _, test := range []struct{ harness, oldVersion, newVersion, cwd, transport string }{
		{"claude", "2.1.232", "2.1.263", "process-cwd", "stdin-text"},
		{"omp", "18.0.7", "18.1.11", "argv", "at-file"},
	} {
		t.Run(test.harness, func(t *testing.T) {
			harness := findHarnessByID(t, catalog, test.harness)
			old := matchingRange(harness, test.oldVersion)
			if old == nil || old.Invocation != nil {
				t.Fatal("legacy range lost or acquired unobserved invocation claims")
			}
			r := matchingRange(harness, test.newVersion)
			if r == nil || r.Invocation == nil || r.Status != "experimental" || r.Certification.Live != "missing" || r.Certification.Deck != "missing" || len(r.Certification.TestedVersions) != 0 {
				t.Fatal("observed invocation must remain experimental without invented certification")
			}
			if err := ValidateInvocationContract(harness, *r); err != nil {
				t.Fatal(err)
			}
			for _, action := range []*InvocationAction{r.Invocation.Actions.Execution, r.Invocation.Actions.Review} {
				if action == nil || action.WorkingDirectory != test.cwd || action.Prompt.Transport != test.transport || action.Output.Normalizer != "final-text-v1" {
					t.Fatal("observed action does not match its declared transport")
				}
			}
			if r.Invocation.Actions.Execution.Publication != nil || r.Invocation.Actions.Review.Publication == nil || !r.Invocation.Actions.Review.Publication.RequiresReadback {
				t.Fatal("publication is not separated from execution and provider output")
			}
		})
	}
}

func TestInvocationContractSchemaRejectsMissingAndUnknownFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing-actions", func(c map[string]any) { delete(c, "actions") }},
		{"unknown-action", func(c map[string]any) { c["actions"].(map[string]any)["deploy"] = map[string]any{} }},
		{"missing-prompt-bound", func(c map[string]any) {
			delete(c["actions"].(map[string]any)["execution"].(map[string]any)["prompt"].(map[string]any), "maxBytes")
		}},
		{"unknown-prompt-field", func(c map[string]any) {
			c["actions"].(map[string]any)["execution"].(map[string]any)["prompt"].(map[string]any)["command"] = "cat"
		}},
		{"execution-with-publication", func(c map[string]any) {
			c["actions"].(map[string]any)["execution"].(map[string]any)["publication"] = map[string]any{}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, path, document, harness := schemaCatalogFixture(t, true)
			harness["id"] = "claude"
			harness["executable"] = "claude"
			harness["versionProbe"].(map[string]any)["argv"] = []string{"claude", "--version"}
			harness["enumerationProbe"] = nil
			encoded, err := json.Marshal(testInvocationRange().Invocation)
			if err != nil {
				t.Fatal(err)
			}
			var contract map[string]any
			if err := json.Unmarshal(encoded, &contract); err != nil {
				t.Fatal(err)
			}
			schemaRange(harness)["invocation"] = contract
			writeSchemaFixture(t, path, document)
			if _, err := LoadCatalog(root); err != nil {
				t.Fatalf("valid invocation rejected: %v", err)
			}
			test.mutate(contract)
			writeSchemaFixture(t, path, document)
			if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), "schema violation") {
				t.Fatalf("invalid invocation did not fail schema validation: %v", err)
			}
		})
	}
}
