package sdlc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeckReceiptCannotAssertLocalCertification(t *testing.T) {
	receipt := map[string]any{
		"schemaVersion": float64(1), "kind": "noise.deck.sdlc-receipt",
		"catalogDigest": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"deckVersion":   "1.0.0", "observedAt": "2026-08-14T00:00:00Z",
		"inventory": map[string]any{
			"schemaVersion": float64(1), "source": "deck-runtime", "catalog": "noise/harnesses@0.4",
			"harnesses": []any{map[string]any{
				"id": "example", "state": "enumerated", "rangeStatus": "supported",
				"models": []any{map[string]any{
					"id": "model", "modelFamily": nil, "qualityTier": "unknown", "entitlement": "unknown",
					"reasoningLevels": []any{}, "provenance": "enumerated", "verified": false,
				}},
			}},
		},
	}
	if err := ValidateDeckReceipt(receipt); err == nil {
		t.Fatal("expected Deck compatibility assertion to fail")
	}
	harness := receipt["inventory"].(map[string]any)["harnesses"].([]any)[0].(map[string]any)
	harness["rangeStatus"] = "unverified"
	harness["models"].([]any)[0].(map[string]any)["verified"] = true
	if err := ValidateDeckReceipt(receipt); err == nil {
		t.Fatal("expected Deck model verification assertion to fail")
	}
	harness["models"].([]any)[0].(map[string]any)["verified"] = false
	if err := ValidateDeckReceipt(receipt); err != nil {
		t.Fatalf("observation-only receipt failed: %v", err)
	}
	harness["models"].([]any)[0].(map[string]any)["qualityTier"] = "balanced"
	if err := ValidateDeckReceipt(receipt); err == nil {
		t.Fatal("expected Deck model classification assertion to fail")
	}
}

func TestPullDeckReceiptRejectsNonLoopbackRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "http://example.com/deck-receipt", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	_, err := PullDeckReceipt(server.URL, t.TempDir(), Catalog{})
	if err == nil || !strings.Contains(err.Error(), "redirects must remain on loopback") {
		t.Fatalf("expected non-loopback redirect rejection, got %v", err)
	}
}

func TestPullDeckReceiptRejectsNonLoopbackEndpoint(t *testing.T) {
	_, err := PullDeckReceipt("http://example.com/deck-receipt", t.TempDir(), Catalog{})
	if err == nil || !strings.Contains(err.Error(), "loopback HTTP endpoints only") {
		t.Fatalf("expected non-loopback endpoint rejection, got %v", err)
	}
}

func TestDeckReceiptCannotIntroduceForeignUnknownFacts(t *testing.T) {
	receipt := map[string]any{
		"schemaVersion": float64(1), "kind": "noise.deck.sdlc-receipt",
		"catalogDigest": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"deckVersion":   "1.0.0", "observedAt": "2026-08-14T00:00:00Z",
		"inventory": map[string]any{
			"schemaVersion": float64(1), "source": "deck-runtime", "catalog": "noise/harnesses@0.4",
			"harnesses": []any{map[string]any{
				"id": "example", "state": "installed", "rangeStatus": "unverified",
				"unknown": []any{"ready", "spend-authority"},
			}},
		},
	}
	if err := ValidateDeckReceipt(receipt); err != nil {
		t.Fatalf("receipt with owned unknown labels failed: %v", err)
	}
	harness := receipt["inventory"].(map[string]any)["harnesses"].([]any)[0].(map[string]any)
	harness["unknown"] = []any{"ready", "quota"}
	if err := ValidateDeckReceipt(receipt); err == nil {
		t.Fatal("expected live quota label to fail")
	}
	harness["unknown"] = []any{"usage-limit"}
	if err := ValidateDeckReceipt(receipt); err == nil {
		t.Fatal("expected foreign unknown label to fail")
	}
}
