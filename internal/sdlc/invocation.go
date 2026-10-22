package sdlc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// InvocationInput names every selection and carries prompt bytes explicitly.
// PromptFile is a proposed runner-owned file, not an existing file to read.
type InvocationInput struct {
	Harness    string
	Version    string
	Action     string
	Model      string
	Reasoning  string
	Workspace  string
	Prompt     []byte
	PromptFile string
}

type InvocationPromptFile struct {
	Path    string `json:"path"`
	Bytes   []byte `json:"bytes"`
	Mode    uint32 `json:"mode"`
	Create  string `json:"create"`
	Cleanup string `json:"cleanup"`
}

// InvocationPlan describes data for an independently authorized runner. It
// neither inspects a workspace nor creates files, executes a shell, or launches
// a provider. Byte slices are owned copies; JSON represents them as base64.
type InvocationPlan struct {
	SchemaVersion      int                   `json:"schemaVersion"`
	Kind               string                `json:"kind"`
	Harness            string                `json:"harness"`
	Version            string                `json:"version"`
	Action             string                `json:"action"`
	Model              string                `json:"model"`
	Reasoning          string                `json:"reasoning"`
	Argv               []string              `json:"argv"`
	Cwd                string                `json:"cwd"`
	Stdin              []byte                `json:"stdin,omitempty"`
	PromptFile         *InvocationPromptFile `json:"promptFile,omitempty"`
	ExecutionAuthority string                `json:"executionAuthority"`
	RunnerBoundary     string                `json:"runnerBoundary"`
	TaskCompletion     string                `json:"taskCompletion"`
}

// PrepareInvocation validates a catalog selection and substitutes whole argv
// elements only. Catalog compatibility is not live runtime certification.
func PrepareInvocation(catalog Catalog, input InvocationInput) (InvocationPlan, error) {
	var empty InvocationPlan
	if !validInvocationVersion(input.Version) {
		return empty, errors.New("invocation requires an explicit semantic version")
	}
	var harness *Harness
	for index := range catalog.Harnesses {
		if catalog.Harnesses[index].ID == input.Harness {
			if harness != nil {
				return empty, errors.New("invocation harness is ambiguous")
			}
			harness = &catalog.Harnesses[index]
		}
	}
	if harness == nil {
		return empty, errors.New("invocation harness is unsupported")
	}
	var selected *VersionRange
	for index := range harness.Ranges {
		candidate := &harness.Ranges[index]
		if !validInvocationVersion(candidate.Minimum) || !validInvocationVersion(candidate.MaximumExclusive) {
			return empty, errors.New("invocation catalog has an invalid version range")
		}
		if compareVersions(candidate.Minimum, input.Version) <= 0 && compareVersions(input.Version, candidate.MaximumExclusive) < 0 {
			if selected != nil {
				return empty, errors.New("invocation version range is ambiguous")
			}
			selected = candidate
		}
	}
	if selected == nil || selected.Invocation == nil {
		return empty, errors.New("invocation version has no supported contract")
	}
	if err := ValidateInvocationContract(*harness, *selected); err != nil {
		return empty, errors.New("invocation contract is invalid")
	}
	action, err := invocationAction(*selected.Invocation, input.Action)
	if err != nil {
		return empty, err
	}
	var model *Model
	for index := range harness.Models {
		if harness.Models[index].ID == input.Model {
			if model != nil {
				return empty, errors.New("invocation model is ambiguous")
			}
			model = &harness.Models[index]
		}
	}
	if model == nil || input.Model == "" || strings.HasPrefix(input.Model, "-") || !validInvocationText(input.Model) {
		return empty, errors.New("invocation model is unsupported")
	}
	if !normalizedReasoningLevels[input.Reasoning] || !contains(model.ReasoningLevels, input.Reasoning) || !contains(selected.ReasoningLevels, input.Reasoning) {
		return empty, errors.New("invocation reasoning is unsupported")
	}
	if !cleanInvocationPath(input.Workspace) {
		return empty, errors.New("invocation workspace must be an absolute clean path")
	}
	if len(input.Prompt) == 0 || len(input.Prompt) > action.Prompt.MaxBytes || !utf8.Valid(input.Prompt) || bytes.IndexByte(input.Prompt, 0) >= 0 {
		return empty, errors.New("invocation prompt must be bounded nonempty UTF-8 text")
	}
	plan := InvocationPlan{
		SchemaVersion: 1, Kind: "noise.sdlc.invocation-plan", Harness: input.Harness,
		Version: input.Version, Action: input.Action, Model: input.Model, Reasoning: input.Reasoning,
		Argv: make([]string, len(action.Argv)), Cwd: input.Workspace,
		ExecutionAuthority: "not-granted", TaskCompletion: "not-established",
		RunnerBoundary: "required: independently authorize provider execution, spend, and publication; enforce filesystem, network, tool, and credential boundaries",
	}
	switch action.Prompt.Transport {
	case "stdin-text":
		if input.PromptFile != "" {
			return empty, errors.New("stdin invocation does not accept a prompt-file option")
		}
		plan.Stdin = bytes.Clone(input.Prompt)
	case "at-file":
		if !cleanInvocationPath(input.PromptFile) {
			return empty, errors.New("invocation prompt file must be an absolute clean path")
		}
		relative, err := filepath.Rel(input.Workspace, input.PromptFile)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return empty, errors.New("invocation prompt file must be inside the declared workspace")
		}
		plan.PromptFile = &InvocationPromptFile{
			Path: input.PromptFile, Bytes: bytes.Clone(input.Prompt), Mode: 0600,
			Create:  "runner-owned-exclusive-create-without-symlinks",
			Cleanup: "runner-must-remove-owned-file-on-all-exits-and-reconcile-after-interruption",
		}
	default:
		return empty, errors.New("invocation prompt transport is unsupported")
	}
	for index, argument := range action.Argv {
		switch argument {
		case "{model}":
			plan.Argv[index] = input.Model
		case "{reasoning}":
			plan.Argv[index] = input.Reasoning
		case "{workspace}":
			plan.Argv[index] = input.Workspace
		case "@{promptFile}":
			if plan.PromptFile == nil {
				return empty, errors.New("invocation prompt-file placeholder requires at-file transport")
			}
			plan.Argv[index] = "@" + input.PromptFile
		default:
			if strings.ContainsAny(argument, "{}") || !validInvocationText(argument) {
				return empty, errors.New("invocation argv contains an unsupported placeholder or argument")
			}
			plan.Argv[index] = argument
		}
	}
	return plan, nil
}

func validInvocationVersion(version string) bool {
	parts := semanticVersionPattern.FindStringSubmatch(version)
	if parts == nil {
		return false
	}
	for _, part := range parts[1:] {
		if len(part) > 1 && part[0] == '0' {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func validInvocationText(text string) bool {
	return utf8.ValidString(text) && strings.IndexFunc(text, unicode.IsControl) < 0
}

func cleanInvocationPath(path string) bool {
	return len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && validInvocationText(path)
}

func invocationAction(contract InvocationContract, action string) (*InvocationAction, error) {
	if contract.SchemaVersion != 1 {
		return nil, errors.New("invocation contract version is unsupported")
	}
	var selected *InvocationAction
	switch action {
	case "execution":
		selected = contract.Actions.Execution
	case "review":
		selected = contract.Actions.Review
	default:
		return nil, errors.New("invocation action is unsupported")
	}
	if selected == nil {
		return nil, errors.New("invocation action has no contract")
	}
	return selected, nil
}

// InvocationAudit is an unverified publication pointer. The actor's own output
// cannot prove identity, candidate coverage, approval, or a successful post.
type InvocationAudit struct {
	ID         uint64 `json:"id"`
	State      string `json:"state"`
	Author     string `json:"author"`
	AuthorType string `json:"author_type"`
}

type InvocationOutput struct {
	SchemaVersion  int              `json:"schemaVersion"`
	Kind           string           `json:"kind"`
	Action         string           `json:"action"`
	ExitCode       int              `json:"exitCode"`
	ProcessOutcome string           `json:"processOutcome"`
	Normalization  string           `json:"normalization"`
	FinalText      string           `json:"finalText,omitempty"`
	Audit          *InvocationAudit `json:"audit,omitempty"`
	Publication    string           `json:"publication"`
	ReadbackOwner  string           `json:"readbackOwner"`
	AutomaticRetry bool             `json:"automaticRetry"`
	TaskCompletion string           `json:"taskCompletion"`
	ApprovalProof  string           `json:"approvalProof"`
	FailureCodes   []string         `json:"failureCodes"`
}

// NormalizeInvocationOutput accepts only the declared final-text channel, not
// stderr or a stream of provider events. Invalid process/output outcomes are
// returned as data so uncertainty and the no-retry boundary are not discarded.
// An error means that no supported normalization contract could be selected;
// the returned outcome still preserves publication uncertainty.
func NormalizeInvocationOutput(contract InvocationContract, actionName string, exitCode int, stdout []byte) (InvocationOutput, error) {
	result := InvocationOutput{
		SchemaVersion: 1, Kind: "noise.sdlc.invocation-output", Action: actionName,
		ExitCode: exitCode, ProcessOutcome: "succeeded", Normalization: "invalid",
		Publication: "uncertain", ReadbackOwner: "not-assessed", TaskCompletion: "not-established",
		ApprovalProof: "not-established", FailureCodes: []string{},
	}
	if actionName == "execution" {
		result.Publication, result.ReadbackOwner = "not-applicable", "not-applicable"
	} else if actionName == "review" {
		result.ReadbackOwner = "noise-review"
	}
	if exitCode != 0 {
		result.ProcessOutcome = "failed"
		result.FailureCodes = append(result.FailureCodes, "process-failed")
	}
	action, err := invocationAction(contract, actionName)
	if err != nil {
		result.FailureCodes = append(result.FailureCodes, "unsupported-output-contract")
		return result, err
	}
	if action.Output.Normalizer != "final-text-v1" || action.Output.MaxBytes < 1 || action.Output.MaxBytes > MaxInvocationOutputBytes || action.Completion.SuccessExitCode != 0 || !action.Completion.RequiresFinalOutput || action.Completion.TaskCompletion != "not-established" {
		result.FailureCodes = append(result.FailureCodes, "unsupported-output-contract")
		return result, errors.New("invocation output contract is unsupported")
	}
	if actionName == "review" && (action.Publication == nil || action.Publication.Contract != "noise-review-v1" || action.Publication.Actor != "reviewer" || !action.Publication.RequiresReadback || action.Publication.AuditRecord != "last-nonempty-line-json") {
		result.FailureCodes = append(result.FailureCodes, "unsupported-publication-contract")
		return result, errors.New("invocation review publication contract is unsupported")
	}
	result.Normalization = "valid"
	if len(stdout) > action.Output.MaxBytes || !utf8.Valid(stdout) || bytes.IndexByte(stdout, 0) >= 0 {
		result.Normalization = "invalid"
		result.FailureCodes = append(result.FailureCodes, "invalid-final-text")
		return result, nil
	}
	if len(bytes.TrimSpace(stdout)) == 0 {
		result.Normalization = "invalid"
		result.FailureCodes = append(result.FailureCodes, "missing-final-text")
		return result, nil
	}
	result.FinalText = string(stdout)
	if actionName == "review" {
		lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
		audit, err := parseInvocationAudit(bytes.TrimSpace(lines[len(lines)-1]))
		if err != nil {
			result.Normalization = "invalid"
			result.FailureCodes = append(result.FailureCodes, "missing-or-malformed-review-audit")
			return result, nil
		}
		result.Audit = audit
		if result.ProcessOutcome == "succeeded" {
			result.Publication = "unverified-claim"
		}
	}
	return result, nil
}

func parseInvocationAudit(line []byte) (*InvocationAudit, error) {
	// Decode top-level keys explicitly to reject duplicate pointer fields rather
	// than letting a JSON consumer choose a different id or state.
	decoder := json.NewDecoder(bytes.NewReader(line))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("review audit must be an object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("review audit key is invalid")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("review audit has duplicate keys")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("review audit value is invalid")
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, errors.New("review audit is incomplete")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("review audit has trailing data")
	}
	var audit InvocationAudit
	if json.Unmarshal(fields["id"], &audit.ID) != nil || audit.ID == 0 || json.Unmarshal(fields["state"], &audit.State) != nil || json.Unmarshal(fields["author"], &audit.Author) != nil || json.Unmarshal(fields["author_type"], &audit.AuthorType) != nil {
		return nil, errors.New("review audit fields are invalid")
	}
	if !contains([]string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED"}, audit.State) || audit.Author == "" || strings.TrimSpace(audit.Author) != audit.Author || len(audit.Author) > 128 || !validInvocationText(audit.Author) || audit.AuthorType != "Bot" {
		return nil, errors.New("review audit fields are unsupported")
	}
	return &audit, nil
}
