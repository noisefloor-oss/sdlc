package sdlc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Diagnostic struct {
	Code        string `json:"code"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
	Scope       string `json:"scope"`
	Remediation string `json:"remediation"`
}

type Diagnosis struct {
	SchemaVersion     int          `json:"schemaVersion"`
	SDLCVersion       string       `json:"sdlcVersion"`
	Kind              string       `json:"kind"`
	Integrity         string       `json:"integrity"`
	DeliveryReadiness string       `json:"deliveryReadiness"`
	RuntimeObservedAt string       `json:"runtimeObservedAt,omitempty"`
	Diagnostics       []Diagnostic `json:"diagnostics"`
}

type diagnosticProbe func(target string, argv []string) ([]byte, string)

const diagnosticLimit = 1 << 20

var errDiagnosticUnreadable = errors.New("diagnostic read could not establish file contents")

var tasksDiagnosticVersion = regexp.MustCompile(`^noise-tasks ([0-9]+\.[0-9]+\.[0-9]+)\s*$`)

// Diagnose observes public contracts. It never repairs, migrates, authenticates,
// runs a provider, or turns a compatible observation into delivery approval.
func Diagnose(repositoryRoot, target string) (Diagnosis, error) {
	return diagnose(repositoryRoot, target, runDiagnosticProbe)
}

func diagnose(repositoryRoot, target string, probe diagnosticProbe) (Diagnosis, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return Diagnosis{}, err
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Diagnosis{}, fmt.Errorf("diagnose requires an existing real project directory")
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return Diagnosis{}, fmt.Errorf("cannot open diagnostic target")
	}
	defer root.Close()
	read := func(relative string) ([]byte, error) { return readDiagnosticFile(root, target, relative) }
	result := Diagnosis{SchemaVersion: 1, SDLCVersion: Version, Kind: "noise.sdlc.diagnosis", Integrity: "not-adopted", DeliveryReadiness: "not-assessed", Diagnostics: []Diagnostic{}}
	add := func(code, status, owner, scope, remediation string) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{code, status, owner, scope, remediation})
	}

	// Preserve the existing verifier/repair vocabulary. The bounded reader keeps
	// diagnosis from opening arbitrary receipt references or unbounded files.
	hasAuthority := false
	for _, relative := range []string{"SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if _, err := root.Lstat(relative); !os.IsNotExist(err) {
			hasAuthority = true
		}
	}
	agents, agentsErr := read("AGENTS.md")
	if agentsErr == nil && bytes.Contains(agents, []byte(BlockBegin)) {
		hasAuthority = true
	}
	if hasAuthority {
		if _, err := verifyWithReader(target, read); err != nil {
			if errors.Is(err, errDiagnosticUnreadable) {
				result.Integrity = "unknown"
				add("integrity-unreadable", "unknown", "noise sdlc", "artifact-integrity", "Diagnosis could not read a managed file within its bounds. Inspect its size and accessibility, then use verify; unreadable does not mean corrupted.")
			} else {
				result.Integrity = "repair-required"
				add("integrity-"+repairReason(err.Error()), "action-required", "noise sdlc", "artifact-integrity", "Use the existing adoption repair plan; review generated changes before applying them.")
			}
		} else {
			result.Integrity = "verified"
		}
	}
	if agentsErr != nil && !os.IsNotExist(agentsErr) {
		add("instructions-unreadable", "unknown", "project", "instructions", "Inspect the AGENTS.md path and file size; diagnosis does not follow symbolic links.")
	}

	diagnoseTasks(target, probe, add)
	for _, relative := range []string{".tasks/tasks/tasks.json", ".noisetask/tasks.json", ".taskmaster/tasks/tasks.json"} {
		// Stat only: a retired projection is never task authority or input data.
		if err := validateManagedPath(target, relative); err != nil {
			add("tasks-state-unavailable", "unknown", "noise tasks", "setup", "Inspect retired task paths without following links; use Tasks-owned diagnosis and migration tools.")
			continue
		}
		if _, err := root.Lstat(relative); err == nil {
			add("tasks-legacy-projection", "action-required", "noise tasks", "setup-only", "An old task projection may obstruct setup even with a working SQLite store. Verify native Tasks authority, preserve needed history, then retire the projection through the supported Tasks cleanup workflow; do not remigrate a working store.")
		} else if !os.IsNotExist(err) {
			add("tasks-state-unavailable", "unknown", "noise tasks", "setup", "Use Tasks-owned diagnosis to inspect task setup paths.")
		}
	}
	if agentsErr == nil {
		for _, pointer := range []string{".tasks/tasks/tasks.json", ".noisetask/tasks.json", ".taskmaster/tasks/tasks.json"} {
			if bytes.Contains(agents, []byte(pointer)) {
				add("tasks-retired-pointer", "action-required", "noise sdlc", "instructions", "Replace the retired task-authority pointer through a reviewed adoption update so AGENTS.md and its receipt remain consistent; use noise tasks context --json for current authority.")
				break
			}
		}
	}
	retiredReview := false
	for _, relative := range []string{"AGENTS.md", "SDLC.md"} {
		content, err := read(relative)
		if err != nil {
			continue
		}
		for _, marker := range []string{"diff-guard[bot]", "diff-guard-personal[bot]", "cross-review-bot[bot]"} {
			if bytes.Contains(content, []byte(marker)) {
				retiredReview = true
			}
		}
	}
	if retiredReview {
		add("review-retired-identity", "action-required", "noise review", "review-instructions", "Reconcile active review instructions with the base-ref reviewer declaration and current Review contract through the reviewed adoption workflow. This static check does not inspect credentials or verify bot access.")
	} else {
		add("review-not-assessed", "not-assessed", "noise review", "review-contract", "No known retired identity was found in active instructions. Review owns configured identity, access, exact-head publication, and verdict evidence; none is certified by this report.")
	}
	diagnoseRuntime(repositoryRoot, read, &result, add)
	return result, nil
}

func readDiagnosticFile(root *os.Root, target, relative string) ([]byte, error) {
	if err := validateManagedPath(target, relative); err != nil {
		return nil, err
	}
	info, err := root.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > diagnosticLimit {
		return nil, errDiagnosticUnreadable
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errDiagnosticUnreadable, err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, diagnosticLimit+1))
	if err != nil || len(content) > diagnosticLimit {
		return nil, errDiagnosticUnreadable
	}
	return content, nil
}

func runDiagnosticProbe(target string, argv []string) ([]byte, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = target
	command.WaitDelay = 250 * time.Millisecond
	stdout := &boundedBuffer{remaining: diagnosticLimit}
	stderr := &boundedBuffer{remaining: diagnosticLimit}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, "timeout"
	}
	if stdout.overflowed || stderr.overflowed {
		return nil, "oversized"
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, "unavailable"
	}
	if err != nil {
		return nil, "failed"
	}
	return stdout.buffer.Bytes(), ""
}

func diagnoseTasks(target string, probe diagnosticProbe, add func(string, string, string, string, string)) {
	data, failure := probe(target, []string{"noise", "tasks", "version"})
	if failure != "" || len(data) > diagnosticLimit {
		add("tasks-unavailable", "unknown", "noise tasks", "tool-compatibility", "Ensure noise tasks is available and its public version command completes; diagnosis did not inspect configuration or credentials.")
		return
	}
	match := tasksDiagnosticVersion.FindSubmatch(data)
	if len(match) != 2 {
		add("tasks-version-unknown", "unknown", "noise tasks", "tool-compatibility", "The public Tasks version response was not recognized. Inspect noise tasks version and reconcile the diagnostic contract; no version incompatibility has been established.")
		return
	}
	if compareVersions(string(match[1]), "2.5.0") < 0 || compareVersions(string(match[1]), "3.0.0") >= 0 {
		add("tasks-version-unsupported", "action-required", "noise tasks", "tool-compatibility", "This diagnostic supports Tasks 2.5 or newer within major version 2 and context schema 4. Reconcile the installed Tasks contract using its supported upgrade path.")
		return
	}
	data, failure = probe(target, []string{"noise", "tasks", "context", "--json"})
	var context struct {
		SchemaVersion   int    `json:"schemaVersion"`
		Start           string `json:"start"`
		ProjectBoundary string `json:"projectBoundary"`
		StorePath       string `json:"storePath"`
		DocsPath        string `json:"docsPath"`
		PreferencesPath string `json:"preferencesPath"`
		Initialized     bool   `json:"initialized"`
		Writable        bool   `json:"writable"`
		Blocked         bool   `json:"blocked"`
		BlockedReason   string `json:"blockedReason"`
	}
	keys := []string{"schemaVersion", "start", "projectBoundary", "storePath", "docsPath", "preferencesPath", "initialized", "writable", "blocked", "blockedReason"}
	if failure != "" || !decodeDiagnosticJSON(data, &context, keys) || context.SchemaVersion != 4 ||
		filepath.Clean(context.Start) != target || !diagnosticBoundaryContains(context.ProjectBoundary, target) ||
		(context.Initialized && !filepath.IsAbs(context.StorePath)) {
		add("tasks-context-unsupported", "unknown", "noise tasks", "store-compatibility", "Ask noise tasks context --json for the authoritative context; reconcile its public schema with this diagnostic. Do not read or migrate a task store through SDLC.")
		return
	}
	if context.Blocked {
		add("tasks-authority-unreadable", "action-required", "noise tasks", "store-compatibility", "Tasks reports blocked authority. Use Tasks-owned diagnostics and supported recovery; SDLC will not fall back to a legacy projection.")
		return
	}
	if !context.Initialized {
		add("tasks-not-initialized", "not-observed", "noise tasks", "store-compatibility", "Initialize task authority through noise tasks when the project needs a backlog.")
		return
	}
	data, failure = probe(target, []string{"noise", "tasks", "verify", "--json"})
	var verification struct {
		File     string            `json:"file"`
		Clean    bool              `json:"clean"`
		Findings []json.RawMessage `json:"findings"`
		Changes  []json.RawMessage `json:"changes"`
	}
	if failure != "" || !decodeDiagnosticJSON(data, &verification, []string{"file", "clean", "findings", "changes"}) || !verification.Clean || len(verification.Findings) != 0 || len(verification.Changes) != 0 || verification.File != context.StorePath {
		add("tasks-authority-unreadable", "action-required", "noise tasks", "store-compatibility", "Tasks could not verify the selected store as clean. Run noise tasks verify --json and use its supported repair or version reconciliation; preserve native authority and do not infer compatibility from context alone.")
		return
	}
	add("tasks-compatible", "compatible", "noise tasks", "observed-store-contract", "Tasks reports a readable clean store under the supported public contract. Task readiness, claims, write authority and completion remain owned by Tasks.")
}

func diagnosticBoundaryContains(boundary, target string) bool {
	if !filepath.IsAbs(boundary) {
		return false
	}
	relative, err := filepath.Rel(boundary, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func decodeDiagnosticJSON(data []byte, destination any, required []string) bool {
	if len(data) > diagnosticLimit {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return false
	}
	for _, key := range required {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination) == nil
}

func diagnoseRuntime(repositoryRoot string, read func(string) ([]byte, error), result *Diagnosis, add func(string, string, string, string, string)) {
	data, err := read(".sdlc/deck-sync.json")
	if os.IsNotExist(err) {
		add("runtime-not-observed", "not-observed", "Deck", "recorded-runtime", "No Deck receipt is recorded. Standalone operation remains available; Deck synchronization is a separate opt-in.")
		return
	}
	var receipt map[string]any
	if err != nil || json.Unmarshal(data, &receipt) != nil || ValidateDeckReceipt(receipt) != nil {
		add("runtime-receipt-invalid", "action-required", "Deck", "recorded-runtime", "Inspect the public Deck receipt and obtain a valid replacement through the opted-in synchronization workflow. Diagnosis does not contact Deck.")
		return
	}
	result.RuntimeObservedAt = stringValue(receipt["observedAt"])
	catalog, err := LoadCatalog(repositoryRoot)
	if err != nil {
		add("runtime-catalog-unavailable", "unknown", "noise sdlc", "recorded-runtime", "Run check-catalog and repair the catalog before assessing the recorded runtime.")
		return
	}
	digest, err := Digest(catalog)
	if err != nil {
		add("runtime-catalog-unavailable", "unknown", "noise sdlc", "recorded-runtime", "The local catalog digest could not be computed; repair the SDLC catalog before assessing Deck synchronization.")
		return
	}
	drift := stringValue(receipt["catalogDigest"]) != digest || stringValue(receipt["inventory"].(map[string]any)["catalog"]) != catalog.Catalog
	if drift {
		add("runtime-catalog-drift", "action-required", "Deck", "recorded-runtime", "The recorded Deck catalog differs from this SDLC catalog. Reconcile through the opted-in sync workflow before relying on its compatibility observations.")
	}
	uncataloged, unverified := compareDeckInventory(receipt, catalog)
	if len(uncataloged)+len(unverified) > 0 {
		add("runtime-version-unverified", "action-required", "noise sdlc", "recorded-runtime", "Recorded harnesses or versions fall outside the catalog. Use harness candidates and version-contract tests; presence does not establish compatibility.")
	} else if !drift {
		add("runtime-recorded-compatible", "compatible", "Deck", "recorded-runtime", "The receipt matches the catalog and has no observed out-of-range version. This is a dated receipt check, not a fresh runtime probe, capacity assessment or readiness claim.")
	}
}
