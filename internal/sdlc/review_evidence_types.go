package sdlc

// ReviewEvidenceInput is a bounded declaration for policy comparison. Parsing
// validates shape and values, never the authenticity or truth of observations.
type ReviewEvidenceInput struct {
	SchemaVersion int                `json:"schemaVersion"`
	Expectation   ReviewExpectation  `json:"expectation"`
	Observations  ReviewObservations `json:"observations"`
}

type ReviewExpectation struct {
	SchemaVersion       int                   `json:"schemaVersion"`
	RequestID           string                `json:"requestID"`
	Repo                string                `json:"repo"`
	PR                  uint64                `json:"pr"`
	BaseSHA             string                `json:"baseSHA"`
	HeadSHA             string                `json:"headSHA"`
	DiffDigest          string                `json:"diffDigest"`
	PolicyDigest        string                `json:"policyDigest"`
	CatalogDigest       string                `json:"catalogDigest"`
	Reviewer            ReviewReviewerBinding `json:"reviewer"`
	Contributors        []ReviewContributor   `json:"contributors"`
	ExpectedBot         string                `json:"expectedBot"`
	RequireFullCoverage bool                  `json:"requireFullCoverage"`
}

type ReviewReviewerBinding struct {
	Harness     string `json:"harness"`
	Version     string `json:"version"`
	Model       string `json:"model"`
	ModelFamily string `json:"modelFamily"`
	Reasoning   string `json:"reasoning"`
}

// ReviewContributor describes actual candidate contributors, including repair
// agents. EvidenceRef is a public locator supplied by the caller and never read.
type ReviewContributor struct {
	Harness     string `json:"harness"`
	Model       string `json:"model"`
	EvidenceRef string `json:"evidenceRef"`
}

// A missing observation group means no observation was supplied. Empty current
// head and null effective settings mean explicitly unknown, not matching values.
type ReviewObservations struct {
	CurrentBaseSHA string                        `json:"currentBaseSHA"`
	CurrentHeadSHA string                        `json:"currentHeadSHA"`
	Execution      *ReviewExecutionObservation   `json:"execution,omitempty"`
	Publication    *ReviewPublicationObservation `json:"publication,omitempty"`
	Coverage       *ReviewCoverageObservation    `json:"coverage,omitempty"`
	Audit          *InvocationAudit              `json:"audit,omitempty"`
}

type ReviewExecutionObservation struct {
	RequestID          string  `json:"requestID"`
	BaseSHA            string  `json:"baseSHA"`
	HeadSHA            string  `json:"headSHA"`
	Harness            string  `json:"harness"`
	Version            string  `json:"version"`
	RequestedModel     string  `json:"requestedModel"`
	RequestedReasoning string  `json:"requestedReasoning"`
	EffectiveModel     *string `json:"effectiveModel"`
	EffectiveReasoning *string `json:"effectiveReasoning"`
	EvidenceRef        string  `json:"evidenceRef"`
}

type ReviewPublicationObservation struct {
	RequestID   string `json:"requestID"`
	Repo        string `json:"repo"`
	PR          uint64 `json:"pr"`
	ReviewID    uint64 `json:"reviewID"`
	Author      string `json:"author"`
	AuthorType  string `json:"authorType"`
	State       string `json:"state"`
	CommitSHA   string `json:"commitSHA"`
	ReadbackRef string `json:"readbackRef"`
}

type ReviewCoverageObservation struct {
	RequestID          string `json:"requestID"`
	BaseSHA            string `json:"baseSHA"`
	HeadSHA            string `json:"headSHA"`
	DiffDigest         string `json:"diffDigest"`
	ReviewedDiffDigest string `json:"reviewedDiffDigest"`
	Full               bool   `json:"full"`
	Truncated          bool   `json:"truncated"`
	ReviewedComplete   bool   `json:"reviewedComplete"`
	UnresolvedFindings uint64 `json:"unresolvedFindings"`
	EvidenceRef        string `json:"evidenceRef"`
}
