package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeJSONRequiresEOF(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		valid bool
	}{
		{"object", `{"value":"first"}`, true},
		{"trailing whitespace", "{\"value\":\"first\"}\n\t ", true},
		{"second object", `{"value":"first"}{"value":"second"}`, false},
		{"second array", `{"value":"first"}[]`, false},
		{"garbage", `{"value":"first"}not-json`, false},
		{"malformed first object", `{"value":`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var destination struct {
				Value string `json:"value"`
			}
			err := decodeJSON("answers.json", []byte(test.input), &destination)
			if test.valid && err != nil {
				t.Fatalf("expected valid input: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid input to fail")
			}
			if test.valid && destination.Value != "first" {
				t.Fatalf("decoded value %q", destination.Value)
			}
		})
	}
}

func TestLoadJSONAnswersRejectsTrailingValue(t *testing.T) {
	encoded, err := json.Marshal(testAnswers())
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n', '{', '}')
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	var answers AnswersFile
	if err := LoadJSON(path, &answers); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected trailing answers value to fail, got %v", err)
	}
}

func TestInspectBootstrapRejectsTrailingReceiptValue(t *testing.T) {
	target := t.TempDir()
	if _, err := Bootstrap(target); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(target, BootstrapReceiptPath)
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt = append(receipt, '\n', '{', '}')
	if err := os.WriteFile(receiptPath, receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBootstrap(target); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected trailing bootstrap receipt value to fail, got %v", err)
	}
}
