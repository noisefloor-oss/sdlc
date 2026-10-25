package sdlc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestObservedInstalledInvocationContracts(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ harness, version, next, model, family string }{
		{"claude", "2.1.310", "2.1.311", "claude-opus-5", "anthropic-claude-5"},
		{"codex", "0.162.1", "0.162.2", "gpt-5.6-sol", "openai-gpt-5.6"},
	} {
		t.Run(c.harness, func(t *testing.T) {
			h := findHarnessByID(t, catalog, c.harness)
			r := matchingRange(h, c.version)
			if r == nil || r.Invocation == nil || r.Status != "experimental" || r.Certification.Live != "missing" || r.Certification.Deck != "missing" || len(r.Certification.TestedVersions) != 0 {
				t.Fatal("syntax observation invented certification")
			}
			for _, action := range []string{"execution", "review"} {
				input := InvocationInput{Harness: c.harness, Version: c.version, Action: action, Model: c.model, Reasoning: "high", Workspace: "/example/path with spaces", Prompt: []byte("--flag\n$(inert); `literal` 雪\n")}
				plan, err := PrepareInvocation(catalog, input)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(plan.Stdin, input.Prompt) || plan.Cwd != input.Workspace || plan.ExecutionAuthority != "not-granted" || plan.TaskCompletion != "not-established" {
					t.Fatal("transport or authority changed")
				}
				if c.harness == "codex" {
					want := []string{"codex", "exec", "--ephemeral", "--model", c.model, "--config", `model_reasoning_effort="high"`, "--sandbox", "workspace-write", "--color", "never", "-"}
					if !reflect.DeepEqual(plan.Argv, want) {
						t.Fatalf("unsafe/config argv: %#v", plan.Argv)
					}
					input.Reasoning = `high"; invalid=true`
					if _, err := PrepareInvocation(catalog, input); err == nil {
						t.Fatal("unvalidated reasoning reached config")
					}
				}
				input.Reasoning = "high"
				input.Version = c.next
				if _, err := PrepareInvocation(catalog, input); err == nil {
					t.Fatal("unobserved next version accepted")
				}
			}
			binding := Binding{Role: "review.formal", Status: "resolved", Harness: stringPointer(c.harness), Model: stringPointer(c.model), ModelFamily: stringPointer(c.family), Reasoning: stringPointer("high")}
			if executionRoleGuidance(catalog, binding, c.version)["state"] != "declared-contracts" {
				t.Fatal("missing installed-version guidance")
			}
			action := r.Invocation.Actions.Review
			if action.Publication == nil || !action.Publication.RequiresReadback || action.Publication.Actor != "reviewer" {
				t.Fatal("review readback weakened")
			}
		})
	}
}

func TestCodexGrammarRejectsConfigAndPermissionSubstitution(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		edit func(*InvocationAction)
	}{
		{"unsafe-sandbox", func(a *InvocationAction) { a.Argv[8] = "danger-full-access" }},
		{"embedded-config", func(a *InvocationAction) { a.Argv[6] = `model_reasoning_effort="{reasoning}"` }},
		{"extra-config", func(a *InvocationAction) { a.Argv = append(a.Argv, "--config", "other=true") }},
		{"mixed-reasoning", func(a *InvocationAction) { a.Argv = append(a.Argv, "{reasoning}") }},
		{"shell", func(a *InvocationAction) { a.Argv[6] = "$(inert)" }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			catalog, err := LoadCatalog(root)
			if err != nil {
				t.Fatal(err)
			}
			h := findHarnessByID(t, catalog, "codex")
			r := matchingRange(h, "0.162.1")
			mutation.edit(r.Invocation.Actions.Execution)
			if err := ValidateInvocationContract(h, *r); err == nil {
				t.Fatal("unsafe grammar accepted")
			}
		})
	}
	// The new typed placeholder remains forbidden in legacy Claude grammar.
	r := testInvocationRange()
	r.Invocation.Actions.Execution.Argv[9] = "{reasoningConfig}"
	if err := ValidateInvocationContract(Harness{ID: "claude", Executable: "claude"}, r); err == nil || !strings.Contains(err.Error(), "grammar") {
		t.Fatalf("cross-harness placeholder accepted: %v", err)
	}
}

func TestCodexInvocationFakeCLIReceivesLiteralInput(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	script := `#!/bin/sh
set -eu
test "$#" = 11
test "$1" = exec
test "$2" = --ephemeral
test "$3" = --model
test "$4" = gpt-5.6-sol
test "$5" = --config
test "$6" = 'model_reasoning_effort="high"'
test "$7" = --sandbox
test "$8" = workspace-write
test "$9" = --color
shift 9
test "$1" = never
test "$2" = -
cat
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	input := InvocationInput{Harness: "codex", Version: "0.162.1", Action: "review", Model: "gpt-5.6-sol", Reasoning: "high", Workspace: directory, Prompt: []byte("--flag\n$(inert); 雪\n")}
	plan, err := PrepareInvocation(catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, plan.Argv[1:]...)
	command.Dir = plan.Cwd
	command.Stdin = bytes.NewReader(plan.Stdin)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, input.Prompt) {
		t.Fatalf("literal stdin changed: %q", output)
	}
}
