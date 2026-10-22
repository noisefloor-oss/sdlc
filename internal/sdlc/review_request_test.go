package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This fixture deliberately adopts an explicit review binding instead of
// asking the resolver to replace the default, unsupported Codex review route.
// All managed files are created only inside t.TempDir through planner helpers.
func reviewRequestFixture(t *testing.T, edit func([]Binding) []Binding) (string, string) {
	t.Helper()
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := LoadSeed(root)
	if err != nil {
		t.Fatal(err)
	}
	template, err := LoadTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []Binding{
		{Role: "implementation.standard", Status: "resolved", Harness: stringPointer("codex"), Model: stringPointer("gpt-5.6-sol"), ModelFamily: stringPointer("openai-gpt-5.6"), Reasoning: stringPointer("high"), Provenance: "operator-declared", Constraints: []string{}, Reasons: []string{}},
		{Role: "review.formal", Status: "resolved", Harness: stringPointer("claude"), Model: stringPointer("claude-opus-5"), ModelFamily: stringPointer("anthropic-claude-5"), Reasoning: stringPointer("high"), Provenance: "operator-declared", Constraints: []string{}, Reasons: []string{}},
	}
	if edit != nil {
		bindings = edit(bindings)
	}
	project := projectDocument(testAnswers(), bindings, template)
	effectiveDigest, err := Digest(map[string]any{"seed": seed, "project": project})
	if err != nil {
		t.Fatal(err)
	}
	projectBytes, err := CanonicalBytes(project)
	if err != nil {
		t.Fatal(err)
	}
	agentsBytes := []byte(renderAgentsBlock(effectiveDigest))
	sdlcBytes := renderSDLC(seed, project, effectiveDigest)
	receipt := adoptionReceipt(catalog, seed, template, projectBytes, agentsBytes, sdlcBytes, effectiveDigest, true, nil, nil, false)
	receiptBytes, err := CanonicalBytes(receipt)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := Adopt(target, map[string][]byte{
		"AGENTS.md": agentsBytes, "SDLC.md": sdlcBytes,
		".sdlc/project.yaml": projectBytes, ".sdlc/adoption.json": receiptBytes,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err != nil {
		t.Fatalf("fixture authority failed verification: %v", err)
	}
	return root, target
}

func validReviewRequestInput() ReviewRequestInput {
	return ReviewRequestInput{
		RequestID: "review-request-84.1", Repo: "example/project", PR: 42,
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40),
		DiffDigest: "sha256:" + strings.Repeat("c", 64), HarnessVersion: "2.1.263", ExpectedBot: "noise-review[bot]",
		Contributors: []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "evidence/absent-implementation.json"}},
	}
}

func TestBuildReviewExpectationUsesVerifiedExplicitBindingWithoutLaunching(t *testing.T) {
	root, target := reviewRequestFixture(t, nil)
	t.Setenv("PATH", t.TempDir())
	before := diagnosisSnapshot(t, target)
	input := validReviewRequestInput()
	input.Contributors = append(input.Contributors, ReviewContributor{Harness: "omp", Model: "zai/glm-5.3", EvidenceRef: "evidence/absent-repair.json"})
	result, err := BuildReviewExpectation(root, target, input)
	if err != nil {
		t.Fatal(err)
	}
	wantReviewer := ReviewReviewerBinding{Harness: "claude", Version: "2.1.263", Model: "claude-opus-5", ModelFamily: "anthropic-claude-5", Reasoning: "high"}
	if result.SchemaVersion != 1 || result.RequestID != input.RequestID || result.Repo != input.Repo || result.PR != input.PR || result.BaseSHA != input.BaseSHA || result.HeadSHA != input.HeadSHA || result.DiffDigest != input.DiffDigest || result.ExpectedBot != input.ExpectedBot || !result.RequireFullCoverage || result.Reviewer != wantReviewer || !reflect.DeepEqual(result.Contributors, input.Contributors) {
		t.Fatalf("candidate metadata or adopted reviewer changed: %#v", result)
	}
	// The receipt has non-string input values too; inspect only these two fields.
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(filepath.Join(target, ".sdlc/adoption.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var inputs map[string]any
	if err := json.Unmarshal(raw["inputs"], &inputs); err != nil {
		t.Fatal(err)
	}
	if result.PolicyDigest != inputs["effectivePolicy"] || result.CatalogDigest != inputs["catalog"] {
		t.Fatal("expectation lost the verified policy or adopted catalog digest")
	}
	if !reflect.DeepEqual(before, diagnosisSnapshot(t, target)) {
		t.Fatal("read-only expectation construction changed the adopted project")
	}
	if _, err := os.Stat(filepath.Join(target, input.Contributors[0].EvidenceRef)); !os.IsNotExist(err) {
		t.Fatal("fixture must demonstrate that public contributor references are declarations, not files to open")
	}
}

func TestBuildReviewExpectationRejectsInvalidCandidateMetadata(t *testing.T) {
	root, target := reviewRequestFixture(t, nil)
	t.Setenv("PATH", t.TempDir())
	before := diagnosisSnapshot(t, target)
	cases := []struct {
		name string
		edit func(*ReviewRequestInput)
	}{
		{"missing-request-id", func(i *ReviewRequestInput) { i.RequestID = "" }},
		{"request-control", func(i *ReviewRequestInput) { i.RequestID = "request\nother" }},
		{"request-oversize", func(i *ReviewRequestInput) { i.RequestID = strings.Repeat("r", 129) }},
		{"missing-repo-owner", func(i *ReviewRequestInput) { i.Repo = "project" }},
		{"repo-url", func(i *ReviewRequestInput) { i.Repo = "https://github.com/example/project" }},
		{"repo-traversal", func(i *ReviewRequestInput) { i.Repo = "example/../project" }},
		{"repo-control", func(i *ReviewRequestInput) { i.Repo = "example/project\nother" }},
		{"zero-pr", func(i *ReviewRequestInput) { i.PR = 0 }},
		{"missing-base", func(i *ReviewRequestInput) { i.BaseSHA = "" }},
		{"abbreviated-head", func(i *ReviewRequestInput) { i.HeadSHA = "abcdef1" }},
		{"nonhex-head", func(i *ReviewRequestInput) { i.HeadSHA = strings.Repeat("z", 40) }},
		{"uppercase-sha", func(i *ReviewRequestInput) { i.HeadSHA = strings.Repeat("B", 40) }},
		{"missing-diff-digest", func(i *ReviewRequestInput) { i.DiffDigest = "" }},
		{"invalid-diff-digest", func(i *ReviewRequestInput) { i.DiffDigest = "sha256:abc" }},
		{"missing-version", func(i *ReviewRequestInput) { i.HarnessVersion = "" }},
		{"malformed-version", func(i *ReviewRequestInput) { i.HarnessVersion = "latest" }},
		{"noncanonical-version", func(i *ReviewRequestInput) { i.HarnessVersion = "02.1.263" }},
		{"unsupported-version", func(i *ReviewRequestInput) { i.HarnessVersion = "99.0.0" }},
		{"missing-bot", func(i *ReviewRequestInput) { i.ExpectedBot = "" }},
		{"human-login", func(i *ReviewRequestInput) { i.ExpectedBot = "human-user" }},
		{"bot-control", func(i *ReviewRequestInput) { i.ExpectedBot = "noise-review[bot]\nother" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := validReviewRequestInput()
			test.edit(&input)
			if _, err := BuildReviewExpectation(root, target, input); err == nil {
				t.Fatal("invalid candidate metadata accepted")
			}
		})
	}
	if !reflect.DeepEqual(before, diagnosisSnapshot(t, target)) {
		t.Fatal("failed request validation changed managed files")
	}
}

func TestBuildReviewExpectationRequiresKnownIndependentActualContributors(t *testing.T) {
	root, target := reviewRequestFixture(t, nil)
	t.Setenv("PATH", t.TempDir())
	cases := []struct {
		name         string
		contributors []ReviewContributor
	}{
		{"missing", nil},
		{"empty", []ReviewContributor{}},
		{"unknown-harness", []ReviewContributor{{Harness: "missing", Model: "gpt-5.6-sol", EvidenceRef: "evidence/implementation.json"}}},
		{"unknown-model", []ReviewContributor{{Harness: "codex", Model: "unrecorded-model", EvidenceRef: "evidence/implementation.json"}}},
		{"missing-source", []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol"}}},
		{"private-text-source", []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "not a bounded public locator"}}},
		{"same-model", []ReviewContributor{{Harness: "claude", Model: "claude-opus-5", EvidenceRef: "evidence/repair.json"}}},
		{"same-family-different-model", []ReviewContributor{{Harness: "claude", Model: "claude-sonnet-5", EvidenceRef: "evidence/repair.json"}}},
		{"independent-implementation-overlapping-repair", []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "evidence/implementation.json"}, {Harness: "claude", Model: "claude-sonnet-5", EvidenceRef: "evidence/repair.json"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := validReviewRequestInput()
			input.Contributors = test.contributors
			if _, err := BuildReviewExpectation(root, target, input); err == nil {
				t.Fatal("unknown or non-independent actual contributor was accepted")
			}
		})
	}
}

func TestBuildReviewExpectationRefusesUnresolvedOrUnsupportedAdoptedRole(t *testing.T) {
	cases := []struct {
		name string
		edit func([]Binding) []Binding
	}{
		{"missing-role", func(b []Binding) []Binding { return b[:1] }},
		{"unknown-role", func(b []Binding) []Binding { b[1].Role = "review.invented"; return b }},
		{"unresolved", func(b []Binding) []Binding { b[1].Status = "unresolved"; return b }},
		{"unknown-harness", func(b []Binding) []Binding { b[1].Harness = stringPointer("missing"); return b }},
		{"unknown-model", func(b []Binding) []Binding { b[1].Model = stringPointer("unrecorded-model"); return b }},
		{"insufficient-reviewer-quality", func(b []Binding) []Binding { b[1].Model = stringPointer("claude-sonnet-5"); return b }},
		{"incorrect-family", func(b []Binding) []Binding { b[1].ModelFamily = stringPointer("openai-gpt-5.6"); return b }},
		{"unsupported-reasoning", func(b []Binding) []Binding { b[1].Reasoning = stringPointer("off"); return b }},
		{"codex-no-substitution", func(b []Binding) []Binding {
			b[1].Harness, b[1].Model, b[1].ModelFamily = stringPointer("codex"), stringPointer("gpt-5.6-sol"), stringPointer("openai-gpt-5.6")
			return b
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, target := reviewRequestFixture(t, test.edit)
			input := validReviewRequestInput()
			if test.name == "codex-no-substitution" {
				input.HarnessVersion = "0.147.0"
				input.Contributors = []ReviewContributor{{Harness: "omp", Model: "zai/glm-5.3", EvidenceRef: "evidence/implementation.json"}}
			}
			if _, err := BuildReviewExpectation(root, target, input); err == nil {
				t.Fatal("unresolved or unsupported adopted review route was replaced or accepted")
			}
		})
	}
}

func TestParseReviewRequestInputPreservesExactValuesAndRejectsAmbiguousJSON(t *testing.T) {
	input := validReviewRequestInput()
	input.PR = 9007199254740993
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseReviewRequestInput(data)
	if err != nil || !reflect.DeepEqual(parsed, input) {
		t.Fatalf("valid request changed during parsing: %#v, %v", parsed, err)
	}
	text := string(data)
	cases := []struct {
		name string
		data string
	}{
		{"duplicate-request", strings.Replace(text, `"requestID":`, `"requestID":"other","requestID":`, 1)},
		{"request-alias-with-canonical", strings.TrimSuffix(text, "}") + `,"RequestID":"other"}`},
		{"request-alias-alone", strings.Replace(text, `"requestID":`, `"RequestID":`, 1)},
		{"duplicate-contributor", strings.Replace(text, `"harness":`, `"harness":"other","harness":`, 1)},
		{"contributor-harness-alias", strings.Replace(text, `"harness":`, `"Harness":`, 1)},
		{"unknown-top-level", strings.TrimSuffix(text, "}") + `,"unexpected":true}`},
		{"unknown-contributor-field", strings.Replace(text, `"harness":`, `"unexpected":true,"harness":`, 1)},
		{"trailing-json", text + "\n{}"},
		{"fractional-pr", strings.Replace(text, `9007199254740993`, `1.5`, 1)},
		{"string-pr", strings.Replace(text, `9007199254740993`, `"42"`, 1)},
		{"invalid-utf8", strings.Replace(text, "example/project", "example/\xffproject", 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseReviewRequestInput([]byte(test.data)); err == nil {
				t.Fatal("ambiguous or unsupported request JSON was accepted")
			}
		})
	}
}

func TestBuildReviewExpectationRefusesDriftAndCatalogMismatch(t *testing.T) {
	for _, file := range []string{"SDLC.md", "AGENTS.md", ".sdlc/project.yaml"} {
		t.Run(file, func(t *testing.T) {
			root, target := reviewRequestFixture(t, nil)
			if err := os.WriteFile(filepath.Join(target, file), []byte("drift\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildReviewExpectation(root, target, validReviewRequestInput()); err == nil {
				t.Fatal("drifted adopted authority accepted")
			}
		})
	}
	root, target := reviewRequestFixture(t, nil)
	path := filepath.Join(target, ".sdlc/adoption.json")
	var receipt map[string]any
	if err := LoadFlexibleJSON(path, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["inputs"].(map[string]any)["catalog"] = "sha256:" + strings.Repeat("0", 64)
	data, err := CanonicalBytes(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildReviewExpectation(root, target, validReviewRequestInput()); err == nil {
		t.Fatal("request substituted the current catalog for the adopted catalog")
	}
	if _, err := BuildReviewExpectation(root, t.TempDir(), validReviewRequestInput()); err == nil {
		t.Fatal("request accepted an unadopted target")
	}
}
