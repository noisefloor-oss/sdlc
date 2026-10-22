package sdlc

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	resources "github.com/noisefloor-oss/sdlc"
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

func TestRepositoryRootRequiresExplicitSource(t *testing.T) {
	t.Setenv("NOISE_SDLC_SOURCE_ROOT", "")
	if root, err := RepositoryRoot(); err != nil || root != "" {
		t.Fatalf("default must use embedded resources: root=%q, err=%v", root, err)
	}
	for _, relative := range []string{".", "noise-sdlc", "../.."} {
		t.Setenv("NOISE_SDLC_SOURCE_ROOT", relative)
		if _, err := RepositoryRoot(); err == nil {
			t.Fatalf("relative source override accepted: %q", relative)
		}
	}
	root := t.TempDir()
	t.Setenv("NOISE_SDLC_SOURCE_ROOT", root)
	if got, err := RepositoryRoot(); err != nil || got != root {
		t.Fatalf("explicit source override: root=%q, err=%v", got, err)
	}
	if _, err := LoadCatalog(root); err == nil {
		t.Fatal("missing override resources fell back to embedded catalog")
	}
}

func TestBundledResourcesPreserveSourceBytes(t *testing.T) {
	root, err := RepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WalkDir(resources.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		bundled, err := resources.Files.ReadFile(path)
		if err != nil {
			return err
		}
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if !bytes.Equal(bundled, source) {
			t.Errorf("bundled resource differs from source: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBundledEvidenceSchemas(t *testing.T) {
	_, path, record := evidenceFixture(t)
	if _, err := CheckEvidence("", path); err != nil {
		t.Fatal(err)
	}
	record["schemaVersion"] = 1
	delete(record, "operations")
	record["update"].(map[string]any)["rollback"] = "unknown"
	writeSchemaFixture(t, path, record)
	if _, err := CheckEvidence("", path); err != nil {
		t.Fatal(err)
	}
	_, input := reviewParseFixture(t)
	if _, err := ParseReviewEvidence("", reviewParseBytes(t, input)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReviewExpectation("", input.Expectation); err != nil {
		t.Fatal(err)
	}
}
