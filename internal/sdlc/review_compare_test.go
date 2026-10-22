package sdlc

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func reviewComparisonFixture(t *testing.T) (Catalog, ReviewExpectation, ReviewObservations) {
	t.Helper()
	catalog := Catalog{SchemaVersion: 1, Catalog: "noise/harnesses@0.5"}
	for _, item := range []struct{ harness, version, maximum, model, family string }{
		{"codex", "0.147.0", "0.147.1", "gpt-5.6-sol", "openai-gpt-5.6"},
		{"claude", "2.1.263", "2.1.264", "claude-opus-5", "anthropic-claude-5"},
		{"omp", "18.1.11", "18.1.12", "zai/glm-5.3", "zai-glm-5.3"},
	} {
		catalog.Harnesses = append(catalog.Harnesses, Harness{
			ID: item.harness, Executable: item.harness,
			VersionProbe:   Probe{Argv: []string{item.harness, "--version"}},
			VersionPattern: `^(?P<version>[0-9]+\.[0-9]+\.[0-9]+)$`,
			Ranges:         []VersionRange{{Minimum: item.version, MaximumExclusive: item.maximum, Status: "experimental", ReasoningLevels: []string{"high", "max"}}},
			Models:         []Model{{ID: item.model, ModelFamily: item.family, QualityTier: "frontier", ReasoningLevels: []string{"high", "max"}, Status: "experimental"}},
		})
	}
	catalogDigest, err := Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	expectation := ReviewExpectation{
		SchemaVersion: 1, RequestID: "request-round-2", Repo: "example/project", PR: 42,
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40),
		DiffDigest: DigestBytes([]byte("entire candidate diff")), PolicyDigest: DigestBytes([]byte("effective policy")), CatalogDigest: catalogDigest,
		Reviewer:     ReviewReviewerBinding{Harness: "omp", Version: "18.1.11", Model: "zai/glm-5.3", ModelFamily: "zai-glm-5.3", Reasoning: "high"},
		Contributors: []ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "https://example.invalid/contributions/initial"}},
		ExpectedBot:  "diff-guard[bot]", RequireFullCoverage: true,
	}
	observations := ReviewObservations{
		CurrentBaseSHA: expectation.BaseSHA, CurrentHeadSHA: expectation.HeadSHA,
		Execution: &ReviewExecutionObservation{
			RequestID: expectation.RequestID, BaseSHA: expectation.BaseSHA, HeadSHA: expectation.HeadSHA,
			Harness: "omp", Version: "18.1.11", RequestedModel: "zai/glm-5.3", RequestedReasoning: "high",
			EffectiveModel: stringPointer("zai/glm-5.3"), EffectiveReasoning: stringPointer("high"), EvidenceRef: "https://example.invalid/execution/round-2",
		},
		Publication: &ReviewPublicationObservation{
			RequestID: expectation.RequestID, Repo: expectation.Repo, PR: expectation.PR, ReviewID: 123,
			Author: expectation.ExpectedBot, AuthorType: "Bot", State: "APPROVED", CommitSHA: expectation.HeadSHA,
			ReadbackRef: "https://example.invalid/reviews/123",
		},
		Coverage: &ReviewCoverageObservation{
			RequestID: expectation.RequestID, BaseSHA: expectation.BaseSHA, HeadSHA: expectation.HeadSHA,
			DiffDigest: expectation.DiffDigest, ReviewedDiffDigest: expectation.DiffDigest, Full: true, ReviewedComplete: true,
			EvidenceRef: "https://example.invalid/coverage/round-2",
		},
	}
	return catalog, expectation, observations
}

func assertReviewComparisonBoundary(t *testing.T, result ReviewComparison) {
	t.Helper()
	if result.SchemaVersion != 1 || result.Kind != "noise.sdlc.review-comparison" || result.ApprovalProof != "not-established" ||
		result.MergeReadiness != "not-assessed" || result.Authenticity != "not-assessed" {
		t.Fatalf("comparison claimed authority: %#v", result)
	}
	if !sort.StringsAreSorted(result.Reasons) {
		t.Fatalf("reason codes are unstable: %v", result.Reasons)
	}
	for index := 1; index < len(result.Reasons); index++ {
		if result.Reasons[index] == result.Reasons[index-1] {
			t.Fatalf("duplicate reason code: %v", result.Reasons)
		}
	}
}

func TestCompareReviewEvidenceMatchIsOnlyDeclarationConsistency(t *testing.T) {
	catalog, expectation, observations := reviewComparisonFixture(t)
	before, err := json.Marshal([]any{catalog, expectation, observations})
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || result.Status != "match" || len(result.Reasons) != 0 {
		t.Fatalf("matching declarations failed: %#v, %v", result, err)
	}
	assertReviewComparisonBoundary(t, result)
	// No audit is required when request-bound owner readback is supplied. Neither
	// that readback nor an actor's final audit is treated as authenticated here.
	observations.Audit = &InvocationAudit{ID: 123, State: "APPROVED", Author: "diff-guard[bot]", AuthorType: "Bot"}
	withAudit, err := CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || !reflect.DeepEqual(result, withAudit) {
		t.Fatalf("matching audit changed policy comparison: %#v, %v", withAudit, err)
	}
	observations.Audit = nil
	after, err := json.Marshal([]any{catalog, expectation, observations})
	if err != nil || string(before) != string(after) {
		t.Fatal("pure comparison mutated its inputs")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "example.invalid") || strings.Contains(string(encoded), "trusted") || strings.Contains(string(encoded), "APPROVED") {
		t.Fatalf("comparison copied evidence or produced a verdict: %s", encoded)
	}
}

func TestCompareReviewEvidenceContradictions(t *testing.T) {
	otherSHA, otherDigest := strings.Repeat("c", 40), DigestBytes([]byte("other diff"))
	cases := []struct {
		name, reason string
		edit         func(*Catalog, *ReviewExpectation, *ReviewObservations)
	}{
		{"catalog-pin", "catalog_digest_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { c.Catalog = "noise/harnesses@0.6" }},
		{"declared-family", "reviewer_family_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			e.Reviewer.ModelFamily = "caller-invented-family"
		}},
		{"unknown-reviewer-harness", "reviewer_harness_unknown", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { e.Reviewer.Harness = "unknown-harness" }},
		{"unknown-reviewer-model", "reviewer_model_unknown", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { e.Reviewer.Model = "zai/glm-4.7" }},
		{"uncataloged-version", "reviewer_version_uncataloged", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { e.Reviewer.Version = "18.1.12" }},
		{"unsupported-reasoning", "reviewer_reasoning_unsupported", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { e.Reviewer.Reasoning = "ultra" }},
		{"unknown-contributor-harness", "contributor_harness_unknown", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			e.Contributors[0].Harness = "uncataloged"
		}},
		{"unknown-contributor-model", "contributor_model_unknown", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { e.Contributors[0].Model = "uncataloged" }},
		{"current-base", "current_base_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.CurrentBaseSHA = otherSHA }},
		{"current-head", "current_head_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.CurrentHeadSHA = otherSHA }},
		{"launch-round", "execution_request_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Execution.RequestID = "request-round-1"
		}},
		{"launch-base", "execution_base_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Execution.BaseSHA = otherSHA }},
		{"launch-head", "execution_head_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Execution.HeadSHA = otherSHA }},
		{"launch-harness", "execution_harness_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Execution.Harness = "claude" }},
		{"launch-version", "execution_version_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Execution.Version = "18.1.10" }},
		{"requested-lower-glm", "execution_requested_model_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Execution.RequestedModel = "zai/glm-4.7"
		}},
		{"effective-lower-glm", "execution_effective_model_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Execution.EffectiveModel = stringPointer("zai/glm-5-turbo")
		}},
		{"requested-reasoning", "execution_requested_reasoning_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Execution.RequestedReasoning = "low" }},
		{"effective-reasoning", "execution_effective_reasoning_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Execution.EffectiveReasoning = stringPointer("low")
		}},
		{"publication-round", "publication_request_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Publication.RequestID = "request-round-1"
		}},
		{"publication-repository", "publication_repository_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.Repo = "example/other" }},
		{"publication-pr", "publication_pr_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.PR++ }},
		{"publication-author", "publication_author_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.Author = "other[bot]" }},
		{"publication-human", "publication_author_type_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.AuthorType = "User" }},
		{"publication-comment", "publication_state_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.State = "COMMENTED" }},
		{"publication-changes-requested", "publication_state_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Publication.State = "CHANGES_REQUESTED"
		}},
		{"publication-old-head", "publication_head_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Publication.CommitSHA = otherSHA }},
		{"coverage-round", "coverage_request_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Coverage.RequestID = "request-round-1"
		}},
		{"coverage-base", "coverage_base_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.BaseSHA = otherSHA }},
		{"coverage-head", "coverage_head_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.HeadSHA = otherSHA }},
		{"delivered-diff", "coverage_delivered_digest_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.DiffDigest = otherDigest }},
		{"reviewed-diff", "coverage_reviewed_digest_mismatch", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) {
			o.Coverage.ReviewedDiffDigest = otherDigest
		}},
		{"partial-delivery", "coverage_delivery_incomplete", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.Full = false }},
		{"truncated-diff", "coverage_truncated", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.Truncated = true }},
		{"partial-review", "coverage_review_incomplete", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.ReviewedComplete = false }},
		{"unresolved-findings", "unresolved_findings", func(c *Catalog, e *ReviewExpectation, o *ReviewObservations) { o.Coverage.UnresolvedFindings = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog, expectation, observations := reviewComparisonFixture(t)
			tc.edit(&catalog, &expectation, &observations)
			result, err := CompareReviewEvidence(catalog, expectation, observations)
			if err != nil || result.Status != "mismatch" || !contains(result.Reasons, tc.reason) {
				t.Fatalf("contradiction was not rejected: %#v, %v; want %s", result, err, tc.reason)
			}
			assertReviewComparisonBoundary(t, result)
		})
	}
}

func TestCompareReviewEvidenceMissingObservationsStayIncomplete(t *testing.T) {
	cases := []struct {
		name, reason string
		edit         func(*ReviewExpectation, *ReviewObservations)
	}{
		{"all-observations", "execution_missing", func(e *ReviewExpectation, o *ReviewObservations) { *o = ReviewObservations{} }},
		{"current-base", "current_base_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.CurrentBaseSHA = "" }},
		{"current-head", "current_head_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.CurrentHeadSHA = "" }},
		{"execution", "execution_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Execution = nil }},
		{"unknown-effective-model", "execution_effective_model_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Execution.EffectiveModel = nil }},
		{"empty-effective-model", "execution_effective_model_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Execution.EffectiveModel = stringPointer("") }},
		{"unknown-effective-reasoning", "execution_effective_reasoning_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Execution.EffectiveReasoning = nil }},
		{"launch-evidence", "execution_evidence_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Execution.EvidenceRef = "" }},
		{"publication", "publication_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Publication = nil }},
		{"review-id", "publication_review_id_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Publication.ReviewID = 0 }},
		{"publication-pr", "publication_pr_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Publication.PR = 0 }},
		{"readback", "publication_readback_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Publication.ReadbackRef = "" }},
		{"coverage", "coverage_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Coverage = nil }},
		{"delivered-digest", "coverage_delivered_digest_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Coverage.DiffDigest = "" }},
		{"reviewed-digest", "coverage_reviewed_digest_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Coverage.ReviewedDiffDigest = "" }},
		{"coverage-evidence", "coverage_evidence_missing", func(e *ReviewExpectation, o *ReviewObservations) { o.Coverage.EvidenceRef = "" }},
		{"contributors", "contributors_missing", func(e *ReviewExpectation, o *ReviewObservations) { e.Contributors = nil }},
		{"contributor-route", "contributor_route_missing", func(e *ReviewExpectation, o *ReviewObservations) { e.Contributors[0].Model = "" }},
		{"contributor-evidence", "contributor_evidence_missing", func(e *ReviewExpectation, o *ReviewObservations) { e.Contributors[0].EvidenceRef = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog, expectation, observations := reviewComparisonFixture(t)
			tc.edit(&expectation, &observations)
			result, err := CompareReviewEvidence(catalog, expectation, observations)
			if err != nil || result.Status != "incomplete" || !contains(result.Reasons, tc.reason) {
				t.Fatalf("missing fact became a match or contradiction: %#v, %v", result, err)
			}
			assertReviewComparisonBoundary(t, result)
		})
	}
}

func TestCompareReviewEvidenceActualRepairContributorOverridesInitialIndependence(t *testing.T) {
	catalog, expectation, observations := reviewComparisonFixture(t)
	// The initial author is independent, but a subsequent repair used the same
	// model family as this review. Every declared contributor must be checked.
	expectation.Contributors = append(expectation.Contributors,
		ReviewContributor{Harness: "claude", Model: "claude-opus-5", EvidenceRef: "https://example.invalid/contributions/second"},
		ReviewContributor{Harness: "omp", Model: "zai/glm-5.3", EvidenceRef: "https://example.invalid/contributions/repair"},
	)
	result, err := CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || result.Status != "mismatch" || !reflect.DeepEqual(result.Reasons, []string{"contributor_family_conflict"}) {
		t.Fatalf("repair family was omitted: %#v, %v", result, err)
	}
	assertReviewComparisonBoundary(t, result)
	expectation.Contributors = append(expectation.Contributors, expectation.Contributors[2])
	for left, right := 0, len(expectation.Contributors)-1; left < right; left, right = left+1, right-1 {
		expectation.Contributors[left], expectation.Contributors[right] = expectation.Contributors[right], expectation.Contributors[left]
	}
	reordered, err := CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || !reflect.DeepEqual(result, reordered) {
		t.Fatalf("contributor order or duplication changed result: %#v, %v", reordered, err)
	}
}

func TestCompareReviewEvidenceCatalogFamilyAndQualityCannotBeSelfDeclared(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		edit         func(*Catalog)
	}{
		{"unknown-reviewer-family", "reviewer_family_unknown", func(c *Catalog) { c.Harnesses[2].Models[0].ModelFamily = "unknown" }},
		{"empty-contributor-family", "contributor_family_unknown", func(c *Catalog) { c.Harnesses[0].Models[0].ModelFamily = "" }},
		{"lower-quality-reviewer", "reviewer_quality_below_frontier", func(c *Catalog) { c.Harnesses[2].Models[0].QualityTier = "balanced" }},
		{"range-reasoning", "reviewer_reasoning_unsupported", func(c *Catalog) { c.Harnesses[2].Ranges[0].ReasoningLevels = []string{"max"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, expectation, observations := reviewComparisonFixture(t)
			tc.edit(&catalog)
			var err error
			expectation.CatalogDigest, err = Digest(catalog)
			if err != nil {
				t.Fatal(err)
			}
			result, err := CompareReviewEvidence(catalog, expectation, observations)
			if err != nil || result.Status != "mismatch" || !contains(result.Reasons, tc.reason) {
				t.Fatalf("caller labels overrode catalog: %#v, %v", result, err)
			}
			assertReviewComparisonBoundary(t, result)
		})
	}
}

func TestCompareReviewEvidenceAuditCannotOverrideOwnerReadback(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		edit         func(*ReviewObservations)
	}{
		{"wrong-review", "audit_publication_review_id_mismatch", func(o *ReviewObservations) { o.Audit.ID++ }},
		{"wrong-author", "audit_publication_author_mismatch", func(o *ReviewObservations) { o.Audit.Author = "other[bot]" }},
		{"human-author", "audit_publication_author_type_mismatch", func(o *ReviewObservations) { o.Audit.AuthorType = "User" }},
		{"wrong-state", "audit_publication_state_mismatch", func(o *ReviewObservations) { o.Audit.State = "COMMENTED" }},
		{"approved-audit-rejected-readback", "publication_state_mismatch", func(o *ReviewObservations) { o.Publication.State = "CHANGES_REQUESTED" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, expectation, observations := reviewComparisonFixture(t)
			observations.Audit = &InvocationAudit{ID: 123, State: "APPROVED", Author: "diff-guard[bot]", AuthorType: "Bot"}
			tc.edit(&observations)
			result, err := CompareReviewEvidence(catalog, expectation, observations)
			if err != nil || result.Status != "mismatch" || !contains(result.Reasons, tc.reason) {
				t.Fatalf("audit was allowed to override readback: %#v, %v", result, err)
			}
			assertReviewComparisonBoundary(t, result)
		})
	}
	// A plausible final audit after an interrupted or uncertain execution still
	// requires effective settings and request-bound publication readback.
	catalog, expectation, observations := reviewComparisonFixture(t)
	observations.Audit = &InvocationAudit{ID: 123, State: "APPROVED", Author: "diff-guard[bot]", AuthorType: "Bot"}
	observations.Execution.EffectiveModel, observations.Execution.EffectiveReasoning = nil, nil
	observations.Publication = nil
	result, err := CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || result.Status != "incomplete" || !contains(result.Reasons, "publication_missing") || !contains(result.Reasons, "execution_effective_model_missing") {
		t.Fatalf("uncertain process or audit proved approval: %#v, %v", result, err)
	}
	assertReviewComparisonBoundary(t, result)
	observations.CurrentHeadSHA = strings.Repeat("c", 40)
	result, err = CompareReviewEvidence(catalog, expectation, observations)
	if err != nil || result.Status != "mismatch" || !contains(result.Reasons, "publication_missing") || !contains(result.Reasons, "current_head_mismatch") {
		t.Fatalf("missing facts masked contradiction: %#v, %v", result, err)
	}
}

func TestCompareReviewEvidenceDefendsDirectInputs(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		edit       func(*Catalog, *ReviewExpectation)
	}{
		{"unsupported-expectation", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.SchemaVersion = 2 }},
		{"disabled-coverage", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.RequireFullCoverage = false }},
		{"missing-request", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.RequestID = "" }},
		{"abbreviated-head", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.HeadSHA = e.HeadSHA[:7] }},
		{"noncanonical-version", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.Reviewer.Version = "018.1.11" }},
		{"overflow-version", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.Reviewer.Version = strings.Repeat("9", 100) + ".1.11" }},
		{"missing-diff-digest", "review_expectation_invalid", func(c *Catalog, e *ReviewExpectation) { e.DiffDigest = "" }},
		{"unsupported-catalog", "review_catalog_invalid", func(c *Catalog, e *ReviewExpectation) { c.SchemaVersion = 2 }},
		{"duplicate-harness", "review_catalog_invalid", func(c *Catalog, e *ReviewExpectation) { c.Harnesses = append(c.Harnesses, c.Harnesses[0]) }},
		{"duplicate-model", "review_catalog_invalid", func(c *Catalog, e *ReviewExpectation) {
			c.Harnesses[2].Models = append(c.Harnesses[2].Models, c.Harnesses[2].Models[0])
		}},
		{"ambiguous-version", "review_catalog_ambiguous_version", func(c *Catalog, e *ReviewExpectation) {
			c.Harnesses[2].Ranges = append(c.Harnesses[2].Ranges, c.Harnesses[2].Ranges[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, expectation, observations := reviewComparisonFixture(t)
			tc.edit(&catalog, &expectation)
			result, err := CompareReviewEvidence(catalog, expectation, observations)
			if err == nil || err.Error() != tc.code || result.Status != "mismatch" || !contains(result.Reasons, tc.code) {
				t.Fatalf("invalid direct input passed: %#v, %v", result, err)
			}
			assertReviewComparisonBoundary(t, result)
		})
	}
}
