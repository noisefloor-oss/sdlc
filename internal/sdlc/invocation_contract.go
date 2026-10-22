package sdlc

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxInvocationPromptBytes = 4 * 1024 * 1024
	MaxInvocationOutputBytes = 16 * 1024 * 1024
)

// Version 1 admits only the two bounded argv grammars observed in public CLI
// help. Enabling another grammar requires its own contract and fixture review.
var invocationV1Argv = map[string][]string{
	"claude": {"claude", "--print", "--input-format", "text", "--output-format", "text", "--model", "{model}", "--effort", "{reasoning}", "--permission-mode", "dontAsk", "--no-session-persistence"},
	"omp":    {"omp", "--print", "--mode", "text", "--no-session", "--no-prewalk", "--approval-mode", "write", "--model", "{model}", "--thinking", "{reasoning}", "--cwd", "{workspace}", "@{promptFile}"},
}

// ValidateInvocationContract also checks programmatically constructed values;
// JSON Schema alone cannot protect callers that did not use LoadCatalog.
func ValidateInvocationContract(harness Harness, versionRange VersionRange) error {
	contract := versionRange.Invocation
	if contract == nil {
		return nil
	}
	if contract.SchemaVersion != 1 {
		return fmt.Errorf("%s: unsupported invocation schema", harness.ID)
	}
	if contract.Actions.Execution == nil && contract.Actions.Review == nil {
		return fmt.Errorf("%s: invocation requires at least one action", harness.ID)
	}
	for _, item := range []struct {
		name   string
		action *InvocationAction
	}{{"execution", contract.Actions.Execution}, {"review", contract.Actions.Review}} {
		if item.action == nil {
			continue
		}
		if err := validateInvocationAction(harness, item.name, *item.action); err != nil {
			return fmt.Errorf("%s: invocation %s: %w", harness.ID, item.name, err)
		}
	}
	return nil
}

func validateInvocationAction(harness Harness, name string, action InvocationAction) error {
	if len(action.Argv) < 1 || len(action.Argv) > 128 || action.Argv[0] != harness.Executable || !executableNamePattern.MatchString(action.Argv[0]) {
		return fmt.Errorf("argv must start with the declared executable and contain at most 128 arguments")
	}
	placeholders := make(map[string]int)
	for _, arg := range action.Argv {
		if !boundedInvocationText(arg, 4096) || strings.ContainsAny(arg, ";&|`$<>") {
			return fmt.Errorf("argv contains empty, oversized, control, or shell text")
		}
		switch arg {
		case "{model}", "{reasoning}", "{workspace}", "@{promptFile}":
			placeholders[arg]++
		default:
			if strings.ContainsAny(arg, "{}") {
				return fmt.Errorf("argv contains an unknown or embedded placeholder")
			}
		}
	}
	if placeholders["{model}"] != 1 || placeholders["{reasoning}"] != 1 {
		return fmt.Errorf("argv requires exactly one model and reasoning placeholder")
	}
	if expected, ok := invocationV1Argv[harness.ID]; !ok || !slices.Equal(action.Argv, expected) {
		return fmt.Errorf("argv does not match a supported version-1 invocation grammar")
	}
	switch action.WorkingDirectory {
	case "process-cwd":
		if placeholders["{workspace}"] != 0 {
			return fmt.Errorf("process-cwd must not also substitute a workspace argument")
		}
	case "argv":
		if placeholders["{workspace}"] != 1 {
			return fmt.Errorf("argv working directory requires exactly one workspace placeholder")
		}
	default:
		return fmt.Errorf("unsupported working directory contract")
	}
	if action.Prompt.MaxBytes < 1 || action.Prompt.MaxBytes > MaxInvocationPromptBytes {
		return fmt.Errorf("prompt byte bound must be between 1 and %d", MaxInvocationPromptBytes)
	}
	switch action.Prompt.Transport {
	case "stdin-text":
		if placeholders["@{promptFile}"] != 0 {
			return fmt.Errorf("stdin-text must not also substitute a prompt file")
		}
	case "at-file":
		if placeholders["@{promptFile}"] != 1 || action.Argv[len(action.Argv)-1] != "@{promptFile}" {
			return fmt.Errorf("at-file requires exactly one final prompt file argument")
		}
	default:
		return fmt.Errorf("unsupported prompt transport")
	}
	if action.Permissions.Mode != "harness-policy" || action.Permissions.ExternalBoundary != "required" || len(action.Permissions.Limitations) < 1 || len(action.Permissions.Limitations) > 8 {
		return fmt.Errorf("permissions must name harness-policy limitations and a required external boundary")
	}
	seen := make(map[string]bool)
	for _, limitation := range action.Permissions.Limitations {
		if !boundedInvocationText(limitation, 512) || strings.TrimSpace(limitation) == "" || seen[limitation] {
			return fmt.Errorf("permission limitations must be bounded, nonempty and unique")
		}
		seen[limitation] = true
	}
	if action.Output.Normalizer != "final-text-v1" || action.Output.MaxBytes < 1 || action.Output.MaxBytes > MaxInvocationOutputBytes {
		return fmt.Errorf("output requires final-text-v1 and a valid byte bound")
	}
	if action.Completion.SuccessExitCode != 0 || !action.Completion.RequiresFinalOutput || action.Completion.TaskCompletion != "not-established" {
		return fmt.Errorf("completion requires exit zero and final output without establishing task completion")
	}
	if name == "execution" {
		if action.Publication != nil {
			return fmt.Errorf("execution cannot declare review publication")
		}
	} else if p := action.Publication; p == nil || p.Contract != "noise-review-v1" || p.Actor != "reviewer" || !p.RequiresReadback || p.AuditRecord != "last-nonempty-line-json" {
		return fmt.Errorf("review requires reviewer publication and independent noise-review-v1 readback")
	}
	return nil
}

func boundedInvocationText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}
