package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionShowUsesAdoptedSnapshotWithoutLaunching(t *testing.T) {
	root, target := diagnosisFixture(t, "# Local instructions\n")
	// Any attempt to discover providers through PATH would fail this fixture.
	t.Setenv("PATH", t.TempDir())
	result, err := ExecutionShow(root, target, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "available" || result["runtimeVersion"] != "not-observed" || result["executionAuthority"] != "not-granted" {
		t.Fatalf("unexpected guidance: %#v", result)
	}
	roles := result["roles"].([]any)
	if len(roles) != 5 {
		t.Fatalf("lost adopted roles: %#v", roles)
	}
	for _, raw := range roles {
		if raw.(map[string]any)["state"] != "unresolved" {
			t.Fatal("re-resolved an unbound adopted role")
		}
	}
	if _, err := ExecutionShow(root, target, "invented", ""); err == nil {
		t.Fatal("unknown role accepted")
	}
	if _, err := ExecutionShow(root, target, "", "2.1.263"); err == nil {
		t.Fatal("version without role accepted")
	}
	if _, err := ExecutionShow(root, target, "review.formal", "junk"); err == nil || !strings.Contains(err.Error(), "version must") {
		t.Fatalf("invalid version did not identify the bad input: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SDLC.md"), []byte("drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecutionShow(root, target, "", ""); err == nil {
		t.Fatal("guidance trusted drifted policy")
	}
}

func TestExecutionShowRefusesCatalogSubstitution(t *testing.T) {
	root, target := diagnosisFixture(t, "# Local instructions\n")
	path := filepath.Join(target, ".sdlc/adoption.json")
	var receipt map[string]any
	if err := LoadFlexibleJSON(path, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["inputs"].(map[string]any)["catalog"] = "sha256:" + strings.Repeat("0", 64)
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := ExecutionShow(root, target, "review.formal", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "catalog-mismatch" || len(result["roles"].([]any)) != 0 {
		t.Fatalf("substituted catalog: %#v", result)
	}
	if _, err := ExecutionShow(root, target, "nonexistent", ""); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("stale catalog concealed an unknown role: %v", err)
	}
}

func TestExecutionRoleGuidancePreservesVersionAndRoute(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Role: "review.formal", Status: "resolved", Harness: stringPointer("claude"), Model: stringPointer("claude-opus-5"), ModelFamily: stringPointer("anthropic-claude-5"), Reasoning: stringPointer("high")}
	result := executionRoleGuidance(catalog, binding, "2.1.263")
	if result["state"] != "declared-contracts" || len(result["contracts"].([]any)) != 1 {
		t.Fatalf("missing declared contract: %#v", result)
	}
	if result["action"] != "review" {
		t.Fatal("formal review selected execution action")
	}
	if got := executionRoleGuidance(catalog, binding, "99.0.0"); got["state"] != "unsupported" {
		t.Fatal("substituted unrecognized version")
	}
	binding.Harness, binding.Model, binding.ModelFamily = stringPointer("codex"), stringPointer("gpt-5.6-sol"), stringPointer("openai-gpt-5.6")
	if got := executionRoleGuidance(catalog, binding, ""); got["state"] != "unsupported" {
		t.Fatal("substituted unsupported Codex route")
	}
	binding.Harness, binding.Model, binding.ModelFamily = stringPointer("claude"), stringPointer("claude-opus-5"), stringPointer("incorrect-family")
	if got := executionRoleGuidance(catalog, binding, ""); got["state"] != "unsupported" {
		t.Fatal("accepted incorrect family")
	}
}
