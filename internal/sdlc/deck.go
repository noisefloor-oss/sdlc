package sdlc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var (
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	catalogNamePattern = regexp.MustCompile(`^noise/harnesses@[0-9]+\.[0-9]+$`)
	harnessIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
)

func InteractionContract() map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"kind":          "noise.sdlc.interaction",
		"entrypoints": []map[string]any{{
			"id": "deck.project-added", "trigger": "after-project-added", "presentation": "optional-card",
			"title": "Choose how agents ship this project", "defaultAction": "skip",
			"actions": []string{"start-selection", "skip"}, "dismissalScope": "project",
			"projectCreationDependency": false,
			"eligibility": map[string]any{
				"showWhen": "no-adoption", "suppressWhen": []string{"verified-adoption", "project-dismissed"},
				"invalidAdoptionAction": "show-repair-state",
			},
			"persistentDeckSync": "separate-opt-in",
		}},
		"questions": []map[string]any{
			{"id": "authorship", "label": "Who primarily authors changes?", "type": "choice", "choices": []string{"agent", "mixed", "human"}, "default": "agent"},
			{"id": "delivery", "label": "What does this project deliver?", "type": "choice", "choices": []string{"software", "both", "research"}, "default": "software"},
			{"id": "risk", "label": "Does it contain named elevated-risk work?", "type": "choice", "choices": []string{"ordinary", "elevated"}, "default": "ordinary"},
			{"id": "highRiskClasses", "label": "Public high-risk class labels", "type": "string-list", "visibleWhen": map[string]string{"risk": "elevated"}, "default": []string{}},
			{"id": "routingPreference", "label": "How should eligible routes be ranked?", "type": "choice", "choices": []string{"balanced", "included-first", "capability-first"}, "default": "balanced"},
		},
		"endpoints": map[string]any{
			"bootstrap":        map[string]string{"method": "GET", "path": "/api/bootstrap"},
			"plan":             map[string]string{"method": "POST", "path": "/api/plan"},
			"adopt":            map[string]string{"method": "POST", "path": "/api/adopt", "confirmation": "planDigest"},
			"deckSync":         map[string]string{"method": "GET", "path": "/api/deck-sync"},
			"deckReceipt":      map[string]string{"method": "POST", "path": "/api/deck-receipt", "confirmation": "actionToken"},
			"onboardingStatus": map[string]string{"method": "GET", "path": "/api/onboarding"},
		},
	}
}

func DeckSyncBundle(catalog Catalog, template ExecutionTemplate) map[string]any {
	digest, _ := Digest(catalog)
	return map[string]any{
		"schemaVersion": 1,
		"kind":          "noise.sdlc.deck-sync",
		"authority": map[string]string{
			"catalog": "noise-sdlc", "inventory": "local-or-opted-in-deck", "bindings": "noise-sdlc",
		},
		"catalog": catalog, "catalogDigest": digest,
		"executionTemplates": []ExecutionTemplate{template}, "interaction": InteractionContract(),
	}
}

func ValidateDeckReceipt(receipt map[string]any) error {
	if intValue(receipt["schemaVersion"]) != 1 || stringValue(receipt["kind"]) != "noise.deck.sdlc-receipt" {
		return fmt.Errorf("unsupported Deck sync receipt")
	}
	if !digestPattern.MatchString(stringValue(receipt["catalogDigest"])) {
		return fmt.Errorf("Deck receipt has an invalid catalog digest")
	}
	if stringValue(receipt["deckVersion"]) == "" {
		return fmt.Errorf("Deck receipt lacks a public component version")
	}
	observedAt := stringValue(receipt["observedAt"])
	if _, err := time.Parse(time.RFC3339, observedAt); err != nil {
		return fmt.Errorf("Deck receipt has an invalid observation time")
	}
	inventory, ok := receipt["inventory"].(map[string]any)
	if !ok || intValue(inventory["schemaVersion"]) != 1 || stringValue(inventory["source"]) != "deck-runtime" || !catalogNamePattern.MatchString(stringValue(inventory["catalog"])) {
		return fmt.Errorf("Deck receipt contains an invalid normalized inventory")
	}
	seen := make(map[string]struct{})
	for _, rawHarness := range arrayValue(inventory["harnesses"]) {
		harness, ok := rawHarness.(map[string]any)
		if !ok {
			return fmt.Errorf("Deck receipt contains an invalid harness observation")
		}
		harnessID := stringValue(harness["id"])
		if !harnessIDPattern.MatchString(harnessID) {
			return fmt.Errorf("Deck receipt contains an invalid harness observation")
		}
		if _, duplicate := seen[harnessID]; duplicate {
			return fmt.Errorf("Deck receipt contains an invalid harness observation")
		}
		seen[harnessID] = struct{}{}
		if !oneOf(stringValue(harness["state"]), "absent", "installed", "ready", "enumerated") {
			return fmt.Errorf("Deck receipt may contain runtime state only")
		}
		rangeStatus := stringValue(harness["rangeStatus"])
		if rangeStatus != "unverified" && rangeStatus != "absent" {
			return fmt.Errorf("Deck receipt may not assert compatibility status")
		}
		for _, fact := range arrayValue(harness["unknown"]) {
			if !oneOf(stringValue(fact), "installed", "ready", "models", "version", "spend-authority") {
				return fmt.Errorf("Deck receipt may not introduce facts outside the inventory vocabulary")
			}
		}
		for _, rawModel := range arrayValue(harness["models"]) {
			model, ok := rawModel.(map[string]any)
			verified, verifiedOK := model["verified"].(bool)
			family, familyOK := model["modelFamily"]
			reasoning, reasoningOK := model["reasoningLevels"].([]any)
			if !ok || !publicModelPattern.MatchString(stringValue(model["id"])) || !verifiedOK || verified || !familyOK || family != nil ||
				stringValue(model["qualityTier"]) != "unknown" || stringValue(model["entitlement"]) != "unknown" ||
				stringValue(model["provenance"]) != "enumerated" || !reasoningOK || len(reasoning) != 0 {
				return fmt.Errorf("Deck receipt contains an invalid model observation")
			}
		}
	}
	return nil
}

func AcceptDeckReceipt(receipt map[string]any, target string, catalog Catalog) (map[string]any, error) {
	if err := ValidateDeckReceipt(receipt); err != nil {
		return nil, err
	}
	encoded, err := CanonicalBytes(receipt)
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(target, ".sdlc", "deck-sync.json"), encoded); err != nil {
		return nil, err
	}
	return DeckSyncStatus(target, catalog), nil
}

func PullDeckReceipt(rawURL, target string, catalog Catalog) (map[string]any, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isLoopbackDeckURL(parsed) {
		return nil, fmt.Errorf("Deck sync pull accepts loopback HTTP endpoints only")
	}
	dialer := &net.Dialer{}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !oneOf(host, "127.0.0.1", "::1", "localhost") {
			return nil, fmt.Errorf("Deck sync pull accepts loopback HTTP endpoints only")
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}}
	client := &http.Client{
		Timeout: 5 * time.Second, Transport: transport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !isLoopbackDeckURL(request.URL) {
				return fmt.Errorf("Deck sync pull redirects must remain on loopback")
			}
			return nil
		},
	}
	response, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("cannot pull Deck sync receipt: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1_048_577))
	if err != nil {
		return nil, err
	}
	if len(payload) > 1_048_576 {
		return nil, fmt.Errorf("Deck sync receipt is oversized")
	}
	var receipt map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&receipt); err != nil {
		return nil, fmt.Errorf("Deck sync endpoint returned invalid JSON")
	}
	return AcceptDeckReceipt(receipt, target, catalog)
}

func isLoopbackDeckURL(parsed *url.URL) bool {
	return parsed != nil && parsed.Scheme == "http" && parsed.User == nil &&
		oneOf(parsed.Hostname(), "127.0.0.1", "::1", "localhost")
}

func DeckSyncStatus(target string, catalog Catalog) map[string]any {
	target, _ = filepath.Abs(target)
	receiptPath := filepath.Join(target, ".sdlc", "deck-sync.json")
	expectedDigest, _ := Digest(catalog)
	if !fileExists(receiptPath) {
		return map[string]any{
			"state": "disabled", "mode": "standalone", "catalog": catalog.Catalog,
			"catalogDigest": expectedDigest, "lastKnownGood": nil,
		}
	}
	var receipt map[string]any
	if err := LoadFlexibleJSON(receiptPath, &receipt); err != nil {
		return staleDeckStatus(catalog, expectedDigest, receiptPath, err)
	}
	if err := ValidateDeckReceipt(receipt); err != nil {
		return staleDeckStatus(catalog, expectedDigest, receiptPath, err)
	}
	state := "catalog-drift"
	if stringValue(receipt["catalogDigest"]) == expectedDigest {
		state = "in-sync"
	}
	uncataloged, unverified := compareDeckInventory(receipt, catalog)
	return map[string]any{
		"state": state, "catalog": catalog.Catalog, "catalogDigest": expectedDigest,
		"deckAcceptedCatalogDigest": receipt["catalogDigest"], "deckVersion": receipt["deckVersion"],
		"observedAt": receipt["observedAt"], "uncatalogedHarnesses": uncataloged,
		"unverifiedHarnesses": unverified, "lastKnownGood": receiptPath,
	}
}

func CatalogCandidates(target string, catalog Catalog) (map[string]any, error) {
	receiptPath := filepath.Join(target, ".sdlc", "deck-sync.json")
	if !fileExists(receiptPath) {
		return nil, fmt.Errorf("no last-known-good Deck receipt")
	}
	var receipt map[string]any
	if err := LoadFlexibleJSON(receiptPath, &receipt); err != nil {
		return nil, err
	}
	if err := ValidateDeckReceipt(receipt); err != nil {
		return nil, err
	}
	knownHarnesses := make(map[string]Harness)
	for _, harness := range catalog.Harnesses {
		knownHarnesses[harness.ID] = harness
	}
	observations := arrayValue(receipt["inventory"].(map[string]any)["harnesses"])
	sort.Slice(observations, func(left, right int) bool {
		return stringValue(observations[left].(map[string]any)["id"]) < stringValue(observations[right].(map[string]any)["id"])
	})
	candidates := make([]map[string]any, 0)
	for _, rawObservation := range observations {
		observation := rawObservation.(map[string]any)
		harnessID := stringValue(observation["id"])
		observedModels := observedModelIDs(observation)
		harness, known := knownHarnesses[harnessID]
		if !known {
			candidates = append(candidates, candidateRow("new-harness", harnessID, observation["version"], observedModels, "author descriptor and contract tests"))
			continue
		}
		version := stringValue(observation["version"])
		if version != "" && matchingRange(harness, version) == nil {
			candidates = append(candidates, candidateRow("new-version", harnessID, observation["version"], []string{}, "run version contract suite"))
		}
		knownModels := make(map[string]struct{})
		for _, model := range harness.Models {
			knownModels[model.ID] = struct{}{}
		}
		newModels := make([]string, 0)
		for _, model := range observedModels {
			if _, known := knownModels[model]; !known {
				newModels = append(newModels, model)
			}
		}
		if len(newModels) > 0 {
			candidates = append(candidates, candidateRow("new-models", harnessID, observation["version"], newModels, "curate model family, tier, entitlement, and reasoning evidence"))
		}
	}
	return map[string]any{
		"schemaVersion": 1, "kind": "noise.sdlc.catalog-candidates",
		"sourceObservedAt": receipt["observedAt"], "supportClaim": false, "candidates": candidates,
	}, nil
}

func compareDeckInventory(receipt map[string]any, catalog Catalog) ([]string, []string) {
	known := make(map[string]Harness)
	for _, harness := range catalog.Harnesses {
		known[harness.ID] = harness
	}
	uncataloged, unverified := []string{}, []string{}
	for _, raw := range arrayValue(receipt["inventory"].(map[string]any)["harnesses"]) {
		observation := raw.(map[string]any)
		harnessID := stringValue(observation["id"])
		harness, found := known[harnessID]
		if !found {
			uncataloged = append(uncataloged, harnessID)
			continue
		}
		version := stringValue(observation["version"])
		if version != "" && matchingRange(harness, version) == nil {
			unverified = append(unverified, harnessID+"@"+version)
		}
	}
	sort.Strings(uncataloged)
	sort.Strings(unverified)
	return uncataloged, unverified
}

func observedModelIDs(observation map[string]any) []string {
	models := make([]string, 0)
	for _, rawModel := range arrayValue(observation["models"]) {
		models = append(models, stringValue(rawModel.(map[string]any)["id"]))
	}
	sort.Strings(models)
	return models
}

func candidateRow(kind, harness string, version any, models []string, next string) map[string]any {
	return map[string]any{"kind": kind, "harness": harness, "version": version, "models": models, "next": next}
}

func staleDeckStatus(catalog Catalog, digest, receiptPath string, err error) map[string]any {
	return map[string]any{
		"state": "stale", "catalog": catalog.Catalog, "catalogDigest": digest,
		"lastKnownGood": receiptPath, "reason": err.Error(),
	}
}

func arrayValue(value any) []any {
	result, _ := value.([]any)
	return result
}
