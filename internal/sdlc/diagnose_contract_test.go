package sdlc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestDiagnoseUnknownObservationsRemainUnknown(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\n"+strings.Repeat("large project instructions\n", 50000))
	if _, err := Verify(target); err != nil {
		t.Fatalf("fixture integrity must be valid: %v", err)
	}
	probe := func(string, []string) ([]byte, string) { return []byte("noise-tasks 2.6.0 (build)\n"), "" }
	result, err := diagnose(root, target, probe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Integrity != "unknown" {
		t.Fatalf("oversized intact file was misclassified: %s", result.Integrity)
	}
	diagnosisRequireCode(t, result, "integrity-unreadable")
	diagnosisRequireCode(t, result, "tasks-version-unknown")
	validateDiagnosisContract(t, root, result)
}

func TestDiagnosisSchemaRejectsReadinessClaims(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\n")
	result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	schema := validateDiagnosisContract(t, root, result)
	result.DeliveryReadiness = "ready"
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(diagnosisJSON(t, result)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(document); err == nil {
		t.Fatal("schema accepted an unsupported readiness claim")
	}
}

func validateDiagnosisContract(t *testing.T, root string, result Diagnosis) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs/schemas/diagnosis.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	const resource = "https://noise.invalid/sdlc/diagnosis.schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(resource)
	if err != nil {
		t.Fatal(err)
	}
	document, err = jsonschema.UnmarshalJSON(bytes.NewReader(diagnosisJSON(t, result)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(document); err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestSharedVerifierPreservesReadErrorsAndNamesMalformedReceipt(t *testing.T) {
	_, target := diagnosisFixture(t, "# Project\n")
	want := errors.New("injected read failure")
	_, err := verifyWithReader(target, func(relative string) ([]byte, error) {
		if relative == "AGENTS.md" {
			return nil, want
		}
		return os.ReadFile(filepath.Join(target, relative))
	})
	if !errors.Is(err, want) {
		t.Fatalf("read failure became drift or vanished: %v", err)
	}
	path := filepath.Join(target, ".sdlc/adoption.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("malformed receipt lost source path: %v", err)
	}
}
