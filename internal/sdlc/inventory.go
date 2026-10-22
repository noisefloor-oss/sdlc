package sdlc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maximumProbeOutput = 1_048_576

var (
	publicModelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._~:/-]{0,127}$`)
	promptPattern      = regexp.MustCompile(`(?i)(press enter|login required|log in to continue|authenticate to continue|select an option|enter (?:an? )?(?:api )?key|continue\?\s*$)`)
	columnPattern      = regexp.MustCompile(`^([a-z0-9][a-z0-9._~:/-]{0,127})\s{2,}\S.*$`)
)

func DiscoverInventory(catalog Catalog, declarations []OperatorCapability, workingDirectory string) Inventory {
	return DiscoverInventoryWithProgress(catalog, declarations, workingDirectory, nil)
}

type InventoryProgress func(harnessID, state string)

func DiscoverInventoryWithProgress(
	catalog Catalog,
	declarations []OperatorCapability,
	workingDirectory string,
	progress InventoryProgress,
) Inventory {
	declarationsByHarness := make(map[string][]OperatorCapability)
	for _, declaration := range declarations {
		declarationsByHarness[declaration.Harness] = append(declarationsByHarness[declaration.Harness], declaration)
	}

	result := Inventory{
		SchemaVersion: 1,
		Source:        "standalone-best-effort",
		Catalog:       catalog.Catalog,
		Harnesses:     make([]HarnessObservation, len(catalog.Harnesses)),
	}
	var wait sync.WaitGroup
	for index, harness := range catalog.Harnesses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if progress != nil {
				progress(harness.ID, "probing")
			}
			result.Harnesses[index] = inspectHarness(
				harness,
				declarationsByHarness[harness.ID],
				workingDirectory,
			)
			if progress != nil {
				progress(harness.ID, "complete")
			}
		}()
	}
	wait.Wait()

	candidateObservations := make(chan HarnessObservation, len(catalog.DiscoveryCandidates))
	for _, candidate := range catalog.DiscoveryCandidates {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if progress != nil {
				progress(candidate.ID, "probing")
			}
			if observation, found := inspectDiscoveryCandidate(candidate, workingDirectory); found {
				candidateObservations <- observation
			}
			if progress != nil {
				progress(candidate.ID, "complete")
			}
		}()
	}
	wait.Wait()
	close(candidateObservations)
	candidates := make([]HarnessObservation, 0, len(candidateObservations))
	for observation := range candidateObservations {
		candidates = append(candidates, observation)
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].ID < candidates[right].ID })
	result.Harnesses = append(result.Harnesses, candidates...)
	return result
}

func inspectDiscoveryCandidate(candidate DiscoveryCandidate, workingDirectory string) (HarnessObservation, bool) {
	var selected *CandidateExecutable
	for index := range candidate.Executables {
		if _, err := exec.LookPath(candidate.Executables[index].Executable); err == nil {
			selected = &candidate.Executables[index]
			break
		}
	}
	if selected == nil {
		return HarnessObservation{}, false
	}

	observation := HarnessObservation{
		ID: candidate.ID, State: "unverified", RangeStatus: "unverified",
		Models: []ModelObservation{}, Unknown: []string{"models", "ready", "spend-authority"},
	}
	versionOutput, probeError := runProbe(selected.VersionProbe, workingDirectory)
	if probeError != "" {
		observation.ProbeErrors = append(observation.ProbeErrors, probeError)
	} else {
		pattern, _ := compileVersionPattern(candidate.VersionPattern)
		matches := pattern.FindStringSubmatch(strings.TrimSpace(versionOutput))
		if matches == nil {
			observation.ProbeErrors = append(observation.ProbeErrors, "malformed")
		} else {
			for index, name := range pattern.SubexpNames() {
				if name == "version" {
					version := matches[index]
					observation.Version = &version
					observation.State = "installed"
				}
			}
		}
	}
	if observation.Version == nil {
		observation.Unknown = append(observation.Unknown, "version")
	}
	observation.Unknown = sortedUnique(observation.Unknown)
	observation.ProbeErrors = sortedUnique(observation.ProbeErrors)
	return observation, true
}

func inspectHarness(harness Harness, declarations []OperatorCapability, workingDirectory string) HarnessObservation {
	if _, err := exec.LookPath(harness.Executable); err != nil {
		return HarnessObservation{
			ID: harness.ID, State: "absent", RangeStatus: "absent",
			Models: []ModelObservation{}, Unknown: []string{"installed", "ready", "models"}, ProbeErrors: []string{"not-found"},
		}
	}

	observation := HarnessObservation{ID: harness.ID, State: "unverified", RangeStatus: "unverified", Models: []ModelObservation{}}
	versionOutput, probeError := runProbe(harness.VersionProbe, workingDirectory)
	if probeError != "" {
		observation.ProbeErrors = append(observation.ProbeErrors, probeError)
	}
	var versionRange *VersionRange
	if probeError == "" {
		pattern, _ := compileVersionPattern(harness.VersionPattern)
		matches := pattern.FindStringSubmatch(strings.TrimSpace(versionOutput))
		if matches == nil {
			observation.ProbeErrors = append(observation.ProbeErrors, "malformed")
		} else {
			for index, name := range pattern.SubexpNames() {
				if name == "version" {
					version := matches[index]
					observation.Version = &version
					versionRange = matchingRange(harness, version)
				}
			}
		}
	}
	if versionRange != nil {
		observation.State = "matched"
		observation.RangeStatus = effectiveRangeStatus(*versionRange, *observation.Version)
	}
	certifiedVersion := versionRange != nil && observation.Version != nil && effectiveRangeStatus(*versionRange, *observation.Version) == "supported"

	models := make(map[string]ModelObservation)
	modelsObserved := false
	if versionRange != nil && harness.EnumerationProbe != nil {
		modelOutput, modelError := runProbe(*harness.EnumerationProbe, workingDirectory)
		if modelError != "" {
			observation.ProbeErrors = append(observation.ProbeErrors, modelError)
		} else if modelIDs, valid := sanitizeModelOutput(modelOutput, *harness.EnumerationProbe); !valid {
			observation.ProbeErrors = append(observation.ProbeErrors, "malformed")
		} else if len(modelIDs) == 0 && strings.TrimSpace(modelOutput) != "" {
			observation.ProbeErrors = append(observation.ProbeErrors, "sanitizer-rejected")
		} else {
			modelsObserved = true
			observation.State = "enumerated"
			for _, modelID := range modelIDs {
				if model := findModel(harness, modelID); model != nil {
					models[modelID] = normalizeModel(*model, "enumerated", certifiedVersion)
				} else {
					models[modelID] = unknownModel(modelID)
				}
			}
		}
	}
	if versionRange != nil && harness.EnumerationProbe == nil {
		for _, model := range harness.Models {
			models[model.ID] = normalizeModel(model, "catalog-static", certifiedVersion)
		}
	}

	modelsDeclared := false
	for _, declaration := range declarations {
		model := findModel(harness, declaration.Model)
		if model == nil {
			continue
		}
		modelsDeclared = true
		normalized, found := models[model.ID]
		if !found {
			normalized = normalizeModel(*model, "operator-declared", certifiedVersion)
		}
		normalized.Entitlement = declaration.Entitlement
		normalized.Provenance = "operator-declared"
		models[model.ID] = normalized
		if observation.State != "enumerated" {
			observation.State = "operator-declared"
		}
	}

	modelIDs := make([]string, 0, len(models))
	for modelID := range models {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	for _, modelID := range modelIDs {
		observation.Models = append(observation.Models, models[modelID])
	}
	if observation.Version == nil {
		observation.Unknown = append(observation.Unknown, "version")
	}
	if !modelsObserved && !modelsDeclared {
		observation.Unknown = append(observation.Unknown, "models")
	}
	// Live subscription quota is deliberately absent: it is a per-account
	// runtime fact owned by whatever holds the provider session, it changes
	// between one probe and the next, and naming it here, even as unknown,
	// would claim a boundary this tool does not own.
	observation.Unknown = append(observation.Unknown, "ready", "spend-authority")
	observation.Unknown = sortedUnique(observation.Unknown)
	observation.ProbeErrors = sortedUnique(observation.ProbeErrors)
	return observation
}

func runProbe(probe Probe, workingDirectory string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(probe.TimeoutSeconds)*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, probe.Argv[0], probe.Argv[1:]...)
	command.Dir = workingDirectory
	command.Stdin = nil
	output := &boundedBuffer{remaining: maximumProbeOutput}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "timeout"
	}
	if errors.Is(err, errProbeOversized) || output.overflowed {
		return "", "oversized"
	}
	if err != nil {
		return "", "nonzero"
	}
	text := output.String()
	if !utf8.ValidString(text) {
		return "", "malformed"
	}
	if promptPattern.MatchString(text) {
		return "", "prompted"
	}
	return text, ""
}

var errProbeOversized = errors.New("probe output exceeds limit")

type boundedBuffer struct {
	buffer     bytes.Buffer
	remaining  int
	overflowed bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.remaining {
		buffer.overflowed = true
		return 0, errProbeOversized
	}
	buffer.remaining -= len(data)
	return buffer.buffer.Write(data)
}

func (buffer *boundedBuffer) String() string { return buffer.buffer.String() }

func sanitizeModelOutput(output string, probe Probe) ([]string, bool) {
	models := make(map[string]struct{})
	fields := probe.IDFields
	if len(fields) == 0 {
		fields = []string{"id", "model", "slug"}
	}
	fieldSet := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		fieldSet[field] = struct{}{}
	}

	switch probe.Format {
	case "json":
		var value any
		decoder := json.NewDecoder(strings.NewReader(output))
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		collectModelIDs(value, fieldSet, models)
	case "jsonl":
		for _, line := range strings.Split(output, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var value any
			if err := json.Unmarshal([]byte(line), &value); err != nil {
				return nil, false
			}
			collectModelIDs(value, fieldSet, models)
		}
	case "lines":
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if publicModelPattern.MatchString(line) {
				models[line] = struct{}{}
				continue
			}
			if columns := columnPattern.FindStringSubmatch(line); columns != nil {
				models[columns[1]] = struct{}{}
			}
		}
	default:
		return nil, false
	}

	result := make([]string, 0, len(models))
	for model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result, true
}

func collectModelIDs(value any, fields map[string]struct{}, destination map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, selected := fields[key]; selected {
				if modelID, ok := child.(string); ok && publicModelPattern.MatchString(modelID) {
					destination[modelID] = struct{}{}
				}
			}
			collectModelIDs(child, fields, destination)
		}
	case []any:
		for _, child := range typed {
			collectModelIDs(child, fields, destination)
		}
	}
}

func findModel(harness Harness, modelID string) *Model {
	for index := range harness.Models {
		if harness.Models[index].ID == modelID {
			return &harness.Models[index]
		}
	}
	return nil
}

func normalizeModel(model Model, provenance string, certifiedVersion bool) ModelObservation {
	family := model.ModelFamily
	entitlement := model.Entitlement
	if provenance == "catalog-static" {
		entitlement = "unknown"
	}
	return ModelObservation{
		ID: model.ID, ModelFamily: &family, QualityTier: model.QualityTier,
		Entitlement: entitlement, ReasoningLevels: model.ReasoningLevels, Provenance: provenance,
		Verified: certifiedVersion && model.Status == "supported",
	}
}

func effectiveRangeStatus(versionRange VersionRange, detected string) string {
	if versionRange.Status != "supported" {
		return versionRange.Status
	}
	for _, tested := range versionRange.Certification.TestedVersions {
		if tested == detected {
			return "supported"
		}
	}
	return "unverified"
}

func unknownModel(modelID string) ModelObservation {
	return ModelObservation{
		ID: modelID, QualityTier: "unknown", Entitlement: "unknown",
		ReasoningLevels: []string{}, Provenance: "enumerated", Verified: false,
	}
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

var _ io.Writer = (*boundedBuffer)(nil)
