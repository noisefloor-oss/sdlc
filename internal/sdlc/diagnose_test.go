package sdlc

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiagnoseKeepsIntegritySeparateFromDeliveryReadiness(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\nUse noise review for formal review.\n")
	var calls []string
	probe := diagnosisHealthyProbe(t, target, &calls)
	before := diagnosisSnapshot(t, target)
	result, err := diagnose(root, target, probe)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Kind != "noise.sdlc.diagnosis" || result.Integrity != "verified" || result.DeliveryReadiness != "not-assessed" {
		t.Fatalf("unexpected report identity or scope: %+v", result)
	}
	for _, code := range []string{"tasks-compatible", "review-not-assessed", "runtime-not-observed"} {
		diagnosisRequireCode(t, result, code)
	}
	wantCalls := []string{"noise tasks version", "noise tasks context --json", "noise tasks verify --json"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("diagnosis escaped its fixed read-only probe contract: %v", calls)
	}
	if after := diagnosisSnapshot(t, target); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnosis changed target files, modes, or directory entries")
	}
}

func TestDiagnoseIndependentFailuresDoNotInvalidateIntactAdoption(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\nUse .tasks/tasks/tasks.json for task state.\nFormal review uses diff-guard[bot].\n")
	receipt := diagnosisReceipt(t, root)
	receipt["catalogDigest"] = "sha256:" + strings.Repeat("0", 64)
	diagnosisWriteJSON(t, filepath.Join(target, ".sdlc", "deck-sync.json"), receipt)
	const untrusted = "UNTRUSTED_DIAGNOSTIC_OUTPUT_MUST_NOT_ESCAPE"
	baseProbe := diagnosisHealthyProbe(t, target, nil)
	probe := func(directory string, argv []string) ([]byte, string) {
		if strings.Join(argv, " ") == "noise tasks verify --json" {
			return []byte(untrusted), "nonzero"
		}
		return baseProbe(directory, argv)
	}
	before := diagnosisSnapshot(t, target)
	result, err := diagnose(root, target, probe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Integrity != "verified" || result.DeliveryReadiness != "not-assessed" {
		t.Fatalf("specialist failures changed artifact integrity or claimed readiness: %+v", result)
	}
	for _, code := range []string{"tasks-authority-unreadable", "tasks-retired-pointer", "review-retired-identity", "runtime-catalog-drift"} {
		diagnosisRequireCode(t, result, code)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), untrusted) {
		t.Fatalf("report leaked provider output: %s, %v", encoded, err)
	}
	if _, err := Verify(target); err != nil {
		t.Fatalf("diagnosis damaged the adoption: %v", err)
	}
	if after := diagnosisSnapshot(t, target); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnosis modified a target with compatibility findings")
	}
}

func TestDiagnoseTasksContractFailures(t *testing.T) {
	cases := []struct {
		name          string
		command       string
		output        string
		failure       string
		contextChange func(map[string]any)
		want          string
	}{
		{name: "missing executable", command: "noise tasks version", failure: "not-found", want: "tasks-unavailable"},
		{name: "old version", command: "noise tasks version", output: "noise-tasks 1.4.0\n", want: "tasks-version-unsupported"},
		{name: "new major", command: "noise tasks version", output: "noise-tasks 3.0.0\n", want: "tasks-version-unsupported"},
		{name: "unsupported context schema", contextChange: func(v map[string]any) { v["schemaVersion"] = 3 }, want: "tasks-context-unsupported"},
		{name: "context missing required field", contextChange: func(v map[string]any) { delete(v, "writable") }, want: "tasks-context-unsupported"},
		{name: "context unknown field", contextChange: func(v map[string]any) { v["unexpected"] = true }, want: "tasks-context-unsupported"},
		{name: "context for another project", contextChange: func(v map[string]any) {
			v["projectBoundary"] = filepath.Join(filepath.Dir(v["projectBoundary"].(string)), "different-project")
		}, want: "tasks-context-unsupported"},
		{name: "context started elsewhere", contextChange: func(v map[string]any) {
			v["start"] = filepath.Join(filepath.Dir(v["start"].(string)), "different-start")
		}, want: "tasks-context-unsupported"},
		{name: "malformed context", command: "noise tasks context --json", output: "untrusted non-JSON output", want: "tasks-context-unsupported"},
		{name: "uninitialized", contextChange: func(v map[string]any) { v["initialized"] = false }, want: "tasks-not-initialized"},
		{name: "unreadable store", command: "noise tasks verify --json", output: "untrusted store error", failure: "nonzero", want: "tasks-authority-unreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, target := diagnosisFixture(t, "# Project\n")
			baseProbe := diagnosisHealthyProbe(t, target, nil)
			probe := func(directory string, argv []string) ([]byte, string) {
				command := strings.Join(argv, " ")
				if command == tc.command {
					return []byte(tc.output), tc.failure
				}
				if command == "noise tasks context --json" && tc.contextChange != nil {
					context := diagnosisContext(target)
					tc.contextChange(context)
					return diagnosisJSON(t, context), ""
				}
				return baseProbe(directory, argv)
			}
			result, err := diagnose(root, target, probe)
			if err != nil {
				t.Fatal(err)
			}
			diagnosisRequireCode(t, result, tc.want)
			if diagnosisHasCode(result, "tasks-compatible") || result.Integrity != "verified" || result.DeliveryReadiness != "not-assessed" {
				t.Fatalf("failed specialist contract was promoted to success: %+v", result)
			}
			encoded := diagnosisJSON(t, result)
			if strings.Contains(string(encoded), "untrusted") {
				t.Fatalf("raw diagnostic output escaped: %s", encoded)
			}
		})
	}
}

func TestDiagnoseRetiredProjectionNeverBecomesTaskAuthority(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\n")
	projection := filepath.Join(target, ".tasks", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(projection), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projection, []byte("not JSON; RETIRED_TASK_BODY_MUST_NOT_ESCAPE\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	before := diagnosisSnapshot(t, target)
	result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	diagnosisRequireCode(t, result, "tasks-legacy-projection")
	diagnosisRequireCode(t, result, "tasks-compatible")
	if strings.Contains(string(diagnosisJSON(t, result)), "RETIRED_TASK_BODY") {
		t.Fatal("diagnosis exposed retired projection content")
	}
	if after := diagnosisSnapshot(t, target); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnosis migrated, removed, or modified the inert projection")
	}
}

func TestDiagnoseDoesNotOpenRetiredProjectionFIFO(t *testing.T) {
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("FIFO read discriminator requires mkfifo")
	}
	root, target := diagnosisFixture(t, "# Project\n")
	projection := filepath.Join(target, ".tasks", "tasks", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(projection), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(mkfifo, projection).CombinedOutput(); err != nil {
		t.Fatalf("create FIFO fixture: %v: %s", err, output)
	}
	type outcome struct {
		report Diagnosis
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		report, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
		finished <- outcome{report, err}
	}()
	select {
	case got := <-finished:
		if got.err != nil {
			t.Fatal(got.err)
		}
		diagnosisRequireCode(t, got.report, "tasks-legacy-projection")
		diagnosisRequireCode(t, got.report, "tasks-compatible")
	case <-time.After(2 * time.Second):
		// Release an accidental reader so a failing implementation does not
		// leave a blocked test goroutine behind after reporting the failure.
		writer, err := os.OpenFile(projection, os.O_RDWR, 0)
		if err == nil {
			_ = writer.Close()
		}
		t.Fatal("diagnosis blocked opening the retired projection instead of inspecting metadata only")
	}
}

func TestDiagnoseRecognizesBothRetiredReviewIdentities(t *testing.T) {
	for _, identity := range []string{"diff-guard[bot]", "diff-guard-personal[bot]"} {
		t.Run(identity, func(t *testing.T) {
			root, target := diagnosisFixture(t, "# Project\nReview with "+identity+".\n")
			result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
			if err != nil {
				t.Fatal(err)
			}
			diagnosisRequireCode(t, result, "review-retired-identity")
			if result.Integrity != "verified" {
				t.Fatalf("stale review instructions invalidated intact managed hashes: %+v", result)
			}
		})
	}
}

func TestDiagnoseRuntimeObservationsRemainSeparateFromCertification(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		want   string
	}{
		{name: "matching recorded inventory", want: "runtime-recorded-compatible"},
		{name: "catalog drift", change: func(r map[string]any) { r["catalogDigest"] = "sha256:" + strings.Repeat("0", 64) }, want: "runtime-catalog-drift"},
		{name: "unsupported version", change: func(r map[string]any) {
			inventory := r["inventory"].(map[string]any)
			inventory["harnesses"] = []any{map[string]any{
				"id": "codex", "state": "installed", "version": "999.0.0", "rangeStatus": "unverified",
				"models": []any{}, "unknown": []any{"ready"}, "probeErrors": []any{},
			}}
		}, want: "runtime-version-unverified"},
		{name: "invalid receipt", change: func(r map[string]any) { r["schemaVersion"] = 99 }, want: "runtime-receipt-invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, target := diagnosisFixture(t, "# Project\n")
			receipt := diagnosisReceipt(t, root)
			if tc.change != nil {
				tc.change(receipt)
			}
			diagnosisWriteJSON(t, filepath.Join(target, ".sdlc", "deck-sync.json"), receipt)
			result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
			if err != nil {
				t.Fatal(err)
			}
			diagnosisRequireCode(t, result, tc.want)
			if result.Integrity != "verified" || result.DeliveryReadiness != "not-assessed" {
				t.Fatalf("runtime observation changed unrelated assurance: %+v", result)
			}
		})
	}
}

func TestDiagnoseIgnoresRetiredNamesOutsideActiveInstructions(t *testing.T) {
	root, target := diagnosisFixture(t, "# Project\n")
	if err := os.Mkdir(filepath.Join(target, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "docs", "history.md"), []byte("Retired diff-guard[bot] and .tasks/tasks/tasks.json were investigated."), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	if diagnosisHasCode(result, "review-retired-identity") || diagnosisHasCode(result, "tasks-retired-pointer") {
		t.Fatalf("historical documentation was mistaken for active policy: %+v", result)
	}
}

func TestDiagnoseRefusesOutsideManagedSymlinks(t *testing.T) {
	for _, relative := range []string{"AGENTS.md", ".sdlc/deck-sync.json"} {
		t.Run(relative, func(t *testing.T) {
			root, target := diagnosisFixture(t, "# Project\n")
			outside := filepath.Join(t.TempDir(), "outside")
			const sentinel = "OUTSIDE_DOCUMENT_MUST_NOT_ESCAPE"
			if err := os.WriteFile(outside, []byte(sentinel), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(target, relative)
			if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			result, err := diagnose(root, target, diagnosisHealthyProbe(t, target, nil))
			if err == nil && relative == "AGENTS.md" && result.Integrity != "repair-required" {
				t.Fatalf("external instruction link was not refused: %+v", result)
			}
			if err == nil && relative == ".sdlc/deck-sync.json" && !diagnosisHasCode(result, "runtime-receipt-invalid") {
				t.Fatalf("external runtime receipt link was not refused: %+v", result)
			}
			serialized := string(diagnosisJSON(t, result))
			if err != nil {
				serialized += err.Error()
			}
			if strings.Contains(serialized, sentinel) {
				t.Fatal("outside file contents escaped through diagnostics")
			}
			content, readErr := os.ReadFile(outside)
			if readErr != nil || string(content) != sentinel {
				t.Fatalf("outside document changed: %v", readErr)
			}
		})
	}
}

func TestDiagnoseOrdinaryUnadoptedProject(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	base := diagnosisHealthyProbe(t, target, nil)
	probe := func(directory string, argv []string) ([]byte, string) {
		if strings.Join(argv, " ") == "noise tasks context --json" {
			context := diagnosisContext(target)
			context["initialized"] = false
			return diagnosisJSON(t, context), ""
		}
		return base(directory, argv)
	}
	before := diagnosisSnapshot(t, target)
	result, err := diagnose(root, target, probe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Integrity != "not-adopted" || result.DeliveryReadiness != "not-assessed" {
		t.Fatalf("ordinary new project was treated as damaged or delivery-ready: %+v", result)
	}
	diagnosisRequireCode(t, result, "tasks-not-initialized")
	if after := diagnosisSnapshot(t, target); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnosis initialized an ordinary unadopted project")
	}
}

func TestDiagnoseAcceptsEnclosingTasksBoundary(t *testing.T) {
	root, target := diagnosisFixture(t, "# Nested project\n")
	base := diagnosisHealthyProbe(t, target, nil)
	probe := func(directory string, argv []string) ([]byte, string) {
		if strings.Join(argv, " ") == "noise tasks context --json" {
			context := diagnosisContext(target)
			context["projectBoundary"] = filepath.Dir(target)
			return diagnosisJSON(t, context), ""
		}
		return base(directory, argv)
	}
	result, err := diagnose(root, target, probe)
	if err != nil {
		t.Fatal(err)
	}
	diagnosisRequireCode(t, result, "tasks-compatible")
}

func TestDiagnoseRejectsMissingTargetBeforeProbing(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Diagnose(root, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("public diagnostic entrypoint accepted a nonexistent project")
	}
}

func diagnosisFixture(t *testing.T, instructions string) (string, string) {
	t.Helper()
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), []byte(instructions), 0o644); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{SchemaVersion: 1, Source: "test", Catalog: "noise/harnesses@0.4", Harnesses: []HarnessObservation{}}
	built, err := BuildPlan(target, root, testAnswers(), &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := Adopt(target, built.Outputs); err != nil {
		t.Fatal(err)
	}
	return root, target
}

func diagnosisContext(target string) map[string]any {
	return map[string]any{
		"schemaVersion": 4, "start": target, "projectBoundary": target,
		"storePath": filepath.Join(target, ".git", "noise-tasks", "tasks.sqlite"),
		"docsPath":  ".tasks/docs/PRD.md", "preferencesPath": ".tasks/preferences.json",
		"initialized": true, "writable": true, "blocked": false, "blockedReason": "",
	}
}

func diagnosisHealthyProbe(t *testing.T, target string, calls *[]string) diagnosticProbe {
	t.Helper()
	return func(directory string, argv []string) ([]byte, string) {
		t.Helper()
		if directory != target {
			t.Fatalf("probe ran outside the requested project: %q", directory)
		}
		command := strings.Join(argv, " ")
		if calls != nil {
			*calls = append(*calls, command)
		}
		switch command {
		case "noise tasks version":
			return []byte("noise-tasks 2.5.0\n"), ""
		case "noise tasks context --json":
			return diagnosisJSON(t, diagnosisContext(target)), ""
		case "noise tasks verify --json":
			return diagnosisJSON(t, map[string]any{"file": filepath.Join(target, ".git", "noise-tasks", "tasks.sqlite"), "clean": true, "findings": []any{}, "changes": []any{}}), ""
		default:
			t.Fatalf("unexpected command instead of a fixed read-only probe: %v", argv)
			return nil, "unexpected"
		}
	}
}

func diagnosisReceipt(t *testing.T, root string) map[string]any {
	t.Helper()
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"schemaVersion": 1, "kind": "noise.deck.sdlc-receipt", "catalogDigest": digest,
		"deckVersion": "1.0.0", "observedAt": "2026-09-06T00:00:00Z",
		"inventory": map[string]any{"schemaVersion": 1, "source": "deck-runtime", "catalog": catalog.Catalog, "harnesses": []any{}},
	}
}

func diagnosisWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.WriteFile(path, diagnosisJSON(t, value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diagnosisJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func diagnosisHasCode(result Diagnosis, code string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func diagnosisRequireCode(t *testing.T, result Diagnosis, code string) {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			if diagnostic.Status == "" || diagnostic.Owner == "" || diagnostic.Scope == "" || diagnostic.Remediation == "" {
				t.Fatalf("diagnostic lacks actionable ownership or scope: %+v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("missing diagnostic %q: %+v", code, result.Diagnostics)
}

func diagnosisSnapshot(t *testing.T, target string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String() + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		if info.Mode().IsRegular() {
			// The zero-mode projection is deliberately unreadable to non-root
			// test runners; size and mode still detect accidental cleanup.
			if info.Mode().Perm() == 0 {
				value += ":unreadable"
			} else {
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				value += ":" + string(body)
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + link
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
