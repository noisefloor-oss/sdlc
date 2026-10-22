package sdlc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebAdoptionRequiresExactConfirmation(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := LoadCatalog(root)
	template, _ := LoadTemplate(root)
	answers := testAnswers()
	server := &webServer{
		target: t.TempDir(), root: root, baseline: answers,
		inventory:   Inventory{SchemaVersion: 1, Source: "test", Catalog: catalog.Catalog, Harnesses: []HarnessObservation{}},
		actionToken: "secret", catalog: catalog, template: template,
	}

	body := map[string]any{"answers": answers.Answers, "planDigest": "wrong", "actionToken": "secret"}
	payload, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "/api/adopt", bytes.NewReader(payload))
	request.Header.Set("content-type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestWebAnswersRejectUnknownFields(t *testing.T) {
	server := &webServer{baseline: testAnswers()}
	values := map[string]any{
		"authorship": "agent", "delivery": "software", "risk": "ordinary",
		"routingPreference": "balanced", "highRiskClasses": []any{}, "surprise": true,
	}
	if _, err := server.mergedAnswers(values); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func testAnswers() AnswersFile {
	return AnswersFile{
		SchemaVersion: 1, Seed: "noise/agent@0.1", ExecutionTemplate: ExecutionTemplateID,
		Answers:              Answers{Authorship: "agent", Delivery: "software", Risk: "ordinary", RoutingPreference: "balanced", HighRiskClasses: []string{}},
		OperatorCapabilities: []OperatorCapability{}, ProjectRules: []Rule{},
	}
}
