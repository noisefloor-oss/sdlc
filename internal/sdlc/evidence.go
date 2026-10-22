package sdlc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CheckEvidence validates a declared evidence record, not the truth of its
// observations or any project milestone. Source references are never opened.
func CheckEvidence(repositoryRoot, path string) (map[string]any, error) {
	data, err := readEvidenceFile(path)
	if err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid evidence JSON in %s: %w", path, err)
	}
	record, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("evidence must be an object: %s", path)
	}
	version, known := evidenceNumber(record["schemaVersion"])
	schemaFile := "evidence.schema.json"
	if !known {
		return nil, fmt.Errorf("unsupported evidence schema version in %s", path)
	}
	legacy := version.Cmp(big.NewRat(1, 1)) == 0
	if legacy {
		schemaFile = "evidence-v1.schema.json"
	} else if version.Cmp(big.NewRat(2, 1)) != 0 {
		return nil, fmt.Errorf("unsupported evidence schema version in %s", path)
	}
	schemaPath := filepath.Join(repositoryRoot, "docs/schemas", schemaFile)
	schemaBytes, err := readEvidenceResource(repositoryRoot, schemaPath)
	if err != nil {
		return nil, err
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid evidence schema %s: %w", schemaPath, err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	const resource = "https://noise.invalid/sdlc/evidence.schema.json"
	if err := compiler.AddResource(resource, schemaDocument); err != nil {
		return nil, fmt.Errorf("invalid evidence schema %s: %w", schemaPath, err)
	}
	schema, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("invalid evidence schema %s: %w", schemaPath, err)
	}
	if err := schema.Validate(document); err != nil {
		return nil, fmt.Errorf("evidence schema violation in %s: %w", path, err)
	}
	if !legacy {
		if err := validateOperationalEvidence(record["operations"].(map[string]any)); err != nil {
			return nil, fmt.Errorf("invalid operational evidence in %s: %w", path, err)
		}
	}
	return map[string]any{"schemaVersion": 1, "kind": "noise.sdlc.evidence-validation", "recordSchemaVersion": map[bool]int{true: 1, false: 2}[legacy], "validation": "valid", "observationTruth": "not-assessed", "milestoneAcceptance": "not-assessed", "humanLaborEstimate": "not-computed", "rates": "not-computed"}, nil
}

func readEvidenceFile(path string) ([]byte, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(absolute)
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("cannot open evidence directory: %w", err)
	}
	defer root.Close()
	data, err := readDiagnosticFile(root, directory, filepath.Base(absolute))
	if err != nil {
		return nil, fmt.Errorf("cannot read bounded evidence file %s: %w", path, err)
	}
	return data, nil
}

func evidenceNumber(value any) (*big.Rat, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	result, ok := new(big.Rat).SetString(string(number))
	return result, ok
}

func validateOperationalEvidence(operations map[string]any) error {
	sources := map[string]string{}
	for _, raw := range operations["sources"].([]any) {
		source := raw.(map[string]any)
		id := source["id"].(string)
		if _, exists := sources[id]; exists {
			return fmt.Errorf("duplicate source id %s", id)
		}
		path := source["repositoryPath"].(string)
		if filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path || strings.Contains(path, "\\") {
			return fmt.Errorf("source %s must use a clean repository-relative public path", id)
		}
		sources[id] = source["kind"].(string)
	}
	window := operations["window"].(map[string]any)
	if err := evidenceTimeOrder(window["start"], window["end"]); err != nil {
		return err
	}
	unknown := false
	for _, value := range window {
		if value == "unknown" {
			unknown = true
		}
	}
	for _, name := range []string{"denominator", "interventions", "timing", "cleanup"} {
		group := operations[name].(map[string]any)
		refs := group["sourceRefs"].([]any)
		for _, raw := range refs {
			if _, ok := sources[raw.(string)]; !ok {
				return fmt.Errorf("%s has a dangling source reference", name)
			}
		}
		observed := false
		for key, value := range group {
			if _, ok := evidenceNumber(value); ok {
				observed = true
			}
			if value == "unknown" {
				unknown = true
			}
			if key == "observedAt" && value != "unknown" && value != "not-applicable" {
				observed = true // The schema has already validated the timestamp.
			}
		}
		if observed && len(refs) == 0 {
			return fmt.Errorf("%s numerical or dated observations require source references", name)
		}
	}
	if unknown && len(operations["limitations"].([]any)) == 0 {
		return fmt.Errorf("unknown measurements require an explicit limitation")
	}
	timing := operations["timing"].(map[string]any)
	total, totalKnown := evidenceNumber(timing["elapsedMilliseconds"])
	sum := new(big.Rat)
	complete := true
	for _, key := range []string{"queueWaitMilliseconds", "executionMilliseconds", "humanDecisionWaitMilliseconds", "unattributedMilliseconds"} {
		value, ok := evidenceNumber(timing[key])
		if !ok {
			complete = false
			continue
		}
		sum.Add(sum, value)
	}
	if totalKnown && (sum.Cmp(total) > 0 || (complete && sum.Cmp(total) != 0)) {
		return fmt.Errorf("exclusive timing states must fit the declared elapsed total and equal it when complete")
	}
	if _, observed := evidenceNumber(timing["humanLaborMilliseconds"]); observed {
		method := timing["humanLaborMethod"].(string)
		if method != "direct-observation" && method != "operator-report" {
			return fmt.Errorf("human labor requires a direct observation or operator report, never elapsed-time inference")
		}
		if method == "operator-report" {
			found := false
			for _, raw := range timing["sourceRefs"].([]any) {
				if sources[raw.(string)] == "operator-report" {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("operator-reported labor requires an operator-report source")
			}
		}
	} else if method := timing["humanLaborMethod"]; method != "unknown" && method != "not-applicable" {
		return fmt.Errorf("unmeasured human labor must retain an unknown or not-applicable method")
	}
	return nil
}

func evidenceTimeOrder(start, end any) error {
	if start == "unknown" || start == "not-applicable" || end == "unknown" || end == "not-applicable" {
		return nil
	}
	first, err1 := parseEvidenceTime(start)
	last, err2 := parseEvidenceTime(end)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("cannot compare observation window timestamps")
	}
	if last.Cmp(first) < 0 {
		return fmt.Errorf("observation window ends before it starts")
	}
	return nil
}

var evidenceTimestamp = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:)(\d{2})(\.[0-9]+)?(Z|[+-]\d{2}:\d{2})$`)

func parseEvidenceTime(value any) (*big.Rat, error) {
	parts := evidenceTimestamp.FindStringSubmatch(strings.ToUpper(fmt.Sprint(value)))
	if parts == nil {
		return nil, fmt.Errorf("invalid observation timestamp")
	}
	second, leap := parts[2], int64(0)
	if second == "60" {
		second, leap = "59", 1
	}
	base, err := time.Parse(time.RFC3339, parts[1]+second+parts[4])
	if err != nil {
		return nil, err
	}
	// This is an ordering key, never an elapsed-time measurement. Leave a slot
	// after each ordinary second for RFC3339 leap seconds, and retain arbitrary
	// fractional precision instead of truncating to Go's nanoseconds.
	key := big.NewRat(2*base.Unix()+leap, 1)
	if parts[3] != "" {
		fraction, ok := new(big.Rat).SetString("0" + parts[3])
		if !ok {
			return nil, fmt.Errorf("invalid observation timestamp fraction")
		}
		key.Add(key, fraction)
	}
	return key, nil
}
