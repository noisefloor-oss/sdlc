package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func reviewEvidenceFixture(t *testing.T) sdlc.ReviewEvidenceInput {
	t.Helper()
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := sdlc.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := sdlc.Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	base, head, diff := strings.Repeat("a", 40), strings.Repeat("b", 40), "sha256:"+strings.Repeat("c", 64)
	model, reasoning := "claude-opus-5", "high"
	return sdlc.ReviewEvidenceInput{
		SchemaVersion: 1,
		Expectation: sdlc.ReviewExpectation{SchemaVersion: 1, RequestID: "fixture-round", Repo: "example/project", PR: 1,
			BaseSHA: base, HeadSHA: head, DiffDigest: diff, PolicyDigest: "sha256:" + strings.Repeat("d", 64), CatalogDigest: digest,
			Reviewer:     sdlc.ReviewReviewerBinding{Harness: "claude", Version: "2.1.263", Model: model, ModelFamily: "anthropic-claude-5", Reasoning: reasoning},
			Contributors: []sdlc.ReviewContributor{{Harness: "codex", Model: "gpt-5.6-sol", EvidenceRef: "fixture:contributor"}}, ExpectedBot: "noise-review[bot]", RequireFullCoverage: true},
		Observations: sdlc.ReviewObservations{CurrentBaseSHA: base, CurrentHeadSHA: head,
			Execution:   &sdlc.ReviewExecutionObservation{RequestID: "fixture-round", BaseSHA: base, HeadSHA: head, Harness: "claude", Version: "2.1.263", RequestedModel: model, RequestedReasoning: reasoning, EffectiveModel: &model, EffectiveReasoning: &reasoning, EvidenceRef: "fixture:launch"},
			Publication: &sdlc.ReviewPublicationObservation{RequestID: "fixture-round", Repo: "example/project", PR: 1, ReviewID: 9007199254740993, Author: "noise-review[bot]", AuthorType: "Bot", State: "APPROVED", CommitSHA: head, ReadbackRef: "fixture:readback"},
			Coverage:    &sdlc.ReviewCoverageObservation{RequestID: "fixture-round", BaseSHA: base, HeadSHA: head, DiffDigest: diff, ReviewedDiffDigest: diff, Full: true, ReviewedComplete: true, EvidenceRef: "fixture:coverage"}},
	}
}

func TestReviewCompareCLISeparatesMatchFromApproval(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	input := reviewEvidenceFixture(t)
	path := filepath.Join(t.TempDir(), "review.json")
	t.Setenv("PATH", t.TempDir())
	for _, state := range []string{"match", "mismatch", "incomplete"} {
		if state == "mismatch" {
			input.Observations.Publication.CommitSHA = input.Expectation.BaseSHA
		}
		if state == "incomplete" {
			input.Observations.Publication = nil
		}
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		output, err := captureStdout(t, func() error { return run([]string{"review", "compare", path}) })
		if (err == nil) != (state == "match") {
			t.Fatalf("wrong exit for %s: %v", state, err)
		}
		var result sdlc.ReviewComparison
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("missing comparison JSON: %v %s", err, output)
		}
		if result.Status != state || result.ApprovalProof != "not-established" || result.Authenticity != "not-assessed" || result.MergeReadiness != "not-assessed" {
			t.Fatalf("misleading result: %#v", result)
		}
	}
	for _, args := range [][]string{{"request"}, {"compare"}, {"compare", path, "extra"}, {"compare", "--input", path}, {"unknown"}} {
		if err := reviewEvidenceCommand(root, args); err == nil {
			t.Fatalf("invalid options accepted: %v", args)
		}
	}
}
