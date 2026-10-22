package sdlc

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ReviewRequestInput supplies candidate metadata, not evidence of its truth.
// Contributors must enumerate actual agent contributors, including repairs;
// their completeness and the candidate identity remain external owner duties.
type ReviewRequestInput struct {
	RequestID      string              `json:"requestID"`
	Repo           string              `json:"repo"`
	PR             uint64              `json:"pr"`
	BaseSHA        string              `json:"baseSHA"`
	HeadSHA        string              `json:"headSHA"`
	DiffDigest     string              `json:"diffDigest"`
	HarnessVersion string              `json:"harnessVersion"`
	ExpectedBot    string              `json:"expectedBot"`
	Contributors   []ReviewContributor `json:"contributors"`
}

func ParseReviewRequestInput(data []byte) (ReviewRequestInput, error) {
	var input ReviewRequestInput
	if err := checkReviewJSON(data); err != nil {
		return input, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return input, fmt.Errorf("review request input must be an object")
	}
	// encoding/json matches struct fields case-insensitively. Enforce canonical
	// keys before decoding so aliases cannot overwrite a separately named key.
	allowed := map[string]bool{"requestID": true, "repo": true, "pr": true,
		"baseSHA": true, "headSHA": true, "diffDigest": true, "harnessVersion": true,
		"expectedBot": true, "contributors": true}
	for key := range fields {
		if !allowed[key] {
			return input, fmt.Errorf("review request has an unknown field")
		}
	}
	if raw, exists := fields["contributors"]; exists {
		var contributors []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &contributors); err != nil {
			return input, fmt.Errorf("invalid contributor declarations")
		}
		for _, contributor := range contributors {
			if contributor == nil {
				return input, fmt.Errorf("contributor declaration must be an object")
			}
			for key := range contributor {
				if key != "harness" && key != "model" && key != "evidenceRef" {
					return input, fmt.Errorf("contributor has an unknown field")
				}
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return ReviewRequestInput{}, fmt.Errorf("invalid review request input: %w", err)
	}
	return input, nil
}

// BuildReviewExpectation derives review settings from a verified adoption. It
// grants no launch authority and does not authenticate caller-supplied metadata.
func BuildReviewExpectation(repositoryRoot, target string, input ReviewRequestInput) (ReviewExpectation, error) {
	var empty ReviewExpectation
	if !validInvocationVersion(input.HarnessVersion) {
		return empty, fmt.Errorf("review request requires a canonical harness version")
	}
	if len(input.Contributors) == 0 {
		return empty, fmt.Errorf("review request requires actual agent contributor declarations, including repairs")
	}
	guidance, err := ExecutionShow(repositoryRoot, target, "review.formal", input.HarnessVersion)
	if err != nil {
		return empty, err
	}
	if guidance["state"] != "available" {
		return empty, fmt.Errorf("review request requires the adopted catalog; review a project update plan")
	}
	roles := guidance["roles"].([]any)
	if len(roles) != 1 {
		return empty, fmt.Errorf("review request requires one adopted formal-review role")
	}
	role := roles[0].(map[string]any)
	if role["state"] != "declared-contracts" {
		return empty, fmt.Errorf("adopted formal-review invocation is unresolved or unsupported; no substitute selected")
	}
	binding := role["binding"].(Binding)
	catalog, err := LoadCatalog(repositoryRoot)
	if err != nil {
		return empty, err
	}
	digest, err := Digest(catalog)
	if err != nil {
		return empty, err
	}
	if digest != guidance["catalogDigest"] {
		return empty, fmt.Errorf("catalog changed while constructing review expectation; read current policy again")
	}
	find := func(harnessID, modelID string) *Model {
		for _, harness := range catalog.Harnesses {
			if harness.ID == harnessID {
				return findModel(harness, modelID)
			}
		}
		return nil
	}
	reviewer := find(pointerValue(binding.Harness), pointerValue(binding.Model))
	if reviewer == nil || reviewer.QualityTier != "frontier" || !reviewComparisonKnownFamily(reviewer.ModelFamily) {
		return empty, fmt.Errorf("adopted reviewer does not satisfy high-capability review")
	}
	for _, contributor := range input.Contributors {
		model := find(contributor.Harness, contributor.Model)
		if model == nil || !reviewComparisonKnownFamily(model.ModelFamily) {
			return empty, fmt.Errorf("actual contributor route is unknown to the adopted catalog")
		}
		if model.ModelFamily == reviewer.ModelFamily {
			return empty, fmt.Errorf("adopted reviewer is not independent from every actual contributor")
		}
	}
	expectation := ReviewExpectation{
		SchemaVersion: 1, RequestID: input.RequestID, Repo: input.Repo, PR: input.PR,
		BaseSHA: input.BaseSHA, HeadSHA: input.HeadSHA, DiffDigest: input.DiffDigest,
		PolicyDigest: guidance["policyDigest"].(string), CatalogDigest: digest,
		Reviewer: ReviewReviewerBinding{Harness: pointerValue(binding.Harness), Version: input.HarnessVersion,
			Model: pointerValue(binding.Model), ModelFamily: reviewer.ModelFamily, Reasoning: pointerValue(binding.Reasoning)},
		Contributors: append([]ReviewContributor(nil), input.Contributors...), ExpectedBot: input.ExpectedBot,
		RequireFullCoverage: true,
	}
	if err := ValidateReviewExpectation(repositoryRoot, expectation); err != nil {
		return empty, err
	}
	return expectation, nil
}

// ReadReviewInput reads only the caller-selected bounded metadata file. Any
// provenance locators inside that file remain unopened declarations.
func ReadReviewInput(path string) ([]byte, error) { return readEvidenceFile(path) }
