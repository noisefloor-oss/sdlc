package sdlc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evidenceFixture(t *testing.T) (string, string, map[string]any) {
	t.Helper()
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := LoadFlexibleJSON(filepath.Join(root, "evidence/sdlc-self.json"), &record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.json")
	writeSchemaFixture(t, path, record)
	return root, path, record
}

func evidenceOperations(record map[string]any) map[string]any {
	return record["operations"].(map[string]any)
}
func evidenceGroup(record map[string]any, key string) map[string]any {
	return evidenceOperations(record)[key].(map[string]any)
}

func citeEvidence(record map[string]any, group string) {
	operations := evidenceOperations(record)
	operations["sources"] = []any{map[string]any{"id": "fixture", "kind": "artifact", "repositoryPath": "evidence/sdlc-self.json", "revision": strings.Repeat("a", 40)}}
	evidenceGroup(record, group)["sourceRefs"] = []any{"fixture"}
}

func TestEvidenceUnknownBaselineDoesNotClaimAcceptance(t *testing.T) {
	root, path, _ := evidenceFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CheckEvidence(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if result["validation"] != "valid" || result["observationTruth"] != "not-assessed" || result["milestoneAcceptance"] != "not-assessed" || result["humanLaborEstimate"] != "not-computed" || result["rates"] != "not-computed" {
		t.Fatalf("validation claimed observation truth, rates or acceptance: %#v", result)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("validation changed the evidence file")
	}
}

func TestEvidenceRejectsUnsupportedObservations(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing-measure", func(r map[string]any) { delete(evidenceGroup(r, "timing"), "queueWaitMilliseconds") }},
		{"unmeasured-labor-with-method", func(r map[string]any) { evidenceGroup(r, "timing")["humanLaborMethod"] = "direct-observation" }},
		{"lowercase-reverse-window", func(r map[string]any) {
			w := evidenceGroup(r, "window")
			w["start"] = "2026-09-06t01:00:00z"
			w["end"] = "2026-09-06t00:00:00z"
		}},
		{"lowercase-date-without-source", func(r map[string]any) { evidenceGroup(r, "cleanup")["observedAt"] = "2026-09-06t00:00:00z" }},
		{"leap-second-without-source", func(r map[string]any) { evidenceGroup(r, "cleanup")["observedAt"] = "2016-12-31T23:59:60Z" }},
		{"negative-count", func(r map[string]any) { evidenceGroup(r, "denominator")["count"] = -1 }},
		{"fractional-count", func(r map[string]any) { evidenceGroup(r, "denominator")["count"] = 1.5 }},
		{"unknown-schema", func(r map[string]any) { r["schemaVersion"] = 99 }},
		{"extra-property", func(r map[string]any) { evidenceOperations(r)["extra"] = true }},
		{"numeric-without-source", func(r map[string]any) { evidenceGroup(r, "denominator")["count"] = 0 }},
		{"dangling-reference", func(r map[string]any) { evidenceGroup(r, "cleanup")["sourceRefs"] = []any{"missing"} }},
		{"duplicate-source-id", func(r map[string]any) {
			citeEvidence(r, "denominator")
			s := evidenceOperations(r)["sources"].([]any)
			evidenceOperations(r)["sources"] = append(s, map[string]any{"id": "fixture", "kind": "owner-evidence", "repositoryPath": "docs/incubation-scorecard.md", "revision": strings.Repeat("b", 40)})
		}},
		{"unsafe-source-path", func(r map[string]any) {
			citeEvidence(r, "denominator")
			evidenceOperations(r)["sources"].([]any)[0].(map[string]any)["repositoryPath"] = "evidence/../private.json"
		}},
		{"reverse-window", func(r map[string]any) {
			w := evidenceGroup(r, "window")
			w["start"] = "2026-09-06T01:00:00Z"
			w["end"] = "2026-09-06T00:00:00Z"
		}},
		{"missing-limitation", func(r map[string]any) { evidenceOperations(r)["limitations"] = []any{} }},
		{"partial-time-exceeds-total", func(r map[string]any) {
			citeEvidence(r, "timing")
			v := evidenceGroup(r, "timing")
			v["elapsedMilliseconds"] = 10
			v["executionMilliseconds"] = 11
		}},
		{"complete-time-mismatch", func(r map[string]any) {
			citeEvidence(r, "timing")
			v := evidenceGroup(r, "timing")
			v["elapsedMilliseconds"] = 10
			for _, key := range []string{"queueWaitMilliseconds", "executionMilliseconds", "humanDecisionWaitMilliseconds", "unattributedMilliseconds"} {
				v[key] = 1
			}
		}},
		{"inferred-human-labor", func(r map[string]any) {
			citeEvidence(r, "timing")
			evidenceGroup(r, "timing")["humanLaborMilliseconds"] = 10
		}},
		{"operator-report-without-source-kind", func(r map[string]any) {
			citeEvidence(r, "timing")
			v := evidenceGroup(r, "timing")
			v["humanLaborMilliseconds"] = 10
			v["humanLaborMethod"] = "operator-report"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, path, record := evidenceFixture(t)
			test.change(record)
			writeSchemaFixture(t, path, record)
			if _, err := CheckEvidence(root, path); err == nil {
				t.Fatal("invalid evidence was accepted")
			}
		})
	}
}

func TestEvidenceTimestampOrdering(t *testing.T) {
	for _, pair := range [][2]string{
		{"2016-12-31T23:59:59.999Z", "2016-12-31T23:59:60Z"},
		{"2016-12-31T23:59:60.999Z", "2017-01-01T00:00:00Z"},
		{"2016-12-31T23:59:60.0000000001Z", "2016-12-31T23:59:60.0000000002Z"},
		{"2026-09-06t00:00:00.0000000001z", "2026-09-06T00:00:00.0000000002Z"},
		{"2017-01-01T00:59:60+01:00", "2017-01-01T00:00:00Z"},
	} {
		t.Run(pair[0], func(t *testing.T) {
			root, path, record := evidenceFixture(t)
			window := evidenceGroup(record, "window")
			window["start"], window["end"] = pair[0], pair[1]
			writeSchemaFixture(t, path, record)
			if _, err := CheckEvidence(root, path); err != nil {
				t.Fatalf("forward window rejected: %v", err)
			}
			window["start"], window["end"] = pair[1], pair[0]
			writeSchemaFixture(t, path, record)
			if _, err := CheckEvidence(root, path); err == nil || !strings.Contains(err.Error(), "ends before") {
				t.Fatalf("reverse window not rejected by ordering check: %v", err)
			}
		})
	}
}

func TestEvidenceObservedZeroAndConsistentTimingRemainValid(t *testing.T) {
	root, path, record := evidenceFixture(t)
	citeEvidence(record, "denominator")
	evidenceGroup(record, "denominator")["count"] = 0
	citeEvidence(record, "timing")
	timing := evidenceGroup(record, "timing")
	timing["elapsedMilliseconds"] = 10
	for key, value := range map[string]int{"queueWaitMilliseconds": 2, "executionMilliseconds": 3, "humanDecisionWaitMilliseconds": 4, "unattributedMilliseconds": 1, "humanLaborMilliseconds": 0} {
		timing[key] = value
	}
	timing["humanLaborMethod"] = "direct-observation"
	writeSchemaFixture(t, path, record)
	if _, err := CheckEvidence(root, path); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceLegacySchemaRetainsHistoricalMismatch(t *testing.T) {
	root, path, record := evidenceFixture(t)
	record["schemaVersion"] = 1
	delete(record, "operations")
	writeSchemaFixture(t, path, record)
	if _, err := CheckEvidence(root, path); err == nil {
		t.Fatal("historical v1 not-tested mismatch silently accepted")
	}
	record["update"].(map[string]any)["rollback"] = "unknown"
	writeSchemaFixture(t, path, record)
	result, err := CheckEvidence(root, path)
	if err != nil || result["recordSchemaVersion"] != 1 {
		t.Fatalf("valid historical v1 rejected: %v", err)
	}
}

func TestEvidenceRejectsTrailingJSONAndSymlinks(t *testing.T) {
	root, path, record := evidenceFixture(t)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n{}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckEvidence(root, path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	link := filepath.Join(t.TempDir(), "linked.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckEvidence(root, link); err == nil {
		t.Fatal("symlink accepted")
	}
}
