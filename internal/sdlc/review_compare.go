package sdlc

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// ReviewComparison compares caller-supplied declarations. Even a complete match
// is not authenticated review evidence, approval, or an assessment of merge gates.
type ReviewComparison struct {
	SchemaVersion  int      `json:"schemaVersion"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Reasons        []string `json:"reasons"`
	ApprovalProof  string   `json:"approvalProof"`
	MergeReadiness string   `json:"mergeReadiness"`
	Authenticity   string   `json:"authenticity"`
}

type reviewComparisonState struct {
	result ReviewComparison
	codes  map[string]bool
}

var reviewComparisonSHAPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// CompareReviewEvidence is pure: it never resolves evidence references, probes a
// harness, reads Git or a review service, launches a provider, or grants authority.
// Contradictions take precedence over missing observations. Codes are sorted and
// deduplicated so repeated contributors and input order cannot change the result.
func CompareReviewEvidence(catalog Catalog, expectation ReviewExpectation, observations ReviewObservations) (ReviewComparison, error) {
	s := reviewComparisonState{result: ReviewComparison{
		SchemaVersion: 1, Kind: "noise.sdlc.review-comparison", Status: "match", Reasons: []string{},
		ApprovalProof: "not-established", MergeReadiness: "not-assessed", Authenticity: "not-assessed",
	}, codes: make(map[string]bool)}
	if !validReviewComparisonExpectation(expectation) {
		return s.invalid("review_expectation_invalid")
	}
	if err := ValidateCatalog(catalog); err != nil {
		return s.invalid("review_catalog_invalid")
	}
	catalogDigest, err := Digest(catalog)
	if err != nil {
		return s.invalid("review_catalog_invalid")
	}
	s.equal(expectation.CatalogDigest, catalogDigest, "catalog_digest")

	reviewerHarness, reviewerModel := reviewComparisonModel(catalog, expectation.Reviewer.Harness, expectation.Reviewer.Model)
	if reviewerHarness == nil {
		s.mismatch("reviewer_harness_unknown")
	} else if reviewerModel == nil {
		s.mismatch("reviewer_model_unknown")
	} else {
		if !reviewComparisonKnownFamily(reviewerModel.ModelFamily) {
			s.mismatch("reviewer_family_unknown")
		} else {
			s.equal(reviewerModel.ModelFamily, expectation.Reviewer.ModelFamily, "reviewer_family")
		}
		if reviewerModel.QualityTier != "frontier" {
			s.mismatch("reviewer_quality_below_frontier")
		}
		if !normalizedReasoningLevels[expectation.Reviewer.Reasoning] || !contains(reviewerModel.ReasoningLevels, expectation.Reviewer.Reasoning) {
			s.mismatch("reviewer_reasoning_unsupported")
		}
	}
	if reviewerHarness != nil {
		matches := 0
		for _, candidate := range reviewerHarness.Ranges {
			if !validInvocationVersion(candidate.Minimum) || !validInvocationVersion(candidate.MaximumExclusive) {
				return s.invalid("review_catalog_invalid")
			}
			if compareVersions(candidate.Minimum, expectation.Reviewer.Version) <= 0 && compareVersions(expectation.Reviewer.Version, candidate.MaximumExclusive) < 0 {
				matches++
				if !contains(candidate.ReasoningLevels, expectation.Reviewer.Reasoning) {
					s.mismatch("reviewer_reasoning_unsupported")
				}
			}
		}
		if matches == 0 {
			s.mismatch("reviewer_version_uncataloged")
		} else if matches > 1 {
			return s.invalid("review_catalog_ambiguous_version")
		}
	}
	if len(expectation.Contributors) == 0 {
		s.missing("contributors_missing")
	}
	for _, contributor := range expectation.Contributors {
		s.required(contributor.EvidenceRef, "contributor_evidence")
		if contributor.Harness == "" || contributor.Model == "" {
			s.missing("contributor_route_missing")
			continue
		}
		harness, model := reviewComparisonModel(catalog, contributor.Harness, contributor.Model)
		if harness == nil {
			s.mismatch("contributor_harness_unknown")
		} else if model == nil {
			s.mismatch("contributor_model_unknown")
		} else if !reviewComparisonKnownFamily(model.ModelFamily) {
			s.mismatch("contributor_family_unknown")
		} else if reviewerModel != nil && model.ModelFamily == reviewerModel.ModelFamily {
			s.mismatch("contributor_family_conflict")
		}
	}

	s.equal(expectation.BaseSHA, observations.CurrentBaseSHA, "current_base")
	s.equal(expectation.HeadSHA, observations.CurrentHeadSHA, "current_head")
	if execution := observations.Execution; execution == nil {
		s.missing("execution_missing")
	} else {
		s.equal(expectation.RequestID, execution.RequestID, "execution_request")
		s.equal(expectation.BaseSHA, execution.BaseSHA, "execution_base")
		s.equal(expectation.HeadSHA, execution.HeadSHA, "execution_head")
		s.equal(expectation.Reviewer.Harness, execution.Harness, "execution_harness")
		s.equal(expectation.Reviewer.Version, execution.Version, "execution_version")
		s.equal(expectation.Reviewer.Model, execution.RequestedModel, "execution_requested_model")
		s.equal(expectation.Reviewer.Reasoning, execution.RequestedReasoning, "execution_requested_reasoning")
		if execution.EffectiveModel == nil {
			s.missing("execution_effective_model_missing")
		} else {
			s.equal(expectation.Reviewer.Model, *execution.EffectiveModel, "execution_effective_model")
		}
		if execution.EffectiveReasoning == nil {
			s.missing("execution_effective_reasoning_missing")
		} else {
			s.equal(expectation.Reviewer.Reasoning, *execution.EffectiveReasoning, "execution_effective_reasoning")
		}
		s.required(execution.EvidenceRef, "execution_evidence")
	}
	if publication := observations.Publication; publication == nil {
		s.missing("publication_missing")
	} else {
		s.equal(expectation.RequestID, publication.RequestID, "publication_request")
		s.equal(expectation.Repo, publication.Repo, "publication_repository")
		s.equalUint(expectation.PR, publication.PR, "publication_pr")
		if publication.ReviewID == 0 {
			s.missing("publication_review_id_missing")
		}
		s.equal(expectation.ExpectedBot, publication.Author, "publication_author")
		s.equal("Bot", publication.AuthorType, "publication_author_type")
		s.equal("APPROVED", publication.State, "publication_state")
		s.equal(expectation.HeadSHA, publication.CommitSHA, "publication_head")
		s.required(publication.ReadbackRef, "publication_readback")
	}
	if coverage := observations.Coverage; coverage == nil {
		s.missing("coverage_missing")
	} else {
		s.equal(expectation.RequestID, coverage.RequestID, "coverage_request")
		s.equal(expectation.BaseSHA, coverage.BaseSHA, "coverage_base")
		s.equal(expectation.HeadSHA, coverage.HeadSHA, "coverage_head")
		s.equal(expectation.DiffDigest, coverage.DiffDigest, "coverage_delivered_digest")
		s.equal(expectation.DiffDigest, coverage.ReviewedDiffDigest, "coverage_reviewed_digest")
		if !coverage.Full {
			s.mismatch("coverage_delivery_incomplete")
		}
		if coverage.Truncated {
			s.mismatch("coverage_truncated")
		}
		if !coverage.ReviewedComplete {
			s.mismatch("coverage_review_incomplete")
		}
		if coverage.UnresolvedFindings > 0 {
			s.mismatch("unresolved_findings")
		}
		s.required(coverage.EvidenceRef, "coverage_evidence")
	}
	// A missing final-line audit can be reconciled by request-bound owner readback.
	// When present it must agree; it can never override conflicting readback.
	if audit := observations.Audit; audit != nil {
		if audit.ID == 0 {
			s.missing("audit_review_id_missing")
		}
		s.equal(expectation.ExpectedBot, audit.Author, "audit_author")
		s.equal("Bot", audit.AuthorType, "audit_author_type")
		s.equal("APPROVED", audit.State, "audit_state")
		if publication := observations.Publication; publication != nil {
			if audit.ID != 0 && publication.ReviewID != 0 {
				s.equalUint(publication.ReviewID, audit.ID, "audit_publication_review_id")
			}
			// Compare even non-approved observations to prevent one supplied group
			// from being silently preferred over another contradictory group.
			if publication.Author != "" && audit.Author != "" {
				s.equal(publication.Author, audit.Author, "audit_publication_author")
			}
			if publication.AuthorType != "" && audit.AuthorType != "" {
				s.equal(publication.AuthorType, audit.AuthorType, "audit_publication_author_type")
			}
			if publication.State != "" && audit.State != "" {
				s.equal(publication.State, audit.State, "audit_publication_state")
			}
		}
	}
	return s.finish(), nil
}

func validReviewComparisonExpectation(expectation ReviewExpectation) bool {
	if expectation.SchemaVersion != 1 || !expectation.RequireFullCoverage || expectation.PR == 0 ||
		!reviewComparisonSHAPattern.MatchString(expectation.BaseSHA) || !reviewComparisonSHAPattern.MatchString(expectation.HeadSHA) ||
		!digestPattern.MatchString(expectation.DiffDigest) || !digestPattern.MatchString(expectation.PolicyDigest) || !digestPattern.MatchString(expectation.CatalogDigest) ||
		!validInvocationVersion(expectation.Reviewer.Version) {
		return false
	}
	for _, value := range []string{expectation.RequestID, expectation.Repo, expectation.ExpectedBot, expectation.Reviewer.Harness, expectation.Reviewer.Model, expectation.Reviewer.ModelFamily, expectation.Reviewer.Reasoning} {
		if strings.TrimSpace(value) == "" || !validInvocationText(value) {
			return false
		}
	}
	return true
}

func reviewComparisonModel(catalog Catalog, harnessID, modelID string) (*Harness, *Model) {
	for index := range catalog.Harnesses {
		harness := &catalog.Harnesses[index]
		if harness.ID == harnessID {
			return harness, findModel(*harness, modelID)
		}
	}
	return nil, nil
}

func reviewComparisonKnownFamily(family string) bool {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "", "unknown", "unverified", "unspecified":
		return false
	}
	return validInvocationText(family) && strings.TrimSpace(family) == family
}

func (s *reviewComparisonState) required(value, field string) {
	if strings.TrimSpace(value) == "" {
		s.missing(field + "_missing")
	}
}

func (s *reviewComparisonState) equal(expected, observed, field string) {
	if observed == "" {
		s.missing(field + "_missing")
	} else if expected != observed {
		s.mismatch(field + "_mismatch")
	}
}

func (s *reviewComparisonState) equalUint(expected, observed uint64, field string) {
	if observed == 0 {
		s.missing(field + "_missing")
	} else if expected != observed {
		s.mismatch(field + "_mismatch")
	}
}

func (s *reviewComparisonState) missing(code string) {
	if s.result.Status == "match" {
		s.result.Status = "incomplete"
	}
	s.codes[code] = true
}

func (s *reviewComparisonState) mismatch(code string) {
	s.result.Status = "mismatch"
	s.codes[code] = true
}

func (s *reviewComparisonState) finish() ReviewComparison {
	for code := range s.codes {
		s.result.Reasons = append(s.result.Reasons, code)
	}
	sort.Strings(s.result.Reasons)
	return s.result
}

func (s *reviewComparisonState) invalid(code string) (ReviewComparison, error) {
	s.mismatch(code)
	return s.finish(), errors.New(code)
}
