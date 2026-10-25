package sdlc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	testWebHost   = "127.0.0.1:8765"
	testWebOrigin = "http://" + testWebHost
)

func TestWebAdoptionRequiresExactConfirmation(t *testing.T) {
	server := newTestWebServer(t)
	body := map[string]any{"answers": server.baseline.Answers, "planDigest": "wrong", "actionToken": "secret"}
	response := performWebRequest(server, http.MethodPost, "/api/adopt", body, testWebHost, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestWebAdoptionAllowsReviewedLoopbackPlan(t *testing.T) {
	server := newTestWebServer(t)
	bootstrap := performWebRequest(server, http.MethodGet, "/api/bootstrap", nil, testWebHost, "")
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("bootstrap got status %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	var boot map[string]any
	if err := json.Unmarshal(bootstrap.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}

	plan := performWebRequest(server, http.MethodPost, "/api/plan", map[string]any{"answers": server.baseline.Answers}, testWebHost, testWebOrigin)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan got status %d: %s", plan.Code, plan.Body.String())
	}
	var planned map[string]any
	if err := json.Unmarshal(plan.Body.Bytes(), &planned); err != nil {
		t.Fatal(err)
	}

	adopt := performWebRequest(server, http.MethodPost, "/api/adopt", map[string]any{
		"answers": server.baseline.Answers, "planDigest": planned["planDigest"], "actionToken": boot["actionToken"],
	}, testWebHost, testWebOrigin)
	if adopt.Code != http.StatusOK {
		t.Fatalf("adopt got status %d: %s", adopt.Code, adopt.Body.String())
	}
	for _, relative := range []string{"AGENTS.md", "SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		info, err := os.Stat(filepath.Join(server.target, relative))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected regular adopted file %s: %v", relative, err)
		}
	}
}

func TestWebRejectsForgedHostBeforeDispatch(t *testing.T) {
	server := newTestWebServer(t)
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/bootstrap"},
		{http.MethodGet, "/api/deck-sync"},
		{http.MethodGet, "/api/onboarding"},
		{http.MethodPost, "/api/plan"},
		{http.MethodPost, "/api/adopt"},
		{http.MethodPost, "/api/deck-receipt"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := performWebRequest(server, test.method, test.path, map[string]any{}, "rebind.attacker.example:8765", "")
			if response.Code != http.StatusForbidden {
				t.Fatalf("got status %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
	entries, err := os.ReadDir(server.target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("forged host wrote %d target entries", len(entries))
	}
}

func TestWebRejectsCrossOriginPosts(t *testing.T) {
	server := newTestWebServer(t)
	for _, path := range []string{"/api/plan", "/api/adopt", "/api/deck-receipt"} {
		response := performWebRequest(server, http.MethodPost, path, map[string]any{}, testWebHost, "http://rebind.attacker.example:8765")
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s got status %d, want %d", path, response.Code, http.StatusForbidden)
		}
	}
}

func TestLoopbackHostPortValidation(t *testing.T) {
	for _, test := range []struct {
		hostport string
		port     int
		valid    bool
	}{
		{"127.0.0.1:8765", 8765, true},
		{"localhost:8765", 8765, true},
		{"LOCALHOST:8765", 8765, true},
		{"[::1]:8765", 8765, true},
		{"127.0.0.1", 80, true},
		{"[::1]", 80, true},
		{"rebind.attacker.example:8765", 8765, false},
		{"127.0.0.1:9999", 8765, false},
		{"127.0.0.1.attacker.example:8765", 8765, false},
		{"127.0.0.1", 8765, false},
		{"::1", 80, false},
		{"[::1", 80, false},
		{"[::1]:8765:80", 80, false},
		{"", 80, false},
	} {
		if got := validLoopbackHostPort(test.hostport, test.port); got != test.valid {
			t.Errorf("validLoopbackHostPort(%q, %d) = %v, want %v", test.hostport, test.port, got, test.valid)
		}
	}
}

func TestLoopbackOriginValidation(t *testing.T) {
	for _, test := range []struct {
		origin string
		valid  bool
	}{
		{"", true},
		{testWebOrigin, true},
		{"http://localhost:8765", true},
		{"http://LOCALHOST:8765", true},
		{"http://[::1]:8765", true},
		{"https://127.0.0.1:8765", false},
		{"null", false},
		{testWebOrigin + "/path", false},
		{testWebOrigin + "?", false},
		{"http://rebind.attacker.example:8765", false},
		{"http://127.0.0.1:9999", false},
	} {
		if got := validLoopbackOrigin(test.origin, 8765); got != test.valid {
			t.Errorf("validLoopbackOrigin(%q) = %v, want %v", test.origin, got, test.valid)
		}
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

func newTestWebServer(t *testing.T) *webServer {
	t.Helper()
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
	answers := testAnswers()
	return &webServer{
		target: t.TempDir(), root: root, baseline: answers,
		inventory:   Inventory{SchemaVersion: 1, Source: "test", Catalog: catalog.Catalog, Harnesses: []HarnessObservation{}},
		actionToken: "secret", catalog: catalog, template: template, port: 8765,
	}
}

func performWebRequest(server *webServer, method, path string, body any, host, origin string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, _ := json.Marshal(body)
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Host = host
	request.Header.Set("content-type", "application/json; charset=utf-8")
	if origin != "" {
		request.Header.Set("origin", origin)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func testAnswers() AnswersFile {
	return AnswersFile{
		SchemaVersion: 1, Seed: "noise/agent@0.1", ExecutionTemplate: ExecutionTemplateID,
		Answers:              Answers{Authorship: "agent", Delivery: "software", Risk: "ordinary", RoutingPreference: "balanced", HighRiskClasses: []string{}},
		OperatorCapabilities: []OperatorCapability{}, ProjectRules: []Rule{},
	}
}
