package sdlc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExecutionShow reads the adopted route, never discovers or launches a harness.
// A supplied version is a caller declaration, not an observation of a process.
func ExecutionShow(repositoryRoot, target, role, version string) (map[string]any, error) {
	if version != "" && !validInvocationVersion(version) {
		return nil, fmt.Errorf("harness version must be a canonical major.minor.patch value")
	}
	if version != "" && role == "" {
		return nil, fmt.Errorf("a harness version requires one explicit role")
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("execution show requires an existing real project directory")
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Reuse exactly the bytes whose receipt was verified, rather than reopening
	// project state after verification and potentially displaying different data.
	files := map[string][]byte{}
	read := func(relative string) ([]byte, error) {
		if data, ok := files[relative]; ok {
			return data, nil
		}
		data, err := readDiagnosticFile(root, target, relative)
		if err == nil {
			files[relative] = data
		}
		return data, err
	}
	if _, err := verifyWithReader(target, read); err != nil {
		return nil, fmt.Errorf("execution guidance unavailable: %w; run noise-sdlc diagnose --target %s", err, target)
	}
	var receipt struct {
		Catalog string         `json:"catalog"`
		Inputs  map[string]any `json:"inputs"`
	}
	if err := json.Unmarshal(files[".sdlc/adoption.json"], &receipt); err != nil {
		return nil, err
	}
	var project struct {
		Execution struct {
			Roles []Binding `json:"roles"`
		} `json:"execution"`
	}
	if err := json.Unmarshal(files[".sdlc/project.yaml"], &project); err != nil {
		return nil, fmt.Errorf("invalid adopted execution bindings: %w", err)
	}
	seen := map[string]bool{}
	for _, binding := range project.Execution.Roles {
		if binding.Role == "" || seen[binding.Role] {
			return nil, fmt.Errorf("adopted execution roles must have unique nonempty names")
		}
		seen[binding.Role] = true
	}
	if role != "" && !seen[role] {
		return nil, fmt.Errorf("role %q is not present in the adopted project", role)
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("adopted project has no execution roles")
	}
	catalog, err := LoadCatalog(repositoryRoot)
	if err != nil {
		return nil, err
	}
	digest, err := Digest(catalog)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"schemaVersion": 1, "kind": "noise.sdlc.execution-guidance",
		"integrity": "verified", "state": "available", "target": target,
		"adoptedCatalog": receipt.Catalog, "availableCatalog": catalog.Catalog,
		"policyDigest":  receipt.Inputs["effectivePolicy"],
		"catalogDigest": digest, "adoptedCatalogDigest": receipt.Inputs["catalog"],
		"runtimeVersion": "not-observed", "executionAuthority": "not-granted",
		"workOwner": "noise tasks", "publicationOwner": "noise review",
		"roles": []any{},
	}
	if receipt.Catalog != catalog.Catalog || receipt.Inputs["catalog"] != digest {
		result["state"] = "catalog-mismatch"
		result["nextStep"] = "Review and apply a project update plan before using changed invocation contracts. The adopted routes have not been replaced."
		return result, nil
	}
	roles := []any{}
	for _, binding := range project.Execution.Roles {
		if role != "" && role != binding.Role {
			continue
		}
		roles = append(roles, executionRoleGuidance(catalog, binding, version))
	}
	result["roles"] = roles
	return result, nil
}

func executionRoleGuidance(catalog Catalog, binding Binding, version string) map[string]any {
	result := map[string]any{"binding": binding, "state": "unsupported", "contracts": []any{}}
	if binding.Status != "resolved" {
		result["state"] = "unresolved"
		result["nextStep"] = "Resolve this role through the project planner."
		return result
	}
	action := "execution"
	if binding.Role == "review.formal" {
		action = "review"
	}
	result["action"] = action
	result["declaredHarnessVersion"] = version
	for _, harness := range catalog.Harnesses {
		if harness.ID != pointerValue(binding.Harness) {
			continue
		}
		model := findModel(harness, pointerValue(binding.Model))
		if model == nil || model.ModelFamily != pointerValue(binding.ModelFamily) || !contains(model.ReasoningLevels, pointerValue(binding.Reasoning)) {
			result["nextStep"] = "The adopted model, family or reasoning is incompatible with this catalog; review a project update plan."
			return result
		}
		contracts := []any{}
		for _, versionRange := range harness.Ranges {
			if version != "" && (compareVersions(version, versionRange.Minimum) < 0 || compareVersions(version, versionRange.MaximumExclusive) >= 0) {
				continue
			}
			if versionRange.Invocation == nil || !contains(versionRange.ReasoningLevels, pointerValue(binding.Reasoning)) {
				continue
			}
			selected := versionRange.Invocation.Actions.Execution
			if action == "review" {
				selected = versionRange.Invocation.Actions.Review
			}
			if selected == nil {
				continue
			}
			contracts = append(contracts, map[string]any{
				"minimum": versionRange.Minimum, "maximumExclusive": versionRange.MaximumExclusive,
				"status": versionRange.Status, "certification": versionRange.Certification,
				"schemaVersion": versionRange.Invocation.SchemaVersion, "action": selected,
			})
		}
		if len(contracts) != 0 {
			result["contracts"] = contracts
			result["state"] = "declared-contracts"
			result["nextStep"] = "The runner must verify the installed version, enforce the declared boundary and obtain execution authority before preparing this invocation."
			return result
		}
	}
	result["nextStep"] = "No invocation contract matches this adopted route and declared version. Add a reviewed descriptor or explicitly update the route; no substitute has been selected."
	return result
}
