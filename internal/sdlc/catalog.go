package sdlc

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	Version             = "0.4.0"
	ExecutionTemplateID = "deck/balanced@0.2"
)

var (
	semanticVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	executableNamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	// normalizedReasoningLevels mirrors the enum in
	// docs/schemas/harness-catalog.schema.json; harness-specific spellings
	// (e.g. cline's "none") must be normalized at authoring time.
	normalizedReasoningLevels = map[string]bool{
		"off": true, "minimal": true, "low": true, "medium": true,
		"high": true, "xhigh": true, "max": true,
	}
)

// RepositoryRoot returns an explicit source override, or an empty root to use
// bundled resources regardless of the build location or caller's directory.
func RepositoryRoot() (string, error) {
	root := os.Getenv("NOISE_SDLC_SOURCE_ROOT")
	if root == "" {
		return "", nil
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("NOISE_SDLC_SOURCE_ROOT must be an absolute path")
	}
	return filepath.Clean(root), nil
}

func LoadCatalog(root string) (Catalog, error) {
	baseSchema, harnessSchema, err := compileCatalogSchemas(root)
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	if err := loadSchemaJSON(root, filepath.Join(root, "catalog", "harnesses.json"), baseSchema, &catalog); err != nil {
		return Catalog{}, err
	}
	var discovery DiscoveryCatalog
	if err := loadResourceJSON(root, filepath.Join(root, "catalog", "harness-candidates.json"), &discovery); err != nil {
		return Catalog{}, err
	}
	if discovery.SchemaVersion != 1 {
		return Catalog{}, fmt.Errorf("unsupported harness discovery schema")
	}
	catalog.DiscoveryCandidates = discovery.Candidates
	fragments, err := globResources(root, filepath.Join(root, "catalog", "harnesses.d", "*.json"))
	if err != nil {
		return Catalog{}, err
	}
	sort.Strings(fragments)
	for _, fragment := range fragments {
		var harness Harness
		if err := loadSchemaJSON(root, fragment, harnessSchema, &harness); err != nil {
			return Catalog{}, err
		}
		catalog.Harnesses = append(catalog.Harnesses, harness)
	}
	sort.Slice(catalog.Harnesses, func(left, right int) bool {
		return catalog.Harnesses[left].ID < catalog.Harnesses[right].ID
	})
	if err := ValidateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func LoadSeed(root string) (Seed, error) {
	var seed Seed
	err := loadResourceJSON(root, filepath.Join(root, "catalog", "packs", "noise-agent-0.1.json"), &seed)
	return seed, err
}

func LoadTemplate(root string) (ExecutionTemplate, error) {
	var template ExecutionTemplate
	err := loadResourceJSON(root, filepath.Join(root, "catalog", "execution", "deck-balanced-0.2.json"), &template)
	return template, err
}

func ValidateCatalog(catalog Catalog) error {
	if catalog.SchemaVersion != 1 {
		return fmt.Errorf("unsupported harness catalog schema")
	}
	seenHarnesses := make(map[string]struct{})
	for _, harness := range catalog.Harnesses {
		if harness.ID == "" {
			return fmt.Errorf("invalid harness id")
		}
		if _, duplicate := seenHarnesses[harness.ID]; duplicate {
			return fmt.Errorf("duplicate harness id: %s", harness.ID)
		}
		seenHarnesses[harness.ID] = struct{}{}
		if len(harness.VersionProbe.Argv) == 0 || harness.VersionProbe.Argv[0] != harness.Executable {
			return fmt.Errorf("%s: version probe executable mismatch", harness.ID)
		}
		if _, err := compileVersionPattern(harness.VersionPattern); err != nil {
			return fmt.Errorf("%s: invalid version pattern: %w", harness.ID, err)
		}
		if harness.EnumerationProbe != nil {
			if len(harness.EnumerationProbe.Argv) == 0 || harness.EnumerationProbe.Argv[0] != harness.Executable {
				return fmt.Errorf("%s: enumeration probe executable mismatch", harness.ID)
			}
		}
		seenModels := make(map[string]struct{})
		for _, model := range harness.Models {
			if _, duplicate := seenModels[model.ID]; duplicate {
				return fmt.Errorf("%s: duplicate model %s", harness.ID, model.ID)
			}
			seenModels[model.ID] = struct{}{}
			for _, level := range model.ReasoningLevels {
				if !normalizedReasoningLevels[level] {
					return fmt.Errorf("%s: model %s uses non-normalized reasoning level %q", harness.ID, model.ID, level)
				}
			}
		}
		for _, versionRange := range harness.Ranges {
			if err := ValidateInvocationContract(harness, versionRange); err != nil {
				return err
			}
			if compareVersions(versionRange.Minimum, versionRange.MaximumExclusive) >= 0 {
				return fmt.Errorf("%s: empty version range", harness.ID)
			}
			for _, level := range versionRange.ReasoningLevels {
				if !normalizedReasoningLevels[level] {
					return fmt.Errorf("%s: range uses non-normalized reasoning level %q", harness.ID, level)
				}
			}
			if versionRange.Status == "supported" {
				certification := versionRange.Certification
				if !certification.Schema || !certification.Argv || !certification.FakeCLI || !certification.Resolver || certification.Live != "passed" ||
					certification.Deck != "passed" || certification.CertifiedAt == nil || certification.ResultDigest == nil ||
					!digestPattern.MatchString(*certification.ResultDigest) || len(certification.TestedVersions) == 0 {
					return fmt.Errorf("%s: supported range lacks certification", harness.ID)
				}
			}
			seenVersions := make(map[string]struct{})
			for _, tested := range versionRange.Certification.TestedVersions {
				if !semanticVersionPattern.MatchString(tested) || compareVersions(tested, versionRange.Minimum) < 0 || compareVersions(tested, versionRange.MaximumExclusive) >= 0 {
					return fmt.Errorf("%s: certified version %q is outside its range", harness.ID, tested)
				}
				if _, duplicate := seenVersions[tested]; duplicate {
					return fmt.Errorf("%s: duplicate certified version %q", harness.ID, tested)
				}
				seenVersions[tested] = struct{}{}
			}
		}
	}
	seenCandidates := make(map[string]struct{})
	seenExecutables := make(map[string]struct{})
	for _, harness := range catalog.Harnesses {
		seenExecutables[harness.Executable] = struct{}{}
	}
	for _, candidate := range catalog.DiscoveryCandidates {
		if !executableNamePattern.MatchString(candidate.ID) {
			return fmt.Errorf("invalid harness discovery candidate id")
		}
		if _, duplicate := seenHarnesses[candidate.ID]; duplicate {
			return fmt.Errorf("cataloged harness cannot also be a discovery candidate: %s", candidate.ID)
		}
		if _, duplicate := seenCandidates[candidate.ID]; duplicate {
			return fmt.Errorf("duplicate harness discovery candidate: %s", candidate.ID)
		}
		seenCandidates[candidate.ID] = struct{}{}
		if len(candidate.Executables) == 0 {
			return fmt.Errorf("%s: harness discovery candidate lacks an executable", candidate.ID)
		}
		pattern, err := compileVersionPattern(candidate.VersionPattern)
		if err != nil {
			return fmt.Errorf("%s: invalid discovery version pattern: %w", candidate.ID, err)
		}
		if pattern.SubexpIndex("version") < 0 {
			return fmt.Errorf("%s: discovery version pattern lacks a named version capture", candidate.ID)
		}
		for _, alias := range candidate.Executables {
			if !executableNamePattern.MatchString(alias.Executable) || len(alias.VersionProbe.Argv) != 2 ||
				alias.VersionProbe.Argv[0] != alias.Executable || alias.VersionProbe.Argv[1] != "--version" {
				return fmt.Errorf("%s: discovery version probe executable mismatch", candidate.ID)
			}
			if alias.VersionProbe.TimeoutSeconds <= 0 || alias.VersionProbe.TimeoutSeconds > 5 {
				return fmt.Errorf("%s: discovery version probe timeout is invalid", candidate.ID)
			}
			if _, duplicate := seenExecutables[alias.Executable]; duplicate {
				return fmt.Errorf("duplicate harness executable or alias: %s", alias.Executable)
			}
			seenExecutables[alias.Executable] = struct{}{}
		}
	}
	return nil
}

func ValidateTemplate(template ExecutionTemplate, catalog Catalog) error {
	if template.SchemaVersion != 1 || template.ID != ExecutionTemplateID {
		return fmt.Errorf("unsupported execution template")
	}
	if template.Escalation.AfterReviewRounds != 2 || template.Escalation.NextRole != "implementation.high" {
		return fmt.Errorf("execution template must escalate to implementation.high after two review rounds")
	}
	expectedRoles := []string{"implementation.bulk", "implementation.standard", "implementation.high", "review.formal", "repair"}
	if len(template.Roles) != len(expectedRoles) {
		return fmt.Errorf("execution roles are missing, duplicated, or out of canonical order")
	}
	knownModels := make(map[string]struct{})
	for _, harness := range catalog.Harnesses {
		for _, model := range harness.Models {
			knownModels[modelKey(harness.ID, model.ID)] = struct{}{}
		}
	}
	for index, role := range template.Roles {
		if role.ID != expectedRoles[index] {
			return fmt.Errorf("execution roles are missing, duplicated, or out of canonical order")
		}
		for _, candidate := range role.Candidates {
			if _, found := knownModels[modelKey(candidate.Harness, candidate.Model)]; !found {
				return fmt.Errorf("%s: unknown candidate %s/%s", role.ID, candidate.Harness, candidate.Model)
			}
		}
	}
	return nil
}

func ValidateAll(root string) error {
	catalog, err := LoadCatalog(root)
	if err != nil {
		return err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return err
	}
	seed, err := LoadSeed(root)
	if err != nil {
		return err
	}
	if seed.SchemaVersion != 1 || seed.ID != "noise/agent@0.1" {
		return fmt.Errorf("unsupported lifecycle seed")
	}
	template, err := LoadTemplate(root)
	if err != nil {
		return err
	}
	return ValidateTemplate(template, catalog)
}

func matchingRange(harness Harness, version string) *VersionRange {
	for index := range harness.Ranges {
		candidate := &harness.Ranges[index]
		if compareVersions(candidate.Minimum, version) <= 0 && compareVersions(version, candidate.MaximumExclusive) < 0 {
			return candidate
		}
	}
	return nil
}

func compareVersions(left, right string) int {
	leftParts := semanticVersionPattern.FindStringSubmatch(left)
	rightParts := semanticVersionPattern.FindStringSubmatch(right)
	if leftParts == nil || rightParts == nil {
		return strings.Compare(left, right)
	}
	for index := 1; index <= 3; index++ {
		leftNumber, _ := strconv.Atoi(leftParts[index])
		rightNumber, _ := strconv.Atoi(rightParts[index])
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
	}
	return 0
}

func compileVersionPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

func modelKey(harness, model string) string {
	return harness + "\x00" + model
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
