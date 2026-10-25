package sdlc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestDeckReceiptRefusesSymlinkedManagedPaths(t *testing.T) {
	for _, layout := range []string{"parent", "leaf"} {
		t.Run(layout, func(t *testing.T) {
			target := t.TempDir()
			external := t.TempDir()
			externalReceipt := filepath.Join(external, "deck-sync.json")
			sentinel := []byte("unchanged external sentinel\n")
			if err := os.WriteFile(externalReceipt, sentinel, 0o644); err != nil {
				t.Fatal(err)
			}
			if layout == "parent" {
				if err := os.Symlink(external, filepath.Join(target, ".sdlc")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(target, ".sdlc"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(externalReceipt, filepath.Join(target, ".sdlc", "deck-sync.json")); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := AcceptDeckReceipt(testDeckReceipt(), target, Catalog{}); err == nil {
				t.Fatal("expected receipt write through managed symlink to fail")
			}
			status := DeckSyncStatus(target, Catalog{})
			if status["state"] != "stale" || !strings.Contains(status["reason"].(string), "symbolic link") {
				t.Fatalf("expected stale symlink status, got %#v", status)
			}
			if _, err := CatalogCandidates(target, Catalog{}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("expected candidates to reject managed symlink, got %v", err)
			}
			content, err := os.ReadFile(externalReceipt)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != string(sentinel) {
				t.Fatalf("external sentinel changed to %q", content)
			}
		})
	}
}

func TestDeckReceiptRejectsManagedPathSwapBeforeOpen(t *testing.T) {
	for _, layout := range []string{"parent", "leaf"} {
		t.Run(layout, func(t *testing.T) {
			target := t.TempDir()
			external := t.TempDir()
			managedDir := filepath.Join(target, ".sdlc")
			managedReceipt := filepath.Join(managedDir, "deck-sync.json")
			externalReceipt := filepath.Join(external, "deck-sync.json")
			if err := os.Mkdir(managedDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(managedReceipt, []byte(`{"source":"managed"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(externalReceipt, []byte(`{"source":"external"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			_, _, err := readDeckReceiptWithHook(target, func() {
				if layout == "parent" {
					if err := os.Rename(managedDir, managedDir+".original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, managedDir); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := os.Rename(managedReceipt, managedReceipt+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(externalReceipt, managedReceipt); err != nil {
					t.Fatal(err)
				}
			})
			if err == nil {
				t.Fatal("expected swapped managed symlink to be rejected")
			}
		})
	}
}

func TestDeckReceiptMissingIsNotExist(t *testing.T) {
	for _, parent := range []bool{false, true} {
		target := t.TempDir()
		if parent {
			if err := os.Mkdir(filepath.Join(target, ".sdlc"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := readDeckReceipt(target)
		if !os.IsNotExist(err) {
			t.Fatalf("expected missing receipt to satisfy os.IsNotExist, got %v", err)
		}
	}
}

func TestDeckReceiptRejectsFIFONonBlocking(t *testing.T) {
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(target, deckSyncReceiptRelative), 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := readDeckReceipt(target)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("expected FIFO rejection, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receipt read blocked on FIFO")
	}
}

func TestDeckReceiptManagedRoundTrip(t *testing.T) {
	target := t.TempDir()
	catalog := Catalog{Catalog: "noise/harnesses@0.5"}
	digest, err := Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	receipt := testDeckReceipt()
	receipt["catalogDigest"] = digest
	if _, err := AcceptDeckReceipt(receipt, target, catalog); err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptDeckReceipt(receipt, target, catalog); err != nil {
		t.Fatalf("expected atomic receipt replacement to succeed: %v", err)
	}
	status := DeckSyncStatus(target, catalog)
	if status["state"] != "in-sync" || status["lastKnownGood"] != filepath.Join(target, deckSyncReceiptRelative) {
		t.Fatalf("unexpected status after managed write: %#v", status)
	}
	candidates, err := CatalogCandidates(target, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rows := candidates["candidates"].([]map[string]any)
	if len(rows) != 1 || rows[0]["kind"] != "new-harness" || rows[0]["harness"] != "example" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}

func testDeckReceipt() map[string]any {
	return map[string]any{
		"schemaVersion": float64(1), "kind": "noise.deck.sdlc-receipt",
		"catalogDigest": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"deckVersion":   "1.0.0", "observedAt": "2026-08-14T00:00:00Z",
		"inventory": map[string]any{
			"schemaVersion": float64(1), "source": "deck-runtime", "catalog": "noise/harnesses@0.5",
			"harnesses": []any{map[string]any{
				"id": "example", "state": "installed", "rangeStatus": "unverified", "version": "1.2.3", "models": []any{},
			}},
		},
	}
}
