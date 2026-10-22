package sdlc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	RendererVersion  = "noise-sdlc-renderer@0.1"
	BlockBegin       = "<!-- noise-sdlc:begin -->"
	BlockEnd         = "<!-- noise-sdlc:end -->"
	SDLCEndPrefix    = "<!-- noise-sdlc:end effective-policy-sha256:"
	IntegrityWarning = "Collection integrity is not lifecycle conformance, review approval, security evidence, runtime acceptance, or merge readiness."
	ExecutionWarning = "Execution bindings are routing policy only; they do not launch a provider, authenticate, or authorize spend."
)

var (
	publicLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	projectRulePattern = regexp.MustCompile(`^project\.[a-z0-9-]+\.[a-z0-9.-]+$`)
	gitRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	qualityRank        = map[string]int{"unknown": -1, "economy": 0, "balanced": 1, "frontier": 2}
)

type BuildResult struct {
	Plan    map[string]any
	Outputs map[string][]byte
}

func ValidateAnswers(answers AnswersFile) error {
	if answers.SchemaVersion != 1 || answers.Seed != "noise/agent@0.1" || answers.ExecutionTemplate != ExecutionTemplateID {
		return fmt.Errorf("answers pin an unsupported version, seed, or execution template")
	}
	if !oneOf(answers.Answers.Authorship, "agent", "mixed", "human") {
		return fmt.Errorf("invalid answer for authorship: %s", answers.Answers.Authorship)
	}
	if !oneOf(answers.Answers.Delivery, "software", "research", "both") {
		return fmt.Errorf("invalid answer for delivery: %s", answers.Answers.Delivery)
	}
	if !oneOf(answers.Answers.Risk, "ordinary", "elevated") {
		return fmt.Errorf("invalid answer for risk: %s", answers.Answers.Risk)
	}
	if !oneOf(answers.Answers.RoutingPreference, "included-first", "balanced", "capability-first") {
		return fmt.Errorf("invalid answer for routingPreference: %s", answers.Answers.RoutingPreference)
	}
	seenClasses := make(map[string]struct{})
	for _, class := range answers.Answers.HighRiskClasses {
		if _, duplicate := seenClasses[class]; duplicate || !publicLabelPattern.MatchString(class) {
			return fmt.Errorf("highRiskClasses must be unique public labels")
		}
		seenClasses[class] = struct{}{}
	}
	if (answers.Answers.Risk == "elevated") != (len(answers.Answers.HighRiskClasses) > 0) {
		return fmt.Errorf("elevated risk requires classes; ordinary risk forbids them")
	}
	seenCapabilities := make(map[string]struct{})
	for _, capability := range answers.OperatorCapabilities {
		key := modelKey(capability.Harness, capability.Model)
		if _, duplicate := seenCapabilities[key]; duplicate {
			return fmt.Errorf("duplicate operator capability: %s/%s", capability.Harness, capability.Model)
		}
		seenCapabilities[key] = struct{}{}
		if !oneOf(capability.Entitlement, "included", "metered", "unknown") {
			return fmt.Errorf("invalid operator capability: %s/%s", capability.Harness, capability.Model)
		}
	}
	seenRules := make(map[string]struct{})
	for _, rule := range answers.ProjectRules {
		if _, duplicate := seenRules[rule.ID]; duplicate || !projectRulePattern.MatchString(rule.ID) {
			return fmt.Errorf("invalid or duplicate project rule: %s", rule.ID)
		}
		seenRules[rule.ID] = struct{}{}
	}
	return nil
}

func ResolveBindings(catalog Catalog, template ExecutionTemplate, inventory Inventory, answers AnswersFile, preference string) []Binding {
	if preference == "" {
		preference = answers.Answers.RoutingPreference
	}
	catalogModels := make(map[string]Model)
	for _, harness := range catalog.Harnesses {
		for _, model := range harness.Models {
			catalogModels[modelKey(harness.ID, model.ID)] = model
		}
	}
	availableModels := make(map[string]ModelObservation)
	for _, harness := range inventory.Harnesses {
		if harness.Version == nil {
			continue
		}
		for _, model := range harness.Models {
			if model.Provenance != "catalog-static" {
				availableModels[modelKey(harness.ID, model.ID)] = model
			}
		}
	}
	permissions := make(map[string]bool)
	for _, capability := range answers.OperatorCapabilities {
		permissions[modelKey(capability.Harness, capability.Model)] = capability.AllowUnverified
	}

	implementationFamilies := make(map[string]struct{})
	bindings := make([]Binding, 0, len(template.Roles))
	for _, role := range template.Roles {
		choices := eligibleChoices(role, preference, catalogModels, availableModels, permissions, implementationFamilies)
		if len(choices.eligible) == 0 {
			reasons := choices.rejected
			if len(reasons) == 0 {
				reasons = []string{"no catalog candidates"}
			}
			bindings = append(bindings, Binding{
				Role: role.ID, Status: "unresolved", Provenance: "unknown",
				Fallback: role.Fallback, Constraints: role.Constraints, Reasons: reasons,
			})
			continue
		}
		selected := choices.eligible[0]
		harnessID, modelID := selected.candidate.Harness, selected.candidate.Model
		family, reasoning := selected.model.ModelFamily, role.Reasoning
		bindings = append(bindings, Binding{
			Role: role.ID, Status: "resolved", Harness: &harnessID, Model: &modelID,
			ModelFamily: &family, Reasoning: &reasoning, Provenance: selected.observation.Provenance,
			Fallback: role.Fallback, Constraints: role.Constraints,
			Reasons: []string{
				fmt.Sprintf("satisfies %s quality", role.MinimumQuality),
				fmt.Sprintf("ranked by %s", preference),
				fmt.Sprintf("entitlement is %s", selected.observation.Entitlement),
			},
		})
		if strings.HasPrefix(role.ID, "implementation.") {
			implementationFamilies[family] = struct{}{}
		}
	}
	return bindings
}

type rankedChoice struct {
	candidate   Candidate
	model       Model
	observation ModelObservation
	primaryRank int
	order       int
}

type choiceSet struct {
	eligible []rankedChoice
	rejected []string
}

func eligibleChoices(
	role Role,
	preference string,
	catalogModels map[string]Model,
	availableModels map[string]ModelObservation,
	permissions map[string]bool,
	implementationFamilies map[string]struct{},
) choiceSet {
	result := choiceSet{}
	for order, candidate := range role.Candidates {
		key := modelKey(candidate.Harness, candidate.Model)
		model := catalogModels[key]
		observation, available := availableModels[key]
		reason := ""
		switch {
		case !available:
			reason = "not observed in installed inventory"
		case qualityRank[model.QualityTier] < qualityRank[role.MinimumQuality]:
			reason = fmt.Sprintf("quality tier %s is below %s", model.QualityTier, role.MinimumQuality)
		case !contains(model.ReasoningLevels, role.Reasoning):
			reason = fmt.Sprintf("reasoning level %s is unsupported", role.Reasoning)
		case !observation.Verified && !permissions[key]:
			reason = "version/model contract is unverified and not explicitly allowed"
		case role.ID == "review.formal" && hasKey(implementationFamilies, model.ModelFamily):
			reason = "model family overlaps an implementation binding"
		}
		if reason != "" {
			result.rejected = append(result.rejected, fmt.Sprintf("%s/%s: %s", candidate.Harness, candidate.Model, reason))
			continue
		}
		primaryRank := order
		switch preference {
		case "included-first":
			if observation.Entitlement != "included" {
				primaryRank = len(role.Candidates) + order
			}
		case "capability-first":
			primaryRank = -qualityRank[model.QualityTier]
		}
		result.eligible = append(result.eligible, rankedChoice{
			candidate: candidate, model: model, observation: observation,
			primaryRank: primaryRank, order: order,
		})
	}
	sort.SliceStable(result.eligible, func(left, right int) bool {
		if result.eligible[left].primaryRank != result.eligible[right].primaryRank {
			return result.eligible[left].primaryRank < result.eligible[right].primaryRank
		}
		return result.eligible[left].order < result.eligible[right].order
	})
	return result
}

func BuildPlan(target, root string, answers AnswersFile, inventory *Inventory) (BuildResult, error) {
	if err := ValidateAnswers(answers); err != nil {
		return BuildResult{}, err
	}
	target, _ = filepath.Abs(target)
	catalog, err := LoadCatalog(root)
	if err != nil {
		return BuildResult{}, err
	}
	seed, err := LoadSeed(root)
	if err != nil {
		return BuildResult{}, err
	}
	template, err := LoadTemplate(root)
	if err != nil {
		return BuildResult{}, err
	}
	facts, err := ProjectFacts(target)
	if err != nil {
		return BuildResult{}, err
	}
	if inventory == nil {
		discovered := DiscoverInventory(catalog, answers.OperatorCapabilities, target)
		inventory = &discovered
	}
	bindings := ResolveBindings(catalog, template, *inventory, answers, "")
	project := projectDocument(answers, bindings, template)
	effectiveDigest, _ := Digest(map[string]any{"seed": seed, "project": project})

	var existingAgents []byte
	agentsInputExists := fileExists(filepath.Join(target, "AGENTS.md"))
	agentsCreated := !agentsInputExists
	if agentsInputExists {
		existingAgents, err = os.ReadFile(filepath.Join(target, "AGENTS.md"))
		if err != nil {
			return BuildResult{}, err
		}
	}
	agentsInput := append([]byte(nil), existingAgents...)
	bootstrap, err := inspectBootstrap(target)
	if err != nil {
		return BuildResult{}, err
	}
	if bootstrap != nil {
		existingAgents, err = stripBootstrapAgents(existingAgents)
		if err != nil {
			return BuildResult{}, err
		}
		agentsCreated = bootstrap.receipt.AgentsCreated
	}
	if fileExists(filepath.Join(target, "SDLC.md")) && !fileExists(filepath.Join(target, ".sdlc", "adoption.json")) {
		return BuildResult{}, fmt.Errorf("refusing to replace an unmanaged root SDLC.md")
	}
	agentsBytes, err := spliceAgents(existingAgents, renderAgentsBlock(effectiveDigest))
	if err != nil {
		return BuildResult{}, err
	}
	projectBytes, _ := CanonicalBytes(project)
	sdlcBytes := renderSDLC(seed, project, effectiveDigest)
	receipt := adoptionReceipt(catalog, seed, template, projectBytes, agentsBytes, sdlcBytes, effectiveDigest, agentsCreated, bootstrap, agentsInput, agentsInputExists)
	receiptBytes, _ := CanonicalBytes(receipt)
	outputs := map[string][]byte{
		"AGENTS.md": agentsBytes, "SDLC.md": sdlcBytes,
		".sdlc/project.yaml": projectBytes, ".sdlc/adoption.json": receiptBytes,
	}
	plan := planDocument(target, catalog, seed, template, answers, facts, *inventory, bindings, outputs, bootstrap)
	return BuildResult{Plan: plan, Outputs: outputs}, nil
}

func ProjectFacts(target string) (map[string]any, error) {
	target, _ = filepath.Abs(target)
	knownPaths := []string{
		"AGENTS.md", BootstrapFilename, "SDLC.md", BootstrapReceiptPath, ".sdlc/project.yaml", ".sdlc/adoption.json", ".tasks", ".github/workflows",
		"go.mod", "Cargo.toml", "package.json", "pyproject.toml", "requirements.txt", "Makefile", "visual-diff.json",
	}
	paths := make(map[string]bool, len(knownPaths))
	for _, path := range knownPaths {
		_, err := os.Stat(filepath.Join(target, path))
		paths[path] = err == nil
	}
	gitWorktree := pathExists(filepath.Join(target, ".git"))
	var head any
	if gitWorktree {
		command := exec.Command("git", "rev-parse", "HEAD")
		command.Dir = target
		if output, err := command.Output(); err == nil {
			candidate := strings.TrimSpace(string(output))
			if gitRevisionPattern.MatchString(candidate) {
				head = candidate
			}
		}
	}
	managedBlocks := 0
	if fileExists(filepath.Join(target, "AGENTS.md")) {
		content, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
		if err != nil {
			return nil, fmt.Errorf("cannot preserve AGENTS.md as UTF-8: %w", err)
		}
		managedBlocks = strings.Count(string(content), BlockBegin)
	}
	return map[string]any{
		"gitWorktree": gitWorktree, "head": head, "hasCommit": head != nil,
		"paths": paths, "managedAgentBlocks": managedBlocks,
	}, nil
}

func projectDocument(answers AnswersFile, bindings []Binding, template ExecutionTemplate) map[string]any {
	return map[string]any{
		"schemaVersion":     1,
		"seed":              answers.Seed,
		"executionTemplate": answers.ExecutionTemplate,
		"answers":           answers.Answers,
		"projectRules":      answers.ProjectRules,
		"execution":         map[string]any{"roles": bindings, "escalation": template.Escalation},
	}
}

func adoptionReceipt(
	catalog Catalog,
	seed Seed,
	template ExecutionTemplate,
	projectBytes, agentsBytes, sdlcBytes []byte,
	effectiveDigest string,
	agentsCreated bool,
	bootstrap *bootstrapState,
	agentsInput []byte,
	agentsInputExists bool,
) map[string]any {
	catalogDigest, _ := Digest(catalog)
	seedDigest, _ := Digest(seed)
	templateDigest, _ := Digest(template)
	receipt := map[string]any{
		"schemaVersion": 1, "renderer": RendererVersion, "catalog": catalog.Catalog,
		"seed": seed.ID, "executionTemplate": template.ID,
		"inputs": map[string]any{
			"catalog": catalogDigest, "seed": seedDigest, "executionTemplate": templateDigest,
			"project": DigestBytes(projectBytes), "effectivePolicy": effectiveDigest,
			"agents": DigestBytes(agentsInput), "agentsExisted": agentsInputExists,
		},
		"outputs": map[string]any{
			"AGENTS.md": DigestBytes(agentsBytes), "SDLC.md": DigestBytes(sdlcBytes),
			".sdlc/project.yaml": DigestBytes(projectBytes),
		},
		"agentsCreated": agentsCreated,
		"warnings":      []string{IntegrityWarning, ExecutionWarning},
	}
	if bootstrap != nil {
		receipt["inputs"].(map[string]any)["bootstrap"] = bootstrap.receiptDigest
	}
	return receipt
}

func planDocument(
	target string,
	catalog Catalog,
	seed Seed,
	template ExecutionTemplate,
	answers AnswersFile,
	facts map[string]any,
	inventory Inventory,
	bindings []Binding,
	outputs map[string][]byte,
	bootstrap *bootstrapState,
) map[string]any {
	files := make([]map[string]any, 0, len(outputs)+2)
	for _, path := range []string{"AGENTS.md", "SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		existing := pathExists(filepath.Join(target, path))
		action, rollback := "create", "remove"
		if existing {
			action, rollback = "replace", "restore"
		}
		if path == "AGENTS.md" && existing {
			action = "update-managed-block"
		}
		files = append(files, map[string]any{
			"path": path, "action": action, "mode": "0644",
			"contentDigest": DigestBytes(outputs[path]), "rollback": rollback,
		})
	}
	if bootstrap != nil {
		files = append(files,
			map[string]any{
				"path": BootstrapFilename, "action": "consume-bootstrap", "mode": "0644",
				"contentDigest": bootstrap.receipt.Outputs[BootstrapFilename], "rollback": "restore",
			},
			map[string]any{
				"path": BootstrapReceiptPath, "action": "consume-bootstrap-receipt", "mode": "0644",
				"contentDigest": bootstrap.receiptDigest, "rollback": "restore",
			},
		)
	}
	ruleIDs := make([]string, 0, len(seed.Rules)+len(answers.ProjectRules))
	for _, rule := range seed.Rules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	for _, rule := range answers.ProjectRules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	unresolved := make([]string, 0)
	for _, binding := range bindings {
		if binding.Status == "unresolved" {
			unresolved = append(unresolved, binding.Role)
		}
	}
	recommendation := map[string]any{
		"seed": seed.ID, "executionTemplate": template.ID, "ruleIds": ruleIDs,
		"unresolved": unresolved, "bindings": bindings, "escalateAfterReviewRounds": 2,
	}
	alternatives := buildAlternatives(catalog, template, inventory, answers, bindings)
	catalogDigest, _ := Digest(catalog)
	seedDigest, _ := Digest(seed)
	templateDigest, _ := Digest(template)
	answersDigest, _ := Digest(answers)
	factsDigest, _ := Digest(facts)
	inventoryDigest, _ := Digest(inventory)
	return map[string]any{
		"schemaVersion": 1,
		"inputs": map[string]any{
			"catalog": catalog.Catalog, "catalogDigest": catalogDigest,
			"seed": seed.ID, "seedDigest": seedDigest,
			"executionTemplate": template.ID, "executionTemplateDigest": templateDigest,
			"answersDigest": answersDigest, "projectFactsDigest": factsDigest, "inventoryDigest": inventoryDigest,
		},
		"causes": buildCauses(facts, answers), "recommendation": recommendation,
		"alternatives": alternatives, "files": files,
		"warnings": []string{IntegrityWarning, ExecutionWarning},
	}
}

func buildAlternatives(catalog Catalog, template ExecutionTemplate, inventory Inventory, answers AnswersFile, original []Binding) []map[string]any {
	result := make([]map[string]any, 0, 2)
	for _, preference := range []string{"included-first", "capability-first"} {
		if preference == answers.Answers.RoutingPreference {
			continue
		}
		alternative := ResolveBindings(catalog, template, inventory, answers, preference)
		changed := make([]Binding, 0)
		for index, binding := range alternative {
			if !sameStringPointer(binding.Harness, original[index].Harness) || !sameStringPointer(binding.Model, original[index].Model) {
				changed = append(changed, binding)
			}
		}
		if len(changed) > 0 {
			result = append(result, map[string]any{
				"id":              preference,
				"tradeoff":        fmt.Sprintf("Rank eligible routes using %s instead of %s.", preference, answers.Answers.RoutingPreference),
				"changedBindings": changed,
			})
		}
		if len(result) == 2 {
			break
		}
	}
	return result
}

func buildCauses(facts map[string]any, answers AnswersFile) []map[string]any {
	causes := []map[string]any{
		{"source": "observed", "key": "project.gitWorktree", "value": facts["gitWorktree"]},
		{"source": "observed", "key": "project.hasCommit", "value": facts["hasCommit"]},
		{"source": "observed", "key": "project.managedAgentBlocks", "value": facts["managedAgentBlocks"]},
	}
	paths := facts["paths"].(map[string]bool)
	pathNames := make([]string, 0, len(paths))
	for name := range paths {
		pathNames = append(pathNames, name)
	}
	sort.Strings(pathNames)
	for _, name := range pathNames {
		if paths[name] {
			causes = append(causes, map[string]any{"source": "observed", "key": "project.path." + name, "value": true})
		}
	}
	answerValues := map[string]any{
		"authorship":        answers.Answers.Authorship,
		"delivery":          answers.Answers.Delivery,
		"highRiskClasses":   strings.Join(answers.Answers.HighRiskClasses, ","),
		"risk":              answers.Answers.Risk,
		"routingPreference": answers.Answers.RoutingPreference,
	}
	answerNames := make([]string, 0, len(answerValues))
	for name := range answerValues {
		answerNames = append(answerNames, name)
	}
	sort.Strings(answerNames)
	for _, name := range answerNames {
		causes = append(causes, map[string]any{"source": "operator-declared", "key": "answers." + name, "value": answerValues[name]})
	}
	return causes
}

func renderAgentsBlock(effectiveDigest string) string {
	return strings.Join([]string{
		BlockBegin,
		"# Noise SDLC bootstrap (generated)",
		"",
		"Before changing this repository, load root `SDLC.md` and verify its end marker and `.sdlc/adoption.json` against this policy digest:",
		"",
		"`" + effectiveDigest + "`",
		"",
		"Accepted requirements and ADRs outrank generated policy; generated `SDLC.md` outranks project-local agent prose for lifecycle rules.",
		"If lifecycle authority or integrity cannot be verified, allow read-only inspection and remediation planning only. Block edits, approvals, merges, publication, spend, deployment, and credential handling.",
		"Collection integrity is not conformance, review approval, security evidence, runtime acceptance, or merge readiness. Execution bindings do not launch providers or authorize spend.",
		BlockEnd,
	}, "\n")
}

func spliceAgents(existing []byte, block string) ([]byte, error) {
	if existing == nil {
		return []byte(block + "\n"), nil
	}
	text := string(existing)
	begins, ends := strings.Count(text, BlockBegin), strings.Count(text, BlockEnd)
	if !((begins == 0 && ends == 0) || (begins == 1 && ends == 1)) {
		return nil, fmt.Errorf("AGENTS.md has malformed or duplicate managed markers")
	}
	remainder := text
	if begins == 1 {
		start := strings.Index(text, BlockBegin)
		finish := strings.Index(text[start:], BlockEnd) + start + len(BlockEnd)
		if start == 0 {
			remainder = strings.TrimLeft(text[finish:], "\n")
		} else {
			remainder = text[:start] + text[finish:]
		}
	}
	return []byte(block + "\n\n" + strings.TrimLeft(remainder, "\n")), nil
}

func renderSDLC(seed Seed, project map[string]any, effectiveDigest string) []byte {
	answers := project["answers"].(Answers)
	rules := append([]Rule{}, seed.Rules...)
	rules = append(rules, project["projectRules"].([]Rule)...)
	lines := []string{
		"# Effective software-delivery lifecycle", "",
		"> Generated by noise sdlc. Edit `.sdlc/project.yaml` through the planner; do not edit this file.",
		fmt.Sprintf("> Seed: `%s` · Effective policy: `%s`", seed.ID, effectiveDigest), "",
		"## Authority", "",
		"Accepted requirements and ADRs outrank this generated projection. Project additions may strengthen or specialize the seed but may not delete or weaken it.", "",
		"## Lifecycle rules", "",
	}
	for _, rule := range rules {
		applicability := rule.Applicability
		if answers.Authorship == "human" && applicability == "agent-authored" {
			applicability += "; dormant until agent-authored work occurs"
		}
		owner := ""
		if rule.Owner != nil {
			owner = fmt.Sprintf(" Owner: `%s` / `%s`.", *rule.Owner, pointerValue(rule.Contract))
		}
		lines = append(lines,
			fmt.Sprintf("### `%s` — %s", rule.ID, rule.Title), "", rule.Requirement, "",
			fmt.Sprintf("Assurance: **%s**. Applicability: **%s**.%s", rule.Assurance, applicability, owner), "",
		)
	}
	lines = append(lines, "## Execution routing", "", "| Role | Binding | Reasoning | Fallback |", "|---|---|---|---|")
	execution := project["execution"].(map[string]any)
	unresolved := make([]string, 0)
	for _, binding := range execution["roles"].([]Binding) {
		chosen := "**unresolved**"
		if binding.Status == "resolved" {
			chosen = fmt.Sprintf("`%s` / `%s` (%s)", pointerValue(binding.Harness), pointerValue(binding.Model), binding.Provenance)
		} else {
			unresolved = append(unresolved, binding.Role)
		}
		reasoning, fallback := pointerValue(binding.Reasoning), pointerValue(binding.Fallback)
		if reasoning == "" {
			reasoning = "-"
		}
		if fallback == "" {
			fallback = "-"
		}
		lines = append(lines, fmt.Sprintf("| `%s` | %s | `%s` | `%s` |", binding.Role, chosen, reasoning, fallback))
	}
	escalation := execution["escalation"].(Escalation)
	unresolvedText := "none"
	if len(unresolved) > 0 {
		for index := range unresolved {
			unresolved[index] = "`" + unresolved[index] + "`"
		}
		unresolvedText = strings.Join(unresolved, ", ")
	}
	lines = append(lines,
		"", "For selected invocation and review contracts, run `noise sdlc execution show --target .`.",
		"Work ownership remains in `noise tasks`. Live guidance does not grant execution authority.",
		"", "Formal review must remain model-family independent from the implementation route and must bind the exact candidate revision.",
		fmt.Sprintf("Repair escalates after **%d** failed review rounds to `%s`.", escalation.AfterReviewRounds, escalation.NextRole),
		"", "Unresolved roles: "+unresolvedText, "", "## Critical safety kernel", "",
		"- If this file, its end marker, or the adoption receipt cannot be verified, stop after read-only inspection and remediation planning.",
		"- Do not edit, approve, merge, publish, spend, deploy, or handle credentials while lifecycle authority is unresolved.",
		"- Collection integrity is not conformance, review approval, security evidence, runtime acceptance, or merge readiness.",
		"- The execution matrix is routing policy only. It does not launch a provider, authenticate, or authorize spend.",
		"", SDLCEndPrefix+strings.TrimPrefix(effectiveDigest, "sha256:")+" -->", "",
	)
	return []byte(strings.Join(lines, "\n"))
}

func Adopt(target string, outputs map[string][]byte) error {
	return adoptWithHook(target, outputs, nil)
}

func adoptWithHook(target string, outputs map[string][]byte, hook transactionHook) error {
	target, _ = filepath.Abs(target)
	ordered := []string{"AGENTS.md", "SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"}
	for _, relative := range ordered {
		if err := validateManagedPath(target, relative); err != nil {
			return err
		}
	}
	expectedBootstrap, err := expectedBootstrapPlanFrom(outputs)
	if err != nil {
		return err
	}
	bootstrap, err := inspectBootstrap(target)
	if err != nil {
		return err
	}
	if expectedBootstrap.receiptDigest != "" && (bootstrap == nil || bootstrap.receiptDigest != expectedBootstrap.receiptDigest) {
		return fmt.Errorf("bootstrap state changed after planning; generate a new plan")
	}
	if expectedBootstrap.receiptDigest == "" && bootstrap != nil {
		return fmt.Errorf("bootstrap state was added after planning; generate a new plan")
	}
	cleanup := []string{}
	if bootstrap != nil {
		cleanup = []string{BootstrapFilename, BootstrapReceiptPath}
	}
	for _, relative := range cleanup {
		if err := validateManagedPath(target, relative); err != nil {
			return err
		}
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return fmt.Errorf("cannot anchor adoption to project root: %w", err)
	}
	defer root.Close()
	guardedPaths := append(append([]string{}, ordered...), cleanup...)
	snapshots, err := snapshotManagedPaths(root, guardedPaths)
	if err != nil {
		return err
	}
	if snapshots["AGENTS.md"].exists != expectedBootstrap.agentsExisted || DigestBytes(snapshots["AGENTS.md"].content) != expectedBootstrap.agentsDigest {
		return fmt.Errorf("AGENTS.md changed after planning; generate a new plan")
	}
	if bootstrap != nil {
		if DigestBytes(snapshots[BootstrapFilename].content) != bootstrap.receipt.Outputs[BootstrapFilename] || DigestBytes(snapshots[BootstrapReceiptPath].content) != bootstrap.receiptDigest {
			return fmt.Errorf("bootstrap state changed after planning; generate a new plan")
		}
	}
	mutations := make([]managedMutation, 0, len(guardedPaths))
	installBeforeReceipt := ordered[:len(ordered)-1]
	for _, relative := range installBeforeReceipt {
		if err := ensureManagedSnapshots(root, snapshots); err != nil {
			return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back after concurrent change: %w", err))
		}
		mutation, err := writeManagedPathGuarded(root, target, relative, outputs[relative], snapshots[relative], hook)
		if err != nil {
			return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back after installation failure: %w", err))
		}
		mutations = append(mutations, mutation)
		snapshots[relative] = mutation.after
	}
	for _, relative := range cleanup {
		if err := ensureManagedSnapshots(root, snapshots); err != nil {
			return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back after concurrent change: %w", err))
		}
		mutation, err := removeManagedPathGuarded(root, relative, snapshots[relative], hook)
		if err != nil {
			return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back after bootstrap cleanup failure: %w", err))
		}
		mutations = append(mutations, mutation)
		snapshots[relative] = mutation.after
	}
	receiptPath := ordered[len(ordered)-1]
	if err := ensureManagedSnapshots(root, snapshots); err != nil {
		return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back after concurrent change: %w", err))
	}
	mutation, err := writeManagedPathGuarded(root, target, receiptPath, outputs[receiptPath], snapshots[receiptPath], hook)
	if err != nil {
		return transactionFailure(root, mutations, fmt.Errorf("adoption rolled back before commit marker: %w", err))
	}
	mutations = append(mutations, mutation)
	snapshots[receiptPath] = mutation.after
	return finalizeManagedMutations(root, mutations)
}

type expectedBootstrapPlan struct {
	receiptDigest string
	agentsDigest  string
	agentsExisted bool
}

func expectedBootstrapPlanFrom(outputs map[string][]byte) (expectedBootstrapPlan, error) {
	var receipt map[string]any
	if err := json.Unmarshal(outputs[".sdlc/adoption.json"], &receipt); err != nil {
		return expectedBootstrapPlan{}, fmt.Errorf("invalid planned adoption receipt: %w", err)
	}
	inputs, ok := receipt["inputs"].(map[string]any)
	if !ok {
		return expectedBootstrapPlan{}, fmt.Errorf("invalid planned adoption receipt: inputs must be an object")
	}
	agentsExisted, ok := inputs["agentsExisted"].(bool)
	if !ok {
		return expectedBootstrapPlan{}, fmt.Errorf("invalid planned adoption receipt: agentsExisted must be boolean")
	}
	return expectedBootstrapPlan{
		receiptDigest: stringValue(inputs["bootstrap"]),
		agentsDigest:  stringValue(inputs["agents"]),
		agentsExisted: agentsExisted,
	}, nil
}

func Verify(target string) (map[string]any, error) {
	target, _ = filepath.Abs(target)
	return verifyWithReader(target, func(relative string) ([]byte, error) {
		return os.ReadFile(filepath.Join(target, relative))
	})
}

func verifyWithReader(target string, read func(string) ([]byte, error)) (map[string]any, error) {
	target, _ = filepath.Abs(target)
	for _, relative := range []string{"AGENTS.md", "SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if err := validateManagedPath(target, relative); err != nil {
			return nil, err
		}
	}
	receiptPath := filepath.Join(target, ".sdlc", "adoption.json")
	projectPath := filepath.Join(target, ".sdlc", "project.yaml")
	if !fileExists(receiptPath) || !fileExists(projectPath) {
		return nil, fmt.Errorf("missing root adoption receipt or project input")
	}
	var receipt map[string]any
	receiptBytes, err := read(".sdlc/adoption.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		return nil, fmt.Errorf("cannot read valid JSON from %s: %w", receiptPath, err)
	}
	if intValue(receipt["schemaVersion"]) != 1 || stringValue(receipt["renderer"]) != RendererVersion {
		return nil, fmt.Errorf("unsupported adoption receipt")
	}
	outputs, ok := receipt["outputs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unsupported adoption receipt: outputs must be an object")
	}
	for _, relative := range []string{".sdlc/project.yaml", "AGENTS.md", "SDLC.md"} {
		path := filepath.Join(target, relative)
		if !fileExists(path) {
			return nil, fmt.Errorf("missing managed output: %s", relative)
		}
		content, err := read(relative)
		if err != nil {
			return nil, err
		}
		if DigestBytes(content) != stringValue(outputs[relative]) {
			return nil, fmt.Errorf("managed output drift: %s", relative)
		}
	}
	agents, err := read("AGENTS.md")
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(agents), BlockBegin) || strings.Count(string(agents), BlockBegin) != 1 || strings.Count(string(agents), BlockEnd) != 1 {
		return nil, fmt.Errorf("AGENTS.md must contain exactly one byte-zero managed block")
	}
	inputs, ok := receipt["inputs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unsupported adoption receipt: inputs must be an object")
	}
	effective := stringValue(inputs["effectivePolicy"])
	if effective == "" {
		return nil, fmt.Errorf("unsupported adoption receipt: effective policy is missing")
	}
	if !strings.Contains(string(agents), effective) {
		return nil, fmt.Errorf("AGENTS.md policy digest does not match receipt")
	}
	sdlc, err := read("SDLC.md")
	if err != nil {
		return nil, err
	}
	marker := SDLCEndPrefix + strings.TrimPrefix(effective, "sha256:") + " -->\n"
	if !strings.HasSuffix(string(sdlc), marker) || strings.Count(string(sdlc), SDLCEndPrefix) != 1 {
		return nil, fmt.Errorf("SDLC.md end marker does not match receipt")
	}
	if shadows := shadowArtifacts(target); len(shadows) > 0 {
		return nil, fmt.Errorf("shadow lifecycle artifacts: %s", strings.Join(shadows, ", "))
	}
	return map[string]any{
		"integrity": "verified", "seed": receipt["seed"], "catalog": receipt["catalog"],
		"executionTemplate": receipt["executionTemplate"], "warnings": receipt["warnings"],
	}, nil
}

func OnboardingStatus(target string) map[string]any {
	target, _ = filepath.Abs(target)
	bootstrap, bootstrapErr := inspectBootstrap(target)
	if bootstrapErr != nil {
		return onboardingResult("repair-required", "invalid-bootstrap", false)
	}
	managedBlock := false
	if fileExists(filepath.Join(target, "AGENTS.md")) {
		content, err := os.ReadFile(filepath.Join(target, "AGENTS.md"))
		if err != nil {
			return onboardingResult("repair-required", "invalid-agents-file", false)
		}
		managedBlock = strings.Contains(string(content), BlockBegin)
	}
	hasAuthority := pathExists(filepath.Join(target, ".sdlc", "adoption.json")) || pathExists(filepath.Join(target, ".sdlc", "project.yaml"))
	hasSDLC := pathExists(filepath.Join(target, "SDLC.md"))
	if !managedBlock && !hasAuthority && !hasSDLC {
		if bootstrap != nil {
			return onboardingResult("offer-selection", "bootstrap-pending", true)
		}
		return onboardingResult("offer-selection", "no-adoption", true)
	}
	if hasSDLC && !managedBlock && !hasAuthority {
		return onboardingResult("repair-required", "unmanaged-sdlc-collision", false)
	}
	verified, err := Verify(target)
	if err != nil {
		return onboardingResult("repair-required", repairReason(err.Error()), false)
	}
	result := onboardingResult("already-onboarded", "verified-adoption", false)
	result["adoption"] = map[string]any{
		"seed": verified["seed"], "catalog": verified["catalog"], "executionTemplate": verified["executionTemplate"],
	}
	return result
}

func RemoveAdoption(target string) error {
	target, _ = filepath.Abs(target)
	if _, err := Verify(target); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return fmt.Errorf("cannot anchor removal to project root: %w", err)
	}
	defer root.Close()
	var receipt map[string]any
	if err := LoadFlexibleJSON(filepath.Join(target, ".sdlc", "adoption.json"), &receipt); err != nil {
		return err
	}
	agents, err := root.ReadFile("AGENTS.md")
	if err != nil {
		return err
	}
	text := string(agents)
	start := strings.Index(text, BlockBegin)
	finish := strings.Index(text[start:], BlockEnd) + start + len(BlockEnd)
	remainder := strings.TrimLeft(text[:start]+text[finish:], "\n")
	if created, _ := receipt["agentsCreated"].(bool); created && strings.TrimSpace(remainder) == "" {
		if err := root.Remove("AGENTS.md"); err != nil {
			return err
		}
	} else if err := writeManagedPath(root, target, "AGENTS.md", []byte(remainder)); err != nil {
		return err
	}
	for _, relative := range []string{"SDLC.md", ".sdlc/project.yaml", ".sdlc/adoption.json"} {
		if err := validateManagedPath(target, relative); err != nil {
			return err
		}
		if err := root.Remove(relative); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = root.Remove(".sdlc")
	return nil
}

func writeManagedPath(root *os.Root, rootPath, relative string, content []byte) error {
	return writeManagedPathWithHook(root, rootPath, relative, content, nil)
}

func writeManagedPathWithHook(root *os.Root, rootPath, relative string, content []byte, beforeRename func()) error {
	expected, err := snapshotManagedPath(root, relative)
	if err != nil {
		return err
	}
	var hook transactionHook
	if beforeRename != nil {
		hook = func(_, _ string) error {
			beforeRename()
			return nil
		}
	}
	mutation, err := writeManagedPathGuarded(root, rootPath, relative, content, expected, hook)
	if err != nil {
		return err
	}
	return finalizeManagedMutations(root, []managedMutation{mutation})
}

func writeManagedPathGuarded(root *os.Root, rootPath, relative string, content []byte, expected managedPathSnapshot, hook transactionHook) (managedMutation, error) {
	if err := validateManagedPath(rootPath, relative); err != nil {
		return managedMutation{}, err
	}
	parent := filepath.Dir(relative)
	if parent != "." {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return managedMutation{}, err
		}
	}
	if err := validateManagedPath(rootPath, relative); err != nil {
		return managedMutation{}, err
	}
	temporary, err := randomManagedSibling(relative, "new")
	if err != nil {
		return managedMutation{}, err
	}
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return managedMutation{}, err
	}
	defer root.Remove(temporary)
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return managedMutation{}, err
	}
	if err := root.Chmod(temporary, 0o644); err != nil {
		return managedMutation{}, err
	}
	staged, err := snapshotManagedPath(root, temporary)
	if err != nil {
		return managedMutation{}, err
	}
	backup := ""
	if expected.exists {
		backup, err = moveManagedPathExact(root, relative, expected, hook, "before-write")
	} else {
		if hook != nil {
			err = hook("before-write", relative)
		}
		if err == nil {
			err = root.Link(temporary, relative)
		}
	}
	if err != nil {
		return managedMutation{}, err
	}
	if expected.exists {
		err = root.Link(temporary, relative)
	}
	if err != nil {
		if backup != "" {
			if restoreErr := restoreManagedBackup(root, backup, relative, expected); restoreErr != nil {
				return managedMutation{}, fmt.Errorf("install failed: %v; original retained at %s: %w", err, backup, restoreErr)
			}
		}
		return managedMutation{}, err
	}
	after, err := snapshotManagedPath(root, relative)
	if err != nil {
		return managedMutation{}, fmt.Errorf("cannot verify installed %s%s: %w", relative, originalRetention(backup), err)
	}
	if !managedSnapshotsEqual(after, staged) {
		return managedMutation{}, fmt.Errorf("installed path changed at atomic boundary: %s%s", relative, originalRetention(backup))
	}
	return managedMutation{path: relative, backup: backup, before: expected, after: after}, nil
}

func originalRetention(backup string) string {
	if backup == "" {
		return ""
	}
	return "; original retained at " + backup
}

func removeManagedPathGuarded(root *os.Root, relative string, expected managedPathSnapshot, hook transactionHook) (managedMutation, error) {
	backup, err := moveManagedPathExact(root, relative, expected, hook, "before-remove")
	if err != nil {
		return managedMutation{}, err
	}
	return managedMutation{path: relative, backup: backup, before: expected, after: managedPathSnapshot{}}, nil
}

func moveManagedPathExact(root *os.Root, relative string, expected managedPathSnapshot, hook transactionHook, stage string) (string, error) {
	if !expected.exists {
		return "", fmt.Errorf("cannot move absent managed path: %s", relative)
	}
	if hook != nil {
		if err := hook(stage, relative); err != nil {
			return "", err
		}
	}
	backup, err := randomManagedSibling(relative, "backup")
	if err != nil {
		return "", err
	}
	if err := root.Rename(relative, backup); err != nil {
		return "", err
	}
	moved, inspectErr := snapshotManagedPath(root, backup)
	if inspectErr == nil && managedSnapshotsEqual(moved, expected) {
		return backup, nil
	}
	if restoreErr := restoreManagedBackup(root, backup, relative, moved); restoreErr != nil {
		return "", fmt.Errorf("managed path changed at atomic boundary: %s; captured object retained at %s: %v", relative, backup, restoreErr)
	}
	if inspectErr != nil {
		return "", inspectErr
	}
	return "", fmt.Errorf("managed path changed at atomic boundary: %s", relative)
}

func randomManagedSibling(relative, label string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(relative), ".noise-sdlc-"+label+"-"+hex.EncodeToString(random)), nil
}

func finalizeManagedMutations(root *os.Root, mutations []managedMutation) error {
	for _, mutation := range mutations {
		if mutation.backup != "" {
			discard, err := moveManagedPathExact(root, mutation.backup, mutation.before, nil, "")
			if err != nil {
				return fmt.Errorf("cannot remove transaction backup %s: %w", mutation.backup, err)
			}
			if err := root.Remove(discard); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cannot remove captured transaction backup %s: %w", discard, err)
			}
		}
	}
	return nil
}

func rollbackManagedMutations(root *os.Root, mutations []managedMutation) error {
	conflicts := make([]string, 0)
	for index := len(mutations) - 1; index >= 0; index-- {
		mutation := mutations[index]
		current, err := snapshotManagedPath(root, mutation.path)
		if err != nil || !managedSnapshotsEqual(current, mutation.after) {
			conflicts = append(conflicts, rollbackConflictDescription(mutation))
			continue
		}
		discard := ""
		if mutation.after.exists {
			discard, err = moveManagedPathExact(root, mutation.path, mutation.after, nil, "")
			if err != nil {
				conflicts = append(conflicts, rollbackConflictDescription(mutation))
				continue
			}
		}
		if mutation.before.exists {
			if err := restoreManagedBackup(root, mutation.backup, mutation.path, mutation.before); err != nil {
				conflicts = append(conflicts, rollbackConflictDescription(mutation))
				continue
			}
			mutations[index].backup = ""
		}
		if discard != "" {
			_ = root.Remove(discard)
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("rollback preserved concurrent changes at: %s; transaction backups were retained", strings.Join(conflicts, ", "))
	}
	return finalizeManagedMutations(root, mutations)
}

func restoreManagedBackup(root *os.Root, backup, relative string, expected managedPathSnapshot) error {
	if err := root.Link(backup, relative); err != nil {
		return err
	}
	discard, err := moveManagedPathExact(root, backup, expected, nil, "")
	if err != nil {
		return err
	}
	if err := root.Remove(discard); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func rollbackConflictDescription(mutation managedMutation) string {
	if mutation.backup != "" {
		return mutation.path + " (original retained at " + mutation.backup + ")"
	}
	return mutation.path
}

func transactionFailure(root *os.Root, mutations []managedMutation, cause error) error {
	if rollbackErr := rollbackManagedMutations(root, mutations); rollbackErr != nil {
		return fmt.Errorf("%w; %v", cause, rollbackErr)
	}
	return cause
}

func validateManagedPath(root, relative string) error {
	clean := filepath.Clean(relative)
	if filepath.IsAbs(relative) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed path escapes project root: %s", relative)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("cannot inspect project root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("project root must be a real directory")
	}
	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("cannot inspect managed path %s: %w", relative, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed path contains symbolic link: %s", relative)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("managed path parent is not a directory: %s", relative)
		}
		if index == len(parts)-1 && info.IsDir() {
			return fmt.Errorf("managed file path is a directory: %s", relative)
		}
	}
	return nil
}

func onboardingResult(state, reason string, offer bool) map[string]any {
	return map[string]any{
		"schemaVersion": 1, "state": state, "reason": reason,
		"offerSelection": offer, "dismissal": "deck-project-state",
	}
}

func repairReason(message string) string {
	mappings := []struct{ fragment, reason string }{
		{"managed output drift", "managed-output-drift"}, {"missing managed output", "missing-managed-output"},
		{"missing root adoption receipt", "missing-authority-input"}, {"unsupported adoption receipt", "unsupported-receipt"},
		{"AGENTS.md", "invalid-managed-bootstrap"}, {"SDLC.md end marker", "invalid-sdlc-marker"},
		{"shadow lifecycle artifacts", "shadow-artifacts"},
	}
	for _, mapping := range mappings {
		if strings.Contains(message, mapping.fragment) {
			return mapping.reason
		}
	}
	return "invalid-adoption"
}

func shadowArtifacts(target string) []string {
	var shadows []string
	_ = filepath.WalkDir(target, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(target, path)
		relative = filepath.ToSlash(relative)
		if (entry.Name() == "SDLC.md" && relative != "SDLC.md") || (entry.Name() == "adoption.json" && relative != ".sdlc/adoption.json") {
			shadows = append(shadows, relative)
		}
		return nil
	})
	sort.Strings(shadows)
	return shadows
}

func oneOf(value string, choices ...string) bool { return contains(choices, value) }
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func hasKey(values map[string]struct{}, key string) bool { _, found := values[key]; return found }
func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func sameStringPointer(left, right *string) bool { return pointerValue(left) == pointerValue(right) }
func pathExists(path string) bool                { _, err := os.Stat(path); return err == nil }
func stringValue(value any) string               { result, _ := value.(string); return result }
func intValue(value any) int                     { number, _ := value.(float64); return int(number) }
