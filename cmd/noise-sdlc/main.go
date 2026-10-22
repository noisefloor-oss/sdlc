package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "noise-sdlc: %v\n", err)
		os.Exit(2)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		fmt.Print(topHelp())
		return nil
	}
	switch arguments[0] {
	case "--version":
		if len(arguments) == 2 && arguments[1] == "--json" {
			return emitRuntimeVersion()
		}
		if len(arguments) != 1 {
			return errors.New("usage: noise-sdlc --version [--json]")
		}
		fmt.Println(sdlc.Version)
		return nil
	case "--help", "-h":
		fmt.Print(topHelp())
		return nil
	case "help":
		if len(arguments) == 1 {
			fmt.Print(topHelp())
			return nil
		}
		if text, ok := commandHelp(arguments[1]); ok {
			fmt.Print(text)
			return nil
		}
		return fmt.Errorf("unknown command: %s (run 'noise-sdlc help' for usage)", arguments[1])
	}
	if !commandKnown(arguments[0]) {
		return fmt.Errorf("unknown command: %s (run 'noise-sdlc help' for usage)", arguments[0])
	}
	if len(arguments) > 1 && (arguments[1] == "--help" || arguments[1] == "-h") {
		text, _ := commandHelp(arguments[0])
		fmt.Print(text)
		return nil
	}
	root, err := sdlc.RepositoryRoot()
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "list":
		seed, err := sdlc.LoadSeed(root)
		if err != nil {
			return err
		}
		return emit(map[string]any{"seeds": []map[string]string{{"id": seed.ID, "title": seed.Title}}})
	case "show":
		return showSeed(root, arguments[1:])
	case "harness":
		return harnessCommand(root, arguments[1:])
	case "inventory":
		return inventoryCommand(root, arguments[1:])
	case "execution":
		return executionCommand(root, arguments[1:])
	case "review":
		return reviewEvidenceCommand(root, arguments[1:])
	case "bootstrap":
		return bootstrapCommand(arguments[1:])
	case "plan":
		return planCommand(root, arguments[1:], false)
	case "adopt":
		return planCommand(root, arguments[1:], true)
	case "verify", "status":
		return verifyCommand(arguments[0], arguments[1:])
	case "diagnose":
		options := flag.NewFlagSet("diagnose", flag.ContinueOnError)
		target := options.String("target", ".", "project directory")
		if err := options.Parse(arguments[1:]); err != nil {
			return err
		}
		if options.NArg() != 0 {
			return errors.New("usage: noise-sdlc diagnose [--target DIR]")
		}
		result, err := sdlc.Diagnose(root, *target)
		if err != nil {
			return err
		}
		return emit(result)
	case "remove":
		return removeCommand(arguments[1:])
	case "sync":
		return syncCommand(root, arguments[1:])
	case "onboarding":
		return onboardingCommand(arguments[1:])
	case "web":
		return webCommand(root, arguments[1:])
	case "check-catalog":
		if err := sdlc.ValidateAll(root); err != nil {
			return err
		}
		return emit(map[string]any{"catalog": "valid", "liveCertification": "not-run"})
	case "check-evidence":
		if len(arguments) != 2 || strings.HasPrefix(arguments[1], "-") {
			return errors.New("usage: noise-sdlc check-evidence FILE")
		}
		result, err := sdlc.CheckEvidence(root, arguments[1])
		if err != nil {
			return err
		}
		return emit(result)
	default:
		return fmt.Errorf("unknown command: %s (run 'noise-sdlc help' for usage)", arguments[0])
	}
}

func bootstrapCommand(arguments []string) error {
	options, err := parseTarget(arguments)
	if err != nil {
		return err
	}
	result, err := sdlc.Bootstrap(options.target)
	if err != nil {
		return err
	}
	return emit(result)
}

// commandKnown reports whether name is dispatched by run's command switch.
// It is backed by commandDocs, so help output and dispatch cannot drift apart
// (enforced by TestHelpCoversDispatchedCommands).
func commandKnown(name string) bool {
	for _, doc := range commandDocs {
		if doc.name == name {
			return true
		}
	}
	return name == "status"
}

func showSeed(root string, arguments []string) error {
	seed, err := sdlc.LoadSeed(root)
	if err != nil {
		return err
	}
	requested := seed.ID
	if len(arguments) > 0 {
		requested = arguments[0]
	}
	if requested != seed.ID {
		return fmt.Errorf("unknown seed: %s", requested)
	}
	return emit(seed)
}

func harnessCommand(root string, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("harness subcommand is required")
	}
	catalog, err := sdlc.LoadCatalog(root)
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "list":
		type rangeSummary struct {
			Minimum          string `json:"minimum"`
			MaximumExclusive string `json:"maximumExclusive"`
			Status           string `json:"status"`
		}
		type harnessSummary struct {
			ID     string         `json:"id"`
			Ranges []rangeSummary `json:"ranges"`
		}
		summaries := make([]harnessSummary, 0, len(catalog.Harnesses))
		for _, harness := range catalog.Harnesses {
			ranges := make([]rangeSummary, 0, len(harness.Ranges))
			for _, versionRange := range harness.Ranges {
				ranges = append(ranges, rangeSummary{versionRange.Minimum, versionRange.MaximumExclusive, versionRange.Status})
			}
			summaries = append(summaries, harnessSummary{harness.ID, ranges})
		}
		return emit(map[string]any{"catalog": catalog.Catalog, "harnesses": summaries})
	case "show":
		if len(arguments) != 2 {
			return errors.New("harness show requires an id")
		}
		for _, harness := range catalog.Harnesses {
			if harness.ID == arguments[1] {
				return emit(harness)
			}
		}
		return fmt.Errorf("unknown harness: %s", arguments[1])
	case "interaction":
		return emit(sdlc.InteractionContract())
	case "candidates":
		options, err := parseTarget(arguments[1:])
		if err != nil {
			return err
		}
		result, err := sdlc.CatalogCandidates(options.target, catalog)
		if err != nil {
			return err
		}
		return emit(result)
	default:
		return fmt.Errorf("unknown harness subcommand: %s", arguments[0])
	}
}

func inventoryCommand(root string, arguments []string) error {
	options, err := parseCommon(arguments, false)
	if err != nil {
		return err
	}
	var declarations []sdlc.OperatorCapability
	if options.answers != "" {
		answers, err := loadAnswers(options.answers)
		if err != nil {
			return err
		}
		declarations = answers.OperatorCapabilities
	}
	catalog, err := sdlc.LoadCatalog(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Inventory: probing %d harnesses and candidates…\n", len(catalog.Harnesses)+len(catalog.DiscoveryCandidates))
	inventory := sdlc.DiscoverInventoryWithProgress(
		catalog,
		declarations,
		options.target,
		func(harnessID, state string) {
			fmt.Fprintf(os.Stderr, "  %-16s %s\n", harnessID, state)
		},
	)
	fmt.Fprintln(os.Stderr, "Inventory: complete.")
	return emit(inventory)
}

func executionCommand(root string, arguments []string) error {
	if len(arguments) > 0 && arguments[0] == "show" {
		options := flag.NewFlagSet("execution show", flag.ContinueOnError)
		target := options.String("target", ".", "adopted project directory")
		role := options.String("role", "", "one adopted role")
		version := options.String("harness-version", "", "caller-declared harness version")
		asJSON := options.Bool("json", false, "structured guidance")
		if err := options.Parse(arguments[1:]); err != nil {
			return err
		}
		if options.NArg() != 0 {
			return errors.New("usage: execution show [--target DIR] [--role ROLE] [--harness-version VERSION] [--json]")
		}
		result, err := sdlc.ExecutionShow(root, *target, *role, *version)
		if err != nil {
			return err
		}
		if *asJSON {
			return emit(result)
		}
		return printExecutionGuidance(result)
	}
	if len(arguments) == 0 || arguments[0] != "plan" {
		return errors.New("execution plan or show is required")
	}
	options, err := parseCommon(arguments[1:], false)
	if err != nil {
		return err
	}
	answers, err := answersForCommand(options.answers)
	if err != nil {
		return err
	}
	result, err := sdlc.BuildPlan(options.target, root, answers, nil)
	if err != nil {
		return err
	}
	recommendation := result.Plan["recommendation"].(map[string]any)
	return emit(map[string]any{"template": answers.ExecutionTemplate, "bindings": recommendation["bindings"], "alternatives": result.Plan["alternatives"], "warnings": result.Plan["warnings"]})
}

func printExecutionGuidance(result map[string]any) error {
	fmt.Printf("Execution guidance: %s\n", result["state"])
	fmt.Printf("Adopted catalog: %s; available: %s\n", result["adoptedCatalog"], result["availableCatalog"])
	fmt.Printf("Policy: %s\n", result["policyDigest"])
	fmt.Println("Work: noise tasks. Review publication: noise review.")
	fmt.Println("No harness was launched; runtime version and execution authority are unverified.")
	if next, ok := result["nextStep"]; ok {
		fmt.Println(next)
	}
	for _, raw := range result["roles"].([]any) {
		role := raw.(map[string]any)
		binding := role["binding"].(sdlc.Binding)
		fmt.Printf("\n%s: %s\n", binding.Role, role["state"])
		if binding.Harness != nil && binding.Model != nil {
			fmt.Printf("  %s / %s\n", *binding.Harness, *binding.Model)
		}
		if binding.Reasoning != nil {
			fmt.Printf("  Reasoning: %s\n", *binding.Reasoning)
		}
		if binding.ModelFamily != nil {
			fmt.Printf("  Model family: %s\n", *binding.ModelFamily)
		}
		if version, ok := role["declaredHarnessVersion"].(string); ok && version != "" {
			fmt.Printf("  Caller-declared harness version: %s\n", version)
		}
		fmt.Println(role["nextStep"])
		if contracts := role["contracts"].([]any); len(contracts) > 0 {
			if err := emit(contracts); err != nil {
				return err
			}
		}
	}
	return nil
}

func planCommand(root string, arguments []string, install bool) error {
	options, err := parseCommon(arguments, install)
	if err != nil {
		return err
	}
	answers, err := answersForCommand(options.answers)
	if err != nil {
		return err
	}
	result, err := sdlc.BuildPlan(options.target, root, answers, nil)
	if err != nil {
		return err
	}
	if err := emit(result.Plan); err != nil {
		return err
	}
	if !install {
		return nil
	}
	if !options.yes {
		fmt.Fprintln(os.Stderr, "Plan only; pass --yes to install these exact generated bytes.")
		return nil
	}
	if err := sdlc.Adopt(options.target, result.Outputs); err != nil {
		return err
	}
	if _, err := sdlc.Verify(options.target); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Adopted; managed artifact integrity verified.")
	return nil
}

func verifyCommand(command string, arguments []string) error {
	options, err := parseTarget(arguments)
	if err != nil {
		return err
	}
	result, err := sdlc.Verify(options.target)
	if err != nil {
		return err
	}
	if command == "status" {
		var project map[string]any
		if err := sdlc.LoadFlexibleJSON(filepath.Join(options.target, ".sdlc", "project.yaml"), &project); err != nil {
			return err
		}
		result["bindings"] = project["execution"].(map[string]any)["roles"]
	}
	return emit(result)
}

func removeCommand(arguments []string) error {
	options, err := parseCommon(arguments, true)
	if err != nil {
		return err
	}
	if !options.yes {
		return errors.New("remove is destructive; pass --yes after reviewing the intact adoption")
	}
	if err := sdlc.RemoveAdoption(options.target); err != nil {
		return err
	}
	return emit(map[string]any{"removed": true, "recoverable": false})
}

func syncCommand(root string, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("sync subcommand is required")
	}
	catalog, err := sdlc.LoadCatalog(root)
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "export":
		template, err := sdlc.LoadTemplate(root)
		if err != nil {
			return err
		}
		return emit(sdlc.DeckSyncBundle(catalog, template))
	case "status":
		options, err := parseTarget(arguments[1:])
		if err != nil {
			return err
		}
		return emit(sdlc.DeckSyncStatus(options.target, catalog))
	case "accept":
		if len(arguments) < 2 {
			return errors.New("sync accept requires a receipt")
		}
		options, err := parseTarget(arguments[2:])
		if err != nil {
			return err
		}
		var receipt map[string]any
		if err := sdlc.LoadFlexibleJSON(arguments[1], &receipt); err != nil {
			return err
		}
		result, err := sdlc.AcceptDeckReceipt(receipt, options.target, catalog)
		if err != nil {
			return err
		}
		return emit(result)
	case "pull":
		if len(arguments) < 2 {
			return errors.New("sync pull requires a URL")
		}
		options, err := parseTarget(arguments[2:])
		if err != nil {
			return err
		}
		result, err := sdlc.PullDeckReceipt(arguments[1], options.target, catalog)
		if err != nil {
			return err
		}
		return emit(result)
	default:
		return fmt.Errorf("unknown sync subcommand: %s", arguments[0])
	}
}

func onboardingCommand(arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "status" {
		return errors.New("onboarding status is required")
	}
	options, err := parseTarget(arguments[1:])
	if err != nil {
		return err
	}
	return emit(sdlc.OnboardingStatus(options.target))
}

func webCommand(root string, arguments []string) error {
	options, err := parseWeb(arguments)
	if err != nil {
		return err
	}
	answers := defaultAnswers()
	if options.answers != "" {
		answers, err = loadAnswers(options.answers)
		if err != nil {
			return err
		}
	}
	return sdlc.ServeWeb(options.host, options.port, options.target, root, answers)
}

type commonOptions struct {
	target, answers string
	yes             bool
}

func parseCommon(arguments []string, allowYes bool) (commonOptions, error) {
	set := flag.NewFlagSet("noise-sdlc", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	options := commonOptions{}
	set.StringVar(&options.target, "target", ".", "target project")
	set.StringVar(&options.answers, "answers", "", "answers JSON")
	if allowYes {
		set.BoolVar(&options.yes, "yes", false, "confirm mutation")
	}
	if err := set.Parse(arguments); err != nil {
		return commonOptions{}, err
	}
	return options, nil
}
func parseTarget(arguments []string) (commonOptions, error) { return parseCommon(arguments, false) }

type webOptions struct {
	commonOptions
	host string
	port int
}

func parseWeb(arguments []string) (webOptions, error) {
	set := flag.NewFlagSet("web", flag.ContinueOnError)
	options := webOptions{}
	set.StringVar(&options.target, "target", ".", "target project")
	set.StringVar(&options.answers, "answers", "", "answers JSON")
	set.StringVar(&options.host, "host", "127.0.0.1", "host")
	set.IntVar(&options.port, "port", 8765, "port")
	if err := set.Parse(arguments); err != nil {
		return webOptions{}, err
	}
	return options, nil
}

func loadAnswers(path string) (sdlc.AnswersFile, error) {
	var answers sdlc.AnswersFile
	err := sdlc.LoadJSON(path, &answers)
	if err == nil {
		err = sdlc.ValidateAnswers(answers)
	}
	return answers, err
}
func answersForCommand(path string) (sdlc.AnswersFile, error) {
	if path != "" {
		return loadAnswers(path)
	}
	return interactiveAnswers()
}
func defaultAnswers() sdlc.AnswersFile {
	return sdlc.AnswersFile{
		SchemaVersion:     1,
		Seed:              "noise/agent@0.1",
		ExecutionTemplate: sdlc.ExecutionTemplateID,
		Answers: sdlc.Answers{
			Authorship:        "agent",
			Delivery:          "software",
			Risk:              "ordinary",
			RoutingPreference: "balanced",
			HighRiskClasses:   []string{},
		},
		OperatorCapabilities: []sdlc.OperatorCapability{},
		ProjectRules:         []sdlc.Rule{},
	}
}

func interactiveAnswers() (sdlc.AnswersFile, error) {
	reader := bufio.NewReader(os.Stdin)
	answers := defaultAnswers()
	var err error
	if answers.Answers.Authorship, err = choose(reader, "Who primarily authors changes?", []string{"agent", "mixed", "human"}); err != nil {
		return answers, err
	}
	if answers.Answers.Delivery, err = choose(reader, "What does this project deliver?", []string{"software", "both", "research"}); err != nil {
		return answers, err
	}
	if answers.Answers.Risk, err = choose(reader, "Does it contain named elevated-risk work?", []string{"ordinary", "elevated"}); err != nil {
		return answers, err
	}
	if answers.Answers.Risk == "elevated" {
		fmt.Fprint(os.Stderr, "Public high-risk class labels (comma-separated): ")
		raw, _ := reader.ReadString('\n')
		set := map[string]struct{}{}
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				set[part] = struct{}{}
			}
		}
		for item := range set {
			answers.Answers.HighRiskClasses = append(answers.Answers.HighRiskClasses, item)
		}
		sort.Strings(answers.Answers.HighRiskClasses)
	}
	answers.Answers.RoutingPreference, err = choose(reader, "How should eligible routes be ranked?", []string{"balanced", "included-first", "capability-first"})
	return answers, err
}
func choose(reader *bufio.Reader, question string, choices []string) (string, error) {
	for {
		fmt.Fprintln(os.Stderr, question)
		for i, choice := range choices {
			fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, choice)
		}
		fmt.Fprint(os.Stderr, "> ")
		raw, err := reader.ReadString('\n')
		if err != nil {
			return "", errors.New("guided planning needs a terminal or --answers FILE")
		}
		answer := strings.TrimSpace(raw)
		for _, choice := range choices {
			if answer == choice {
				return choice, nil
			}
		}
		for i := range choices {
			if answer == fmt.Sprint(i+1) {
				return choices[i], nil
			}
		}
		fmt.Fprintln(os.Stderr, "Choose a listed number or value.")
	}
}
func emit(value any) error {
	payload, err := sdlc.CanonicalBytes(value)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(payload)
	return err
}
