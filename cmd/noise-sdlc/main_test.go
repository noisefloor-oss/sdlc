package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

// Source fixtures explicitly opt in to the checkout's resources.
func TestMain(m *testing.M) {
	root, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("NOISE_SDLC_SOURCE_ROOT", root); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestStatusReadsAuthoritativeProjectYAML(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := adoptTestProject(t, root, t.TempDir())
	if err := verifyCommand("status", []string{"--target", target}); err != nil {
		t.Fatalf("status rejected a valid adoption: %v", err)
	}
}

func TestExecutionShowCLIUsesAdoptedGuidance(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	target := adoptTestProject(t, root, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"execution", "show", "--target", target, "--role", "review.formal"}
		if jsonOutput {
			args = append(args, "--json")
		}
		output, err := captureStdout(t, func() error { return run(args) })
		if err != nil || !strings.Contains(output, "review.formal") || !strings.Contains(output, "unresolved") {
			t.Fatalf("unexpected live guidance: %v %s", err, output)
		}
	}
	for _, args := range [][]string{
		{"show", "--target", target, "unexpected"},
		{"show", "--target", target, "--answers", "unused"},
		{"show", "--target", target, "--harness-version", "2.1.263"},
	} {
		if err := executionCommand(root, args); err == nil {
			t.Fatalf("accepted invalid options: %v", args)
		}
	}
}

func TestExecutionTextGuidanceIncludesSelectedSubstitutions(t *testing.T) {
	harness, model, family, reasoning := "claude", "claude-opus-5", "anthropic-claude-5", "high"
	binding := sdlc.Binding{Role: "implementation.high", Status: "resolved", Harness: &harness, Model: &model, ModelFamily: &family, Reasoning: &reasoning}
	result := map[string]any{
		"state": "available", "adoptedCatalog": "test", "availableCatalog": "test", "policyDigest": "sha256:test",
		"roles": []any{map[string]any{"binding": binding, "state": "declared-contracts", "declaredHarnessVersion": "2.1.263", "contracts": []any{}, "nextStep": "Verify runtime before execution."}},
	}
	output, err := captureStdout(t, func() error { return printExecutionGuidance(result) })
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{harness, model, "Reasoning: high", "Model family: " + family, "Caller-declared harness version: 2.1.263", "Policy: sha256:test"} {
		if !strings.Contains(output, required) {
			t.Fatalf("text guidance omits %q: %s", required, output)
		}
	}
}

func TestLauncherRunsOutsideModule(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(root, "bin", "noise-sdlc"), "--version")
	command.Dir = t.TempDir()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("launcher failed outside the module: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != sdlc.Version {
		t.Fatalf("unexpected version output: %q", output)
	}
}

func TestLauncherPreservesRelativeTargetOutsideModule(t *testing.T) {
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	caller := t.TempDir()
	target := filepath.Join(caller, "project")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	adoptTestProject(t, root, target)

	command := exec.Command(filepath.Join(root, "bin", "noise-sdlc"), "status", "--target", "project")
	command.Dir = caller
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("launcher lost the caller-relative target: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"integrity": "verified"`) {
		t.Fatalf("unexpected status output: %s", output)
	}
}

func adoptTestProject(t *testing.T, root, target string) string {
	t.Helper()
	answers, err := loadAnswers(filepath.Join(root, "examples", "sdlc-answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	inventory := sdlc.Inventory{SchemaVersion: 1, Source: "test", Catalog: "test", Harnesses: []sdlc.HarnessObservation{}}
	result, err := sdlc.BuildPlan(target, root, answers, &inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := sdlc.Adopt(target, result.Outputs); err != nil {
		t.Fatal(err)
	}
	return target
}

// TestHelpCoversDispatchedCommands parses the dispatch switch in main.go and
// asserts every dispatched command name appears in the top-level help, so the
// help output cannot drift from the switch.
func TestHelpCoversDispatchedCommands(t *testing.T) {
	help := topHelp()
	for _, name := range dispatchedCommands(t) {
		if !strings.Contains(help, name) {
			t.Errorf("top-level help does not mention dispatched command %q", name)
		}
		if name != "status" { // status is documented as the verify alias
			if _, ok := commandHelp(name); !ok {
				t.Errorf("no per-command help for dispatched command %q", name)
			}
		}
	}
}

func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "cmd", "noise-sdlc", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" {
			continue
		}
		var names []string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if strings.HasPrefix(name, "-") || name == "help" {
					continue
				}
				names = append(names, name)
			}
			return true
		})
		if len(names) == 0 {
			t.Fatal("found no command names in run's dispatch switch")
		}
		return names
	}
	t.Fatal("run function not found in main.go")
	return nil
}

// captureStdout reroutes os.Stdout while action runs and returns its output.
func captureStdout(t *testing.T, action func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	runErr := action()
	writer.Close()
	os.Stdout = original
	output, err := os.ReadFile("/dev/fd/" + strconv.Itoa(int(reader.Fd())))
	if err != nil {
		t.Fatal(err)
	}
	return string(output), runErr
}

func TestHelpPathsExitZero(t *testing.T) {
	for _, arguments := range [][]string{{}, {"help"}, {"--help"}, {"-h"}} {
		output, err := captureStdout(t, func() error { return run(arguments) })
		if err != nil {
			t.Errorf("run(%v) returned %v", arguments, err)
		}
		if !strings.Contains(output, "Usage:") {
			t.Errorf("run(%v) did not print top-level help to stdout", arguments)
		}
	}
	for _, arguments := range [][]string{{"help", "verify"}, {"verify", "--help"}, {"verify", "-h"}, {"help", "adopt"}, {"adopt", "--help"}} {
		output, err := captureStdout(t, func() error { return run(arguments) })
		if err != nil {
			t.Errorf("run(%v) returned %v", arguments, err)
		}
		for _, section := range []string{"Synopsis:", "Reads:", "Writes:", "Requires:", "Example:"} {
			if !strings.Contains(output, section) {
				t.Errorf("run(%v) help is missing %s", arguments, section)
			}
		}
	}
}

func TestUnknownCommandSuggestsHelp(t *testing.T) {
	if _, err := captureStdout(t, func() error { return run([]string{"bogus"}) }); err == nil {
		t.Fatal("run([bogus]) did not fail")
	} else if !strings.Contains(err.Error(), "noise-sdlc help") {
		t.Fatalf("unknown-command error does not suggest help: %v", err)
	}
	if _, err := captureStdout(t, func() error { return run([]string{"help", "bogus"}) }); err == nil {
		t.Fatal("run([help bogus]) did not fail")
	} else if !strings.Contains(err.Error(), "noise-sdlc help") {
		t.Fatalf("unknown help topic error does not suggest help: %v", err)
	}
}

func TestVerifyHelpStatesScopeLimits(t *testing.T) {
	text, ok := commandHelp("verify")
	if !ok {
		t.Fatal("no help for verify")
	}
	if !strings.Contains(text, "not conformance evidence") {
		t.Fatal("verify help does not state its scope limits")
	}
}

func TestBootstrapHelpStatesSeedOnceAndApprovalBoundary(t *testing.T) {
	text, ok := commandHelp("bootstrap")
	if !ok {
		t.Fatal("no help for bootstrap")
	}
	for _, phrase := range []string{"BOOTSTRAP.md", "idempotent", "Successful adoption consumes", "does not plan or adopt policy"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("bootstrap help is missing %q", phrase)
		}
	}
}

func TestAdoptHelpDisclosesBootstrapCleanup(t *testing.T) {
	text, ok := commandHelp("adopt")
	if !ok {
		t.Fatal("no help for adopt")
	}
	for _, phrase := range []string{"BOOTSTRAP.md", ".sdlc/bootstrap.json", "consumed after validation"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("adopt help is missing %q", phrase)
		}
	}
}

func TestStatusAliasHasHelp(t *testing.T) {
	for _, arguments := range [][]string{{"help", "status"}, {"status", "--help"}} {
		output, err := captureStdout(t, func() error { return run(arguments) })
		if err != nil {
			t.Errorf("run(%v) returned %v", arguments, err)
		}
		if !strings.Contains(output, "Synopsis:") {
			t.Errorf("run(%v) did not print the verify/status help", arguments)
		}
	}
}

func TestHelpStaysWithinEightyColumns(t *testing.T) {
	texts := []string{topHelp()}
	for _, doc := range commandDocs {
		text, ok := commandHelp(doc.name)
		if !ok {
			t.Fatalf("no per-command help for %q", doc.name)
		}
		texts = append(texts, text)
	}
	for _, text := range texts {
		for line := range strings.Lines(text) {
			if len(strings.TrimRight(line, "\n")) > 80 {
				t.Errorf("help line exceeds 80 columns: %q", line)
			}
		}
	}
}
