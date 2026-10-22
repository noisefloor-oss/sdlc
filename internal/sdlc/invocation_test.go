package sdlc

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func invocationFixture(harness string) (Catalog, InvocationInput) {
	version, model := "2.1.263", "claude-opus-5"
	action := InvocationAction{
		Argv:             []string{"claude", "--print", "--input-format", "text", "--output-format", "text", "--model", "{model}", "--effort", "{reasoning}", "--permission-mode", "dontAsk", "--no-session-persistence"},
		WorkingDirectory: "process-cwd", Prompt: InvocationPrompt{Transport: "stdin-text", MaxBytes: 262144},
		Permissions: InvocationPermissions{Mode: "harness-policy", ExternalBoundary: "required", Limitations: []string{"Runner must independently enforce authorized effects."}},
		Output:      InvocationOutputContract{Normalizer: "final-text-v1", MaxBytes: 1048576},
		Completion:  InvocationCompletion{SuccessExitCode: 0, RequiresFinalOutput: true, TaskCompletion: "not-established"},
	}
	maximum := "2.1.264"
	if harness == "omp" {
		version, model, maximum = "18.1.11", "zai/glm-5.3", "18.1.12"
		action.Argv = []string{"omp", "--print", "--mode", "text", "--no-session", "--no-prewalk", "--approval-mode", "write", "--model", "{model}", "--thinking", "{reasoning}", "--cwd", "{workspace}", "@{promptFile}"}
		action.WorkingDirectory = "argv"
		action.Prompt.Transport = "at-file"
	}
	review := action
	review.Argv = append([]string(nil), action.Argv...)
	review.Publication = &InvocationPublication{Contract: "noise-review-v1", Actor: "reviewer", RequiresReadback: true, AuditRecord: "last-nonempty-line-json"}
	contract := &InvocationContract{SchemaVersion: 1, Actions: InvocationActions{Execution: &action, Review: &review}}
	catalog := Catalog{SchemaVersion: 1, Harnesses: []Harness{{
		ID: harness, Executable: harness,
		Ranges: []VersionRange{{Minimum: version, MaximumExclusive: maximum, Status: "experimental", ReasoningLevels: []string{"high", "max"}, Invocation: contract}},
		Models: []Model{{ID: model, ReasoningLevels: []string{"high"}, Status: "experimental"}},
	}}}
	input := InvocationInput{Harness: harness, Version: version, Action: "execution", Model: model, Reasoning: "high", Workspace: "/example/project", Prompt: []byte("Please inspect the change.\n")}
	if harness == "omp" {
		input.PromptFile = "/example/project/prompt.txt"
	}
	return catalog, input
}

func TestPrepareInvocationClaudePreservesLiteralPromptAndArgv(t *testing.T) {
	for _, action := range []string{"execution", "review"} {
		t.Run(action, func(t *testing.T) {
			catalog, input := invocationFixture("claude")
			input.Action = action
			input.Workspace = filepath.Join(t.TempDir(), "uncreated space ; $(inert) `text`")
			input.Prompt = []byte("  \n--dangerously-skip-permissions $HOME $(touch marker); | & < > `text` @{file}\r\nλ 雪  \n")
			original := bytes.Clone(input.Prompt)
			plan, err := PrepareInvocation(catalog, input)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"claude", "--print", "--input-format", "text", "--output-format", "text", "--model", "claude-opus-5", "--effort", "high", "--permission-mode", "dontAsk", "--no-session-persistence"}
			if !reflect.DeepEqual(plan.Argv, want) || !bytes.Equal(plan.Stdin, original) || plan.Cwd != input.Workspace || plan.PromptFile != nil {
				t.Fatalf("argument or prompt boundary changed: %#v", plan)
			}
			if plan.ExecutionAuthority != "not-granted" || plan.TaskCompletion != "not-established" || !strings.Contains(plan.RunnerBoundary, "required") {
				t.Fatal("preparation claimed authority or completion")
			}
			input.Prompt[0] = 'x'
			plan.Argv[0] = "changed"
			if !bytes.Equal(plan.Stdin, original) || catalog.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv[0] != "claude" {
				t.Fatal("plan aliases prompt or catalog inputs")
			}
			if _, err := os.Stat(input.Workspace); !os.IsNotExist(err) {
				t.Fatal("pure preparation inspected or created a required workspace")
			}
		})
	}
}

func TestPrepareInvocationOMPDeclaresOwnedFileWithoutCreatingIt(t *testing.T) {
	catalog, input := invocationFixture("omp")
	input.Workspace = filepath.Join(t.TempDir(), "uncreated workspace ; $(inert)")
	input.PromptFile = filepath.Join(input.Workspace, "prompt with spaces ; $literal.txt")
	input.Prompt = []byte(" --flag\n$(inert) ; `literal` @nested\n雪 \n")
	plan, err := PrepareInvocation(catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"omp", "--print", "--mode", "text", "--no-session", "--no-prewalk", "--approval-mode", "write", "--model", "zai/glm-5.3", "--thinking", "high", "--cwd", input.Workspace, "@" + input.PromptFile}
	if !reflect.DeepEqual(plan.Argv, want) || plan.Cwd != input.Workspace || len(plan.Stdin) != 0 || plan.PromptFile == nil {
		t.Fatalf("incorrect at-file launch description: %#v", plan)
	}
	file := plan.PromptFile
	if file.Path != input.PromptFile || !bytes.Equal(file.Bytes, input.Prompt) || file.Mode != 0600 || !strings.Contains(file.Create, "exclusive") || !strings.Contains(file.Create, "without-symlinks") || !strings.Contains(file.Cleanup, "all-exits") {
		t.Fatalf("missing prompt-file ownership boundary: %#v", file)
	}
	file.Bytes[0] = 'x'
	if input.Prompt[0] != ' ' {
		t.Fatal("prompt file aliases input bytes")
	}
	if _, err := os.Stat(input.Workspace); !os.IsNotExist(err) {
		t.Fatal("preparation created a workspace or prompt file")
	}
}

func TestPrepareInvocationRejectsUnsupportedInputs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Catalog, *InvocationInput)
	}{
		{"unknown-harness", func(c *Catalog, i *InvocationInput) { i.Harness = "missing" }},
		{"missing-version", func(c *Catalog, i *InvocationInput) { i.Version = "" }},
		{"decorated-version", func(c *Catalog, i *InvocationInput) { i.Version += "-unverified" }},
		{"noncanonical-version", func(c *Catalog, i *InvocationInput) { i.Version = "02.1.263" }},
		{"overflow-version", func(c *Catalog, i *InvocationInput) { i.Version = strings.Repeat("9", 100) + ".1.1" }},
		{"outside-range", func(c *Catalog, i *InvocationInput) { i.Version = "2.1.264" }},
		{"unknown-action", func(c *Catalog, i *InvocationInput) { i.Action = "shell" }},
		{"unknown-model", func(c *Catalog, i *InvocationInput) { i.Model = "--model=other" }},
		{"unknown-reasoning", func(c *Catalog, i *InvocationInput) { i.Reasoning = "ultra" }},
		{"model-reasoning-mismatch", func(c *Catalog, i *InvocationInput) { i.Reasoning = "max" }},
		{"range-reasoning-mismatch", func(c *Catalog, i *InvocationInput) { c.Harnesses[0].Ranges[0].ReasoningLevels = []string{"low"} }},
		{"relative-workspace", func(c *Catalog, i *InvocationInput) { i.Workspace = "relative" }},
		{"unclean-workspace", func(c *Catalog, i *InvocationInput) { i.Workspace = "/example/../project" }},
		{"control-workspace", func(c *Catalog, i *InvocationInput) { i.Workspace = "/example/\x00project" }},
		{"empty-prompt", func(c *Catalog, i *InvocationInput) { i.Prompt = nil }},
		{"invalid-utf8", func(c *Catalog, i *InvocationInput) { i.Prompt = []byte{0xff} }},
		{"nul-prompt", func(c *Catalog, i *InvocationInput) { i.Prompt = []byte{'a', 0, 'b'} }},
		{"oversize-prompt", func(c *Catalog, i *InvocationInput) { i.Prompt = bytes.Repeat([]byte("x"), 262145) }},
		{"unexpected-file-option", func(c *Catalog, i *InvocationInput) { i.PromptFile = "/example/project/prompt" }},
		{"missing-contract", func(c *Catalog, i *InvocationInput) { c.Harnesses[0].Ranges[0].Invocation = nil }},
		{"missing-action", func(c *Catalog, i *InvocationInput) { c.Harnesses[0].Ranges[0].Invocation.Actions.Execution = nil }},
		{"duplicate-harness", func(c *Catalog, i *InvocationInput) { c.Harnesses = append(c.Harnesses, c.Harnesses[0]) }},
		{"overlapping-range", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Ranges = append(c.Harnesses[0].Ranges, c.Harnesses[0].Ranges[0])
		}},
		{"duplicate-model", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Models = append(c.Harnesses[0].Models, c.Harnesses[0].Models[0])
		}},
		{"unknown-placeholder", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv[7] = "{secret}"
		}},
		{"embedded-placeholder", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv[7] = "prefix-{model}"
		}},
		{"raw-prompt-placeholder", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv = append(c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv, "{prompt}")
		}},
		{"unknown-option", func(c *Catalog, i *InvocationInput) {
			c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv = append(c.Harnesses[0].Ranges[0].Invocation.Actions.Execution.Argv, "--made-up-option")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			catalog, input := invocationFixture("claude")
			test.edit(&catalog, &input)
			if _, err := PrepareInvocation(catalog, input); err == nil {
				t.Fatal("unsupported invocation accepted")
			} else if strings.Contains(err.Error(), string(input.Prompt)) {
				if len(input.Prompt) > 0 {
					t.Fatal("error echoed prompt bytes")
				}
			}
		})
	}
	for _, path := range []string{"", "relative", "/outside/prompt", "/example/project", "/example/project/../prompt", "/example/project-extra/prompt"} {
		catalog, input := invocationFixture("omp")
		input.PromptFile = path
		if _, err := PrepareInvocation(catalog, input); err == nil {
			t.Fatalf("unsafe prompt-file declaration accepted: %q", path)
		}
	}
}

func TestNormalizeInvocationOutputPreservesFinalTextWithoutCompletionClaim(t *testing.T) {
	catalog, _ := invocationFixture("claude")
	contract := *catalog.Harnesses[0].Ranges[0].Invocation
	stdout := []byte("  Finished.\r\nλ 雪\n  ")
	result, err := NormalizeInvocationOutput(contract, "execution", 0, stdout)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != string(stdout) || result.ProcessOutcome != "succeeded" || result.Normalization != "valid" || result.TaskCompletion != "not-established" || result.ApprovalProof != "not-established" || result.Publication != "not-applicable" || result.AutomaticRetry {
		t.Fatalf("incorrect final output boundary: %#v", result)
	}
}

func TestNormalizeInvocationReviewAuditRemainsUnverifiedForEveryVerdict(t *testing.T) {
	catalog, _ := invocationFixture("omp")
	contract := *catalog.Harnesses[0].Ranges[0].Invocation
	for _, state := range []string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED"} {
		stdout := []byte("Review complete.\r\n{\"id\":9007199254740993,\"state\":\"" + state + "\",\"author\":\"noise-review[bot]\",\"author_type\":\"Bot\",\"submitted_at\":\"2026-09-06T00:00:00Z\"}\r\n \r\n")
		result, err := NormalizeInvocationOutput(contract, "review", 0, stdout)
		if err != nil {
			t.Fatal(err)
		}
		if result.Audit == nil || result.Audit.ID != 9007199254740993 || result.Audit.State != state || result.Publication != "unverified-claim" || result.ReadbackOwner != "noise-review" || result.ApprovalProof != "not-established" || result.TaskCompletion != "not-established" || result.AutomaticRetry || len(result.FailureCodes) != 0 {
			t.Fatalf("audit mistaken for proof or lost exact id: %#v", result)
		}
	}
}

func TestNormalizeInvocationMalformedAuditMeansUncertainPublication(t *testing.T) {
	catalog, _ := invocationFixture("claude")
	contract := *catalog.Harnesses[0].Ranges[0].Invocation
	valid := `{"id":42,"state":"APPROVED","author":"noise-review[bot]","author_type":"Bot"}`
	cases := []string{
		"review finished without audit", valid + "\nfollow-up prose", "```json\n" + valid + "\n```",
		"{\n\"id\":42,\n\"state\":\"APPROVED\"\n}", "[]", "null", valid + " {}",
		strings.Replace(valid, `"id":42`, `"id":0`, 1),
		strings.Replace(valid, `"id":42`, `"id":-1`, 1),
		strings.Replace(valid, `"id":42`, `"id":1.5`, 1),
		strings.Replace(valid, `"id":42`, `"id":"42"`, 1),
		strings.Replace(valid, `"id":42`, `"id":18446744073709551616`, 1),
		strings.Replace(valid, `"id":42`, `"id":42,"id":43`, 1),
		strings.Replace(valid, `"state":"APPROVED"`, `"state":"PENDING"`, 1),
		strings.Replace(valid, `"author":"noise-review[bot]"`, `"author":""`, 1),
		strings.Replace(valid, `"author":"noise-review[bot]"`, `"author":"actor\nother"`, 1),
		strings.Replace(valid, `,"author_type":"Bot"`, "", 1),
		strings.Replace(valid, `"author_type":"Bot"`, `"author_type":"User"`, 1),
	}
	for index, text := range cases {
		result, err := NormalizeInvocationOutput(contract, "review", 0, []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if result.Publication != "uncertain" || result.AutomaticRetry || result.Audit != nil || result.Normalization != "invalid" || result.ApprovalProof != "not-established" || !contains(result.FailureCodes, "missing-or-malformed-review-audit") {
			t.Fatalf("case %d turned ambiguous publication into certainty: %#v", index, result)
		}
	}
	result, err := NormalizeInvocationOutput(contract, "review", 1, []byte(valid))
	if err != nil || result.Audit == nil || result.Publication != "uncertain" || result.ProcessOutcome != "failed" || result.AutomaticRetry {
		t.Fatalf("failed process with audit allowed a retry or success: %#v, %v", result, err)
	}
}

func TestNormalizeInvocationBoundsAndProcessFailure(t *testing.T) {
	catalog, _ := invocationFixture("claude")
	contract := *catalog.Harnesses[0].Ranges[0].Invocation
	contract.Actions.Review.Output.MaxBytes = 4
	for _, stdout := range [][]byte{nil, []byte(" \r\n"), []byte("12345"), {0xff}, {'x', 0}} {
		result, err := NormalizeInvocationOutput(contract, "review", 1, stdout)
		if err != nil || result.Normalization != "invalid" || result.FinalText != "" || result.Publication != "uncertain" || result.ProcessOutcome != "failed" || result.AutomaticRetry {
			t.Fatalf("unbounded or failed output normalized: %#v, %v", result, err)
		}
	}
	contract.Actions.Execution.Output.MaxBytes = 4
	result, err := NormalizeInvocationOutput(contract, "execution", -1, []byte("雪x"))
	if err != nil || result.Normalization != "valid" || result.ProcessOutcome != "failed" || result.FinalText != "雪x" || result.TaskCompletion != "not-established" {
		t.Fatalf("UTF-8 byte bound or failed-exit handling is wrong: %#v, %v", result, err)
	}
	if _, err := NormalizeInvocationOutput(contract, "unknown", 0, nil); err == nil {
		t.Fatal("unknown action accepted")
	}
	contract.Actions.Review.Publication.RequiresReadback = false
	if result, err := NormalizeInvocationOutput(contract, "review", 0, nil); err == nil || result.Publication != "uncertain" || result.AutomaticRetry || result.Normalization != "invalid" {
		t.Fatal("review without readback boundary accepted or lost publication uncertainty")
	}
	contract.Actions.Execution.Output.Normalizer = "provider-events"
	if _, err := NormalizeInvocationOutput(contract, "execution", 0, nil); err == nil {
		t.Fatal("provider events accepted as final text")
	}
}
