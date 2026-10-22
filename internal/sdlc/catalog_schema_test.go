package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func schemaCatalogFixture(t *testing.T, fragment bool) (string, string, map[string]any, map[string]any) {
	t.Helper()
	repository, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, directory := range []string{"catalog/harnesses.d", "docs/schemas"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile(filepath.Join(repository, "docs/schemas/harness-catalog.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/schemas/harness-catalog.schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := LoadFlexibleJSON(filepath.Join(repository, "catalog/harnesses.json"), &original); err != nil {
		t.Fatal(err)
	}
	harness := original["harnesses"].([]any)[0].(map[string]any)
	document := map[string]any{"schemaVersion": 1, "catalog": "noise/harnesses@0.4", "harnesses": []any{harness}}
	path := filepath.Join(root, "catalog/harnesses.json")
	writeSchemaFixture(t, filepath.Join(root, "catalog/harness-candidates.json"), map[string]any{"schemaVersion": 1, "candidates": []any{}})
	if fragment {
		document["harnesses"] = []any{}
		writeSchemaFixture(t, path, document)
		path = filepath.Join(root, "catalog/harnesses.d/fixture.json")
		document = harness
	}
	writeSchemaFixture(t, path, document)
	return root, path, document, harness
}

func writeSchemaFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func schemaRange(harness map[string]any) map[string]any {
	return harness["ranges"].([]any)[0].(map[string]any)
}

func TestCatalogSchemaRequiredNullableFields(t *testing.T) {
	for _, fragment := range []bool{false, true} {
		for _, key := range []string{"enumerationProbe", "modelFlag", "reasoningFlag", "structuredOutput", "certifiedAt", "resultDigest"} {
			t.Run(map[bool]string{false: "base", true: "fragment"}[fragment]+"/"+key, func(t *testing.T) {
				root, path, document, harness := schemaCatalogFixture(t, fragment)
				parent := schemaRange(harness)
				if key == "enumerationProbe" {
					parent = harness
				} else if key == "certifiedAt" || key == "resultDigest" {
					parent = parent["certification"].(map[string]any)
				}
				parent[key] = nil
				writeSchemaFixture(t, path, document)
				if _, err := LoadCatalog(root); err != nil {
					t.Fatalf("explicit null rejected: %v", err)
				}
				delete(parent, key)
				writeSchemaFixture(t, path, document)
				if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), key) {
					t.Fatalf("missing nullable key must name file and field: %v", err)
				}
			})
		}
	}
}

func TestCatalogSchemaRejectsInvalidDescriptors(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown-field", func(h map[string]any) { h["extra"] = true }},
		{"empty-ranges", func(h map[string]any) { h["ranges"] = []any{} }},
		{"invalid-isolation", func(h map[string]any) { h["isolation"] = "anything" }},
		{"invalid-source-uri", func(h map[string]any) { h["sources"] = []any{"not a uri"} }},
		{"duplicate-sources", func(h map[string]any) { h["sources"] = []any{"https://example.com", "https://example.com"} }},
		{"invalid-argv", func(h map[string]any) { schemaRange(h)["run"] = []any{"codex", "$INJECT"} }},
		{"empty-argv", func(h map[string]any) { schemaRange(h)["run"] = []any{} }},
		{"fractional-timeout", func(h map[string]any) { h["versionProbe"].(map[string]any)["timeoutSeconds"] = 1.5 }},
		{"excessive-timeout", func(h map[string]any) { h["versionProbe"].(map[string]any)["timeoutSeconds"] = 11 }},
		{"zero-timeout", func(h map[string]any) { h["versionProbe"].(map[string]any)["timeoutSeconds"] = 0 }},
		{"invalid-version", func(h map[string]any) { schemaRange(h)["minimum"] = "today" }},
		{"unsupported-certification", func(h map[string]any) { schemaRange(h)["status"] = "supported" }},
		{"bad-date", func(h map[string]any) { schemaRange(h)["certification"].(map[string]any)["certifiedAt"] = "yesterday" }},
		{"bad-digest", func(h map[string]any) { schemaRange(h)["certification"].(map[string]any)["resultDigest"] = "sha256:no" }},
		{"duplicate-reasoning", func(h map[string]any) { schemaRange(h)["reasoningLevels"] = []any{"high", "high"} }},
	}
	for _, fragment := range []bool{false, true} {
		for _, test := range cases {
			t.Run(map[bool]string{false: "base", true: "fragment"}[fragment]+"/"+test.name, func(t *testing.T) {
				root, path, document, harness := schemaCatalogFixture(t, fragment)
				test.change(harness)
				writeSchemaFixture(t, path, document)
				if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "schema violation") {
					t.Fatalf("invalid descriptor accepted or unlocated error: %v", err)
				}
			})
		}
	}
}

func TestCatalogSchemaAndSemanticChecksBothApply(t *testing.T) {
	root, path, document, harness := schemaCatalogFixture(t, false)
	harness["versionProbe"].(map[string]any)["argv"] = []any{"wrong", "--version"}
	writeSchemaFixture(t, path, document)
	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), "executable mismatch") {
		t.Fatalf("semantic validation bypassed: %v", err)
	}
}

func TestCatalogSchemaRejectsTrailingJSON(t *testing.T) {
	root, path, _, _ := schemaCatalogFixture(t, true)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n{}"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
}

func TestCatalogSchemaUnavailableInvalidOrExternal(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid", "external-http", "external-file"} {
		t.Run(scenario, func(t *testing.T) {
			root, _, _, _ := schemaCatalogFixture(t, false)
			path := filepath.Join(root, "docs/schemas/harness-catalog.schema.json")
			switch scenario {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				writeSchemaFixture(t, path, map[string]any{"type": 9})
			case "external-http", "external-file":
				reference := "http://127.0.0.1:1/must-not-load"
				if scenario == "external-file" {
					reference = "file:///must-not-load.json"
				}
				writeSchemaFixture(t, path, map[string]any{"$ref": reference})
			}
			if _, err := LoadCatalog(root); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("schema failure not surfaced: %v", err)
			} else if strings.HasPrefix(scenario, "external-") && !strings.Contains(err.Error(), "no URLLoader registered") {
				t.Fatalf("external schema access was not denied by loader: %v", err)
			}
		})
	}
}
