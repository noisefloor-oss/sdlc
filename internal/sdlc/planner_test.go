package sdlc

import (
	"strings"
	"testing"
)

func TestResolveBindingsKeepsFormalReviewIndependent(t *testing.T) {
	catalog := Catalog{Harnesses: []Harness{
		resolverHarness("bulk", resolverModel("bulk-model", "family-a", "economy", "included")),
		resolverHarness("standard", resolverModel("standard-model", "family-b", "balanced", "included")),
		resolverHarness("high", resolverModel("high-model", "family-c", "frontier", "metered")),
		resolverHarness("review-overlap", resolverModel("overlap-model", "family-c", "frontier", "included")),
		resolverHarness("review-independent", resolverModel("review-model", "family-review", "frontier", "metered")),
	}}
	template := resolverTemplate([]Role{
		resolverRole("implementation.bulk", "economy", "bulk", "bulk-model"),
		resolverRole("implementation.standard", "balanced", "standard", "standard-model"),
		resolverRole("implementation.high", "frontier", "high", "high-model"),
		{
			ID: "review.formal", MinimumQuality: "frontier", Reasoning: "high",
			Candidates: []Candidate{{Harness: "review-overlap", Model: "overlap-model"}, {Harness: "review-independent", Model: "review-model"}},
		},
		resolverRole("repair", "frontier", "high", "high-model"),
	})
	inventory := resolverInventory(catalog)
	answers := resolverAnswers(catalog)

	bindings := ResolveBindings(catalog, template, inventory, answers, "balanced")
	formal := bindings[3]
	if formal.Status != "resolved" || formal.Harness == nil || *formal.Harness != "review-independent" ||
		formal.ModelFamily == nil || *formal.ModelFamily != "family-review" {
		t.Fatalf("formal review was not independent: %#v", formal)
	}
	if !contains(formal.Reasons, "satisfies frontier quality") {
		t.Fatalf("formal review lacks an eligibility explanation: %#v", formal.Reasons)
	}
}

func TestResolveBindingsPreservesFallbackAndRejectionReasons(t *testing.T) {
	model := resolverModel("candidate", "family-a", "economy", "included")
	catalog := Catalog{Harnesses: []Harness{resolverHarness("missing", model)}}
	template := resolverTemplate([]Role{
		{
			ID: "implementation.bulk", MinimumQuality: "economy", Reasoning: "high",
			Candidates: []Candidate{{Harness: "missing", Model: "candidate"}}, Fallback: stringPointer("implementation.standard"),
		},
		{ID: "implementation.standard"}, {ID: "implementation.high"}, {ID: "review.formal"}, {ID: "repair"},
	})

	binding := ResolveBindings(catalog, template, Inventory{}, resolverAnswers(catalog), "balanced")[0]
	if binding.Status != "unresolved" || binding.Fallback == nil || *binding.Fallback != "implementation.standard" {
		t.Fatalf("unresolved binding lost its fallback: %#v", binding)
	}
	if len(binding.Reasons) != 1 || !strings.Contains(binding.Reasons[0], "not observed") {
		t.Fatalf("unresolved binding lacks the rejection reason: %#v", binding.Reasons)
	}
}

func TestEligibleChoicesHonorsRankingPreferences(t *testing.T) {
	economy := resolverModel("economy", "family-economy", "economy", "included")
	frontier := resolverModel("frontier", "family-frontier", "frontier", "metered")
	role := Role{
		ID: "implementation.bulk", MinimumQuality: "economy", Reasoning: "high",
		Candidates: []Candidate{{Harness: "metered", Model: "frontier"}, {Harness: "included", Model: "economy"}},
	}
	models := map[string]Model{
		modelKey("metered", "frontier"): frontier,
		modelKey("included", "economy"): economy,
	}
	observed := map[string]ModelObservation{
		modelKey("metered", "frontier"): resolverObservation(frontier, "metered"),
		modelKey("included", "economy"): resolverObservation(economy, "included"),
	}
	permissions := map[string]bool{
		modelKey("metered", "frontier"): true,
		modelKey("included", "economy"): true,
	}

	tests := []struct {
		preference string
		want       string
	}{
		{preference: "balanced", want: "frontier"},
		{preference: "included-first", want: "economy"},
		{preference: "capability-first", want: "frontier"},
	}
	for _, test := range tests {
		t.Run(test.preference, func(t *testing.T) {
			choices := eligibleChoices(role, test.preference, models, observed, permissions, map[string]struct{}{})
			if len(choices.eligible) != 2 || choices.eligible[0].candidate.Model != test.want {
				t.Fatalf("%s ranked %#v", test.preference, choices.eligible)
			}
		})
	}
}

func TestValidateTemplateRequiresTwoRoundEscalation(t *testing.T) {
	template := resolverTemplate([]Role{
		{ID: "implementation.bulk"}, {ID: "implementation.standard"}, {ID: "implementation.high"}, {ID: "review.formal"}, {ID: "repair"},
	})
	if err := ValidateTemplate(template, Catalog{}); err != nil {
		t.Fatalf("valid escalation rejected: %v", err)
	}
	template.Escalation.AfterReviewRounds = 3
	if err := ValidateTemplate(template, Catalog{}); err == nil {
		t.Fatal("non-canonical escalation was accepted")
	}
}

func resolverTemplate(roles []Role) ExecutionTemplate {
	return ExecutionTemplate{
		SchemaVersion: 1, ID: ExecutionTemplateID, Roles: roles,
		Escalation: Escalation{AfterReviewRounds: 2, NextRole: "implementation.high"},
	}
}

func resolverHarness(id string, models ...Model) Harness { return Harness{ID: id, Models: models} }

func resolverModel(id, family, quality, entitlement string) Model {
	return Model{
		ID: id, ModelFamily: family, QualityTier: quality, Entitlement: entitlement,
		ReasoningLevels: []string{"high", "xhigh"}, Status: "experimental",
	}
}

func resolverRole(id, quality, harness, model string) Role {
	return Role{
		ID: id, MinimumQuality: quality, Reasoning: "high",
		Candidates: []Candidate{{Harness: harness, Model: model}},
	}
}

func resolverInventory(catalog Catalog) Inventory {
	inventory := Inventory{Harnesses: make([]HarnessObservation, 0, len(catalog.Harnesses))}
	for _, harness := range catalog.Harnesses {
		version := "1.0.0"
		observation := HarnessObservation{ID: harness.ID, Version: &version}
		for _, model := range harness.Models {
			observation.Models = append(observation.Models, resolverObservation(model, model.Entitlement))
		}
		inventory.Harnesses = append(inventory.Harnesses, observation)
	}
	return inventory
}

func resolverObservation(model Model, entitlement string) ModelObservation {
	family := model.ModelFamily
	return ModelObservation{
		ID: model.ID, ModelFamily: &family, QualityTier: model.QualityTier,
		Entitlement: entitlement, ReasoningLevels: model.ReasoningLevels,
		Provenance: "enumerated", Verified: false,
	}
}

func resolverAnswers(catalog Catalog) AnswersFile {
	answers := testAnswers()
	for _, harness := range catalog.Harnesses {
		for _, model := range harness.Models {
			answers.OperatorCapabilities = append(answers.OperatorCapabilities, OperatorCapability{
				Harness: harness.ID, Model: model.ID, Entitlement: model.Entitlement, AllowUnverified: true,
			})
		}
	}
	return answers
}

func stringPointer(value string) *string { return &value }
