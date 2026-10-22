package sdlc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRunProbeRejectsUnsafeOutput(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		result string
	}{
		{name: "prompt", body: "printf 'login required\\n'", result: "prompted"},
		{name: "nonzero", body: "exit 7", result: "nonzero"},
		{name: "malformed utf8", body: "printf '\\377'", result: "malformed"},
		{name: "oversized", body: "head -c 1048577 /dev/zero", result: "oversized"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			executable := filepath.Join(directory, "probe")
			content := "#!/bin/sh\n" + test.body + "\n"
			if err := os.WriteFile(executable, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			_, result := runProbe(Probe{Argv: []string{executable}, TimeoutSeconds: 1}, directory)
			if result != test.result {
				t.Fatalf("got %q, want %q", result, test.result)
			}
		})
	}
}

func TestSanitizeModelOutput(t *testing.T) {
	probe := Probe{Format: "json", IDFields: []string{"id"}}
	models, valid := sanitizeModelOutput(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"bad value"}]}`, probe)
	if !valid || len(models) != 2 || models[0] != "model-a" || models[1] != "model-b" {
		t.Fatalf("unexpected sanitized models: %#v, valid=%v", models, valid)
	}
}

func TestConcurrentInventoryPreservesCatalogOrderAndReportsProgress(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"slow-probe", "fast-probe"} {
		path := filepath.Join(directory, name)
		body := "#!/bin/sh\nprintf 'tool 1.0.0\\n'\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	catalog := Catalog{Catalog: "test", Harnesses: []Harness{
		testHarness("first", "slow-probe"),
		testHarness("second", "fast-probe"),
	}}
	var lock sync.Mutex
	events := make(map[string][]string)
	inventory := DiscoverInventoryWithProgress(catalog, nil, directory, func(id, state string) {
		lock.Lock()
		defer lock.Unlock()
		events[id] = append(events[id], state)
	})
	if inventory.Harnesses[0].ID != "first" || inventory.Harnesses[1].ID != "second" {
		t.Fatalf("catalog order changed: %#v", inventory.Harnesses)
	}
	for _, id := range []string{"first", "second"} {
		if !reflect.DeepEqual(events[id], []string{"probing", "complete"}) {
			t.Fatalf("unexpected events for %s: %#v", id, events[id])
		}
	}
}

func TestDiscoveryCandidateAbsentProducesNoObservation(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	catalog := Catalog{Catalog: "test", DiscoveryCandidates: []DiscoveryCandidate{
		testDiscoveryCandidate("candidate", "candidate"),
	}}

	inventory := DiscoverInventory(catalog, nil, directory)
	if len(inventory.Harnesses) != 0 {
		t.Fatalf("absent candidate became visible: %#v", inventory.Harnesses)
	}
}

func TestDiscoveryCandidateAliasesCollapseToOneObservation(t *testing.T) {
	directory := t.TempDir()
	for _, executable := range []string{"candidate", "candidate-alias"} {
		writeProbeExecutable(t, directory, executable, "printf '1.2.3\\n'")
	}
	t.Setenv("PATH", directory)
	candidate := testDiscoveryCandidate("candidate", "candidate", "candidate-alias")

	inventory := DiscoverInventory(Catalog{Catalog: "test", DiscoveryCandidates: []DiscoveryCandidate{candidate}}, nil, directory)
	if len(inventory.Harnesses) != 1 || inventory.Harnesses[0].ID != "candidate" {
		t.Fatalf("aliases did not collapse to one candidate: %#v", inventory.Harnesses)
	}
}

func TestDiscoveryCandidateRejectsMalformedVersion(t *testing.T) {
	directory := t.TempDir()
	writeProbeExecutable(t, directory, "candidate", "printf 'version unknown\\n'")
	t.Setenv("PATH", directory)
	catalog := Catalog{Catalog: "test", DiscoveryCandidates: []DiscoveryCandidate{
		testDiscoveryCandidate("candidate", "candidate"),
	}}

	inventory := DiscoverInventory(catalog, nil, directory)
	observation := inventory.Harnesses[0]
	if observation.Version != nil || observation.State != "unverified" || !reflect.DeepEqual(observation.ProbeErrors, []string{"malformed"}) {
		t.Fatalf("malformed candidate version was accepted: %#v", observation)
	}
}

func TestDiscoveryCandidateVersionIsVisibleButNonRoutable(t *testing.T) {
	directory := t.TempDir()
	writeProbeExecutable(t, directory, "candidate", "printf '9.8.7\\n'")
	t.Setenv("PATH", directory)
	catalog := Catalog{Catalog: "test", DiscoveryCandidates: []DiscoveryCandidate{
		testDiscoveryCandidate("candidate", "candidate"),
	}}

	inventory := DiscoverInventory(catalog, nil, directory)
	observation := inventory.Harnesses[0]
	if observation.Version == nil || *observation.Version != "9.8.7" || observation.State != "installed" || observation.RangeStatus != "unverified" {
		t.Fatalf("unexpected candidate observation: %#v", observation)
	}
	if len(observation.Models) != 0 {
		t.Fatalf("uncataloged candidate became routable: %#v", observation.Models)
	}
}

func TestCatalogValidatesDiscoveryCandidateContract(t *testing.T) {
	if err := ValidateCatalog(validDiscoveryCatalog()); err != nil {
		t.Fatalf("valid discovery catalog failed: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Catalog)
		want   string
	}{
		{name: "invalid id", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].ID = "Bad ID" }, want: "invalid harness discovery candidate id"},
		{name: "cataloged id", mutate: func(catalog *Catalog) {
			catalog.Harnesses = []Harness{testHarness("candidate", "cataloged")}
		}, want: "cataloged harness cannot also be a discovery candidate"},
		{name: "duplicate id", mutate: func(catalog *Catalog) {
			catalog.DiscoveryCandidates = append(catalog.DiscoveryCandidates, testDiscoveryCandidate("candidate", "other"))
		}, want: "duplicate harness discovery candidate"},
		{name: "no executable", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].Executables = nil }, want: "lacks an executable"},
		{name: "invalid pattern", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].VersionPattern = "[" }, want: "invalid discovery version pattern"},
		{name: "missing version capture", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].VersionPattern = `^[0-9]+$` }, want: "lacks a named version capture"},
		{name: "invalid executable", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].Executables[0].Executable = "Bad Alias" }, want: "version probe executable mismatch"},
		{name: "extra argument", mutate: func(catalog *Catalog) {
			catalog.DiscoveryCandidates[0].Executables[0].VersionProbe.Argv = []string{"candidate", "--version", "--json"}
		}, want: "version probe executable mismatch"},
		{name: "different executable", mutate: func(catalog *Catalog) {
			catalog.DiscoveryCandidates[0].Executables[0].VersionProbe.Argv[0] = "other"
		}, want: "version probe executable mismatch"},
		{name: "unsafe argument", mutate: func(catalog *Catalog) {
			catalog.DiscoveryCandidates[0].Executables[0].VersionProbe.Argv[1] = "models"
		}, want: "version probe executable mismatch"},
		{name: "zero timeout", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].Executables[0].VersionProbe.TimeoutSeconds = 0 }, want: "timeout is invalid"},
		{name: "long timeout", mutate: func(catalog *Catalog) { catalog.DiscoveryCandidates[0].Executables[0].VersionProbe.TimeoutSeconds = 6 }, want: "timeout is invalid"},
		{name: "cataloged executable", mutate: func(catalog *Catalog) {
			catalog.Harnesses = []Harness{testHarness("cataloged", "candidate")}
		}, want: "duplicate harness executable or alias"},
		{name: "duplicate alias", mutate: func(catalog *Catalog) {
			catalog.DiscoveryCandidates[0].Executables = append(catalog.DiscoveryCandidates[0].Executables, catalog.DiscoveryCandidates[0].Executables[0])
		}, want: "duplicate harness executable or alias"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog := validDiscoveryCatalog()
			test.mutate(&catalog)
			if err := ValidateCatalog(catalog); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestSupportedRangeRequiresExactCertifiedVersion(t *testing.T) {
	rangeContract := VersionRange{
		Minimum: "1.0.0", MaximumExclusive: "2.0.0", Status: "supported",
		Certification: Certification{TestedVersions: []string{"1.2.3"}},
	}
	if got := effectiveRangeStatus(rangeContract, "1.2.3"); got != "supported" {
		t.Fatalf("certified version got %q", got)
	}
	if got := effectiveRangeStatus(rangeContract, "1.2.4"); got != "unverified" {
		t.Fatalf("uncertified patch got %q", got)
	}
}

func TestCatalogRejectsCertifiedVersionOutsideRange(t *testing.T) {
	when := "2026-08-14T00:00:00Z"
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	harness := testHarness("example", "example")
	harness.Ranges[0] = VersionRange{
		Minimum: "1.0.0", MaximumExclusive: "2.0.0", Status: "supported",
		Certification: Certification{
			Schema: true, Argv: true, FakeCLI: true, Resolver: true, Live: "passed", Deck: "passed",
			TestedVersions: []string{"2.0.0"}, CertifiedAt: &when, ResultDigest: &digest,
		},
	}
	if err := ValidateCatalog(Catalog{SchemaVersion: 1, Catalog: "test", Harnesses: []Harness{harness}}); err == nil {
		t.Fatal("expected out-of-range certified version to fail")
	}
}

func TestCatalogRequiresDeckEvidenceForSupportedRange(t *testing.T) {
	when := "2026-08-14T00:00:00Z"
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	harness := testHarness("example", "example")
	harness.Ranges[0] = VersionRange{
		Minimum: "1.0.0", MaximumExclusive: "2.0.0", Status: "supported",
		Certification: Certification{
			Schema: true, Argv: true, FakeCLI: true, Resolver: true, Live: "passed", Deck: "not-applicable",
			TestedVersions: []string{"1.2.3"}, CertifiedAt: &when, ResultDigest: &digest,
		},
	}
	if err := ValidateCatalog(Catalog{SchemaVersion: 1, Catalog: "test", Harnesses: []Harness{harness}}); err == nil {
		t.Fatal("expected missing Deck interoperability evidence to fail")
	}
}

func TestCatalogRejectsNonNormalizedReasoningLevel(t *testing.T) {
	harness := testHarness("example", "example")
	harness.Ranges[0].ReasoningLevels = []string{"none"}
	if err := ValidateCatalog(Catalog{SchemaVersion: 1, Catalog: "test", Harnesses: []Harness{harness}}); err == nil {
		t.Fatal("expected non-normalized range reasoning level to fail")
	}

	harness = testHarness("example", "example")
	harness.Models = []Model{{ID: "example/model", ReasoningLevels: []string{"ultra"}}}
	if err := ValidateCatalog(Catalog{SchemaVersion: 1, Catalog: "test", Harnesses: []Harness{harness}}); err == nil {
		t.Fatal("expected non-normalized model reasoning level to fail")
	}
}

func testHarness(id, executable string) Harness {
	return Harness{
		ID: id, Executable: executable,
		VersionProbe:   Probe{Argv: []string{executable}, TimeoutSeconds: 1},
		VersionPattern: `(?P<version>[0-9]+\.[0-9]+\.[0-9]+)`,
		Ranges:         []VersionRange{{Minimum: "0.0.0", MaximumExclusive: "2.0.0", Status: "experimental"}},
		Models:         []Model{},
	}
}

func testDiscoveryCandidate(id string, executables ...string) DiscoveryCandidate {
	aliases := make([]CandidateExecutable, 0, len(executables))
	for _, executable := range executables {
		aliases = append(aliases, CandidateExecutable{
			Executable: executable,
			VersionProbe: Probe{
				Argv: []string{executable, "--version"}, TimeoutSeconds: 1, Format: "lines", Source: "static",
			},
		})
	}
	return DiscoveryCandidate{
		ID: id, Executables: aliases, VersionPattern: `^(?P<version>[0-9]+\.[0-9]+\.[0-9]+)$`,
	}
}

func validDiscoveryCatalog() Catalog {
	return Catalog{
		SchemaVersion: 1,
		Catalog:       "test",
		DiscoveryCandidates: []DiscoveryCandidate{
			testDiscoveryCandidate("candidate", "candidate"),
		},
	}
}

func writeProbeExecutable(t *testing.T, directory, name, body string) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
