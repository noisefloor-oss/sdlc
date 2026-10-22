package sdlc

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reviewParseFixture(t *testing.T) (string, ReviewEvidenceInput) {
	t.Helper()
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	base, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	diff := "sha256:" + strings.Repeat("c", 64)
	model, reasoning := "claude-opus-5", "high"
	return root, ReviewEvidenceInput{
		SchemaVersion: 1,
		Expectation: ReviewExpectation{
			SchemaVersion: 1, RequestID: "request-1", Repo: "owner/project", PR: 1,
			BaseSHA: base, HeadSHA: head, DiffDigest: diff,
			PolicyDigest: "sha256:" + strings.Repeat("d", 64), CatalogDigest: "sha256:" + strings.Repeat("e", 64),
			Reviewer:     ReviewReviewerBinding{Harness: "claude", Version: "2.1.263", Model: model, ModelFamily: "anthropic-claude-5", Reasoning: reasoning},
			Contributors: []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "git:owner/project@candidate"}},
			ExpectedBot:  "noise-review[bot]", RequireFullCoverage: true,
		},
		Observations: ReviewObservations{
			CurrentBaseSHA: base, CurrentHeadSHA: head,
			Execution:   &ReviewExecutionObservation{RequestID: "request-1", BaseSHA: base, HeadSHA: head, Harness: "claude", Version: "2.1.263", RequestedModel: model, RequestedReasoning: reasoning, EffectiveModel: &model, EffectiveReasoning: &reasoning, EvidenceRef: "runner:request-1"},
			Publication: &ReviewPublicationObservation{RequestID: "request-1", Repo: "owner/project", PR: 1, ReviewID: 2, Author: "noise-review[bot]", AuthorType: "Bot", State: "APPROVED", CommitSHA: head, ReadbackRef: "https://github.com/owner/project/pull/1#pullrequestreview-2"},
			Coverage:    &ReviewCoverageObservation{RequestID: "request-1", BaseSHA: base, HeadSHA: head, DiffDigest: diff, ReviewedDiffDigest: diff, Full: true, Truncated: false, ReviewedComplete: true, UnresolvedFindings: 0, EvidenceRef: "coverage:request-1"},
			Audit:       &InvocationAudit{ID: 2, State: "APPROVED", Author: "noise-review[bot]", AuthorType: "Bot"},
		},
	}
}

func reviewParseBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReviewEvidenceParsePreservesExactUint64IDs(t *testing.T) {
	root, input := reviewParseFixture(t)
	input.Expectation.PR = math.MaxUint64
	input.Observations.Publication.PR = math.MaxUint64
	input.Observations.Publication.ReviewID = 9007199254740993
	input.Observations.Audit.ID = math.MaxUint64
	input.Observations.Coverage.UnresolvedFindings = math.MaxUint64
	parsed, err := ParseReviewEvidence(root, reviewParseBytes(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Expectation.PR != math.MaxUint64 || parsed.Observations.Publication.PR != math.MaxUint64 || parsed.Observations.Publication.ReviewID != 9007199254740993 || parsed.Observations.Audit.ID != math.MaxUint64 || parsed.Observations.Coverage.UnresolvedFindings != math.MaxUint64 {
		t.Fatal("identifier/count changed during schema or typed decoding")
	}
}

func TestReviewEvidenceParseRepresentsMissingAndUnknown(t *testing.T) {
	root, input := reviewParseFixture(t)
	input.Expectation.Contributors = []ReviewContributor{}
	input.Observations = ReviewObservations{}
	parsed, err := ParseReviewEvidence(root, reviewParseBytes(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Observations.Execution != nil || parsed.Observations.CurrentHeadSHA != "" || parsed.Observations.CurrentBaseSHA != "" || len(parsed.Expectation.Contributors) != 0 {
		t.Fatal("missing observations were converted into evidence")
	}
	_, input = reviewParseFixture(t)
	input.Observations.Execution.EffectiveModel = nil
	input.Observations.Execution.EffectiveReasoning = nil
	input.Observations.Coverage.Full = false
	input.Observations.Coverage.ReviewedComplete = false
	parsed, err = ParseReviewEvidence(root, reviewParseBytes(t, input))
	if err != nil || parsed.Observations.Execution.EffectiveModel != nil || parsed.Observations.Execution.EffectiveReasoning != nil || parsed.Observations.Coverage.Full || parsed.Observations.Coverage.ReviewedComplete {
		t.Fatalf("explicit unknown/false observation lost: %v", err)
	}
}

func TestReviewEvidenceParseRequiredFieldsAndClosedObjects(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"schema", func(v map[string]any) { v["schemaVersion"] = 2 }},
		{"expectation-schema", func(v map[string]any) { v["expectation"].(map[string]any)["schemaVersion"] = 2 }},
		{"root-unknown", func(v map[string]any) { v["trusted"] = true }},
		{"missing-expectation", func(v map[string]any) { delete(v, "expectation") }},
		{"missing-observations", func(v map[string]any) { delete(v, "observations") }},
		{"missing-current-head", func(v map[string]any) { delete(v["observations"].(map[string]any), "currentHeadSHA") }},
		{"missing-current-base", func(v map[string]any) { delete(v["observations"].(map[string]any), "currentBaseSHA") }},
		{"missing-request", func(v map[string]any) { delete(v["expectation"].(map[string]any), "requestID") }},
		{"missing-diff", func(v map[string]any) { delete(v["expectation"].(map[string]any), "diffDigest") }},
		{"nullable-contributors", func(v map[string]any) { v["expectation"].(map[string]any)["contributors"] = nil }},
		{"weakened-coverage", func(v map[string]any) { v["expectation"].(map[string]any)["requireFullCoverage"] = false }},
		{"null-execution-group", func(v map[string]any) { v["observations"].(map[string]any)["execution"] = nil }},
		{"effective-model-absent", func(v map[string]any) { delete(reviewParseGroup(v, "execution"), "effectiveModel") }},
		{"requested-reasoning-absent", func(v map[string]any) { delete(reviewParseGroup(v, "execution"), "requestedReasoning") }},
		{"readback-absent", func(v map[string]any) { delete(reviewParseGroup(v, "publication"), "readbackRef") }},
		{"body-forbidden", func(v map[string]any) { reviewParseGroup(v, "publication")["body"] = "not permitted" }},
		{"trusted-forbidden", func(v map[string]any) { reviewParseGroup(v, "execution")["trusted"] = true }},
		{"reviewed-digest-absent", func(v map[string]any) { delete(reviewParseGroup(v, "coverage"), "reviewedDiffDigest") }},
		{"zero-count-absent", func(v map[string]any) { delete(reviewParseGroup(v, "coverage"), "unresolvedFindings") }},
		{"false-truncation-absent", func(v map[string]any) { delete(reviewParseGroup(v, "coverage"), "truncated") }},
		{"empty-ref", func(v map[string]any) { reviewParseGroup(v, "coverage")["evidenceRef"] = "" }},
		{"multiline-ref", func(v map[string]any) { reviewParseGroup(v, "coverage")["evidenceRef"] = "public\nprivate" }},
		{"query-ref", func(v map[string]any) {
			reviewParseGroup(v, "coverage")["evidenceRef"] = "https://example.test/item?secret=value"
		}},
		{"userinfo-ref", func(v map[string]any) {
			reviewParseGroup(v, "coverage")["evidenceRef"] = "https://user:password@example.test/item"
		}},
		{"long-ref", func(v map[string]any) {
			reviewParseGroup(v, "coverage")["evidenceRef"] = strings.Repeat("a", 513)
		}},
		{"short-head", func(v map[string]any) { v["expectation"].(map[string]any)["headSHA"] = "abcdef" }},
		{"uppercase-head", func(v map[string]any) { v["observations"].(map[string]any)["currentHeadSHA"] = strings.Repeat("A", 40) }},
		{"noncanonical-digest", func(v map[string]any) { v["expectation"].(map[string]any)["policyDigest"] = strings.Repeat("a", 64) }},
		{"missing-bot-suffix", func(v map[string]any) { v["expectation"].(map[string]any)["expectedBot"] = "noise-review" }},
		{"noncanonical-repo", func(v map[string]any) { v["expectation"].(map[string]any)["repo"] = "https://github.com/owner/project" }},
		{"request-whitespace", func(v map[string]any) { v["expectation"].(map[string]any)["requestID"] = "request 1" }},
		{"unknown-state", func(v map[string]any) { reviewParseGroup(v, "publication")["state"] = "PASS" }},
		{"zero-review-id", func(v map[string]any) { reviewParseGroup(v, "publication")["reviewID"] = 0 }},
		{"negative-count", func(v map[string]any) { reviewParseGroup(v, "coverage")["unresolvedFindings"] = -1 }},
		{"overflow-id", func(v map[string]any) {
			reviewParseGroup(v, "publication")["reviewID"] = json.Number("18446744073709551616")
		}},
		{"fraction-id", func(v map[string]any) { reviewParseGroup(v, "publication")["reviewID"] = json.Number("1.5") }},
		{"exponent-id", func(v map[string]any) { reviewParseGroup(v, "publication")["reviewID"] = json.Number("1e3") }},
		{"string-id", func(v map[string]any) { reviewParseGroup(v, "publication")["reviewID"] = "2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, input := reviewParseFixture(t)
			var value map[string]any
			decoder := json.NewDecoder(bytes.NewReader(reviewParseBytes(t, input)))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			tc.edit(value)
			if _, err := ParseReviewEvidence(root, reviewParseBytes(t, value)); err == nil {
				t.Fatal("invalid comparison declaration accepted")
			}
		})
	}
}

func reviewParseGroup(value map[string]any, group string) map[string]any {
	return value["observations"].(map[string]any)[group].(map[string]any)
}

func TestReviewEvidenceParseRejectsDuplicateKeysAndMalformedBytes(t *testing.T) {
	root, input := reviewParseFixture(t)
	valid := string(reviewParseBytes(t, input))
	for name, data := range map[string][]byte{
		"root-duplicate":     []byte(strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)),
		"nested-duplicate":   []byte(strings.Replace(valid, `"full":true`, `"full":true,"full":false`, 1)),
		"escaped-duplicate":  []byte(strings.Replace(valid, `"reviewID":2`, `"reviewID":2,"review\u0049D":3`, 1)),
		"trailing":           []byte(valid + `{}`),
		"trailing-malformed": []byte(valid + `x`),
		"empty":              nil,
		"invalid-utf8":       append([]byte(valid), 0xff),
		"unterminated":       []byte(valid[:len(valid)-1]),
		"deep":               []byte(strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReviewEvidence(root, data); err == nil {
				t.Fatal("ambiguous or malformed input accepted")
			}
		})
	}
}

func TestReviewEvidenceParseByteBoundAndReferencesStayUnopened(t *testing.T) {
	root, input := reviewParseFixture(t)
	// These references do not exist; parsing must not perform filesystem or
	// network observation to convert their mere presence into verified evidence.
	input.Observations.Execution.EvidenceRef = "https://never-resolves.invalid/run/1"
	input.Observations.Publication.ReadbackRef = "artifact:missing-review-2"
	data := reviewParseBytes(t, input)
	data = append(data, bytes.Repeat([]byte(" "), MaxReviewEvidenceBytes-len(data))...)
	if _, err := ParseReviewEvidence(root, data); err != nil {
		t.Fatalf("valid byte-bound record failed: %v", err)
	}
	if _, err := ParseReviewEvidence(root, append(data, ' ')); err == nil {
		t.Fatal("oversized record accepted")
	}
}

func TestReviewExpectationUsesTheCommittedSchema(t *testing.T) {
	root, input := reviewParseFixture(t)
	if err := ValidateReviewExpectation(root, input.Expectation); err != nil {
		t.Fatal(err)
	}
	input.Expectation.RequireFullCoverage = false
	if err := ValidateReviewExpectation(root, input.Expectation); err == nil {
		t.Fatal("builder validation weakened full coverage")
	}
	_, input = reviewParseFixture(t)
	for name, schema := range map[string]string{
		"missing":                  "",
		"malformed":                "{",
		"duplicate":                `{"type":"object","type":"array"}`,
		"remote":                   `{"$ref":"https://never-resolves.invalid/schema.json"}`,
		"external-file":            `{"$ref":"file:///unavailable/review-schema.json"}`,
		"changed-committed-schema": `{"type":"object","properties":{"schemaVersion":{"const":99}},"$defs":{"expectation":{"type":"object","properties":{"schemaVersion":{"const":99}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			isolated := t.TempDir()
			path := filepath.Join(isolated, "docs", "schemas", "review-evidence.schema.json")
			if schema != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ParseReviewEvidence(isolated, reviewParseBytes(t, input)); err == nil {
				t.Fatal("missing, external, or conflicting committed schema accepted")
			}
		})
	}
}
