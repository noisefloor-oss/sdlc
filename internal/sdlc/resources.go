package sdlc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	resources "github.com/noisefloor-oss/sdlc"
)

// An empty root selects bundled resources. A source root remains authoritative:
// missing or invalid files there must never fall back to the bundled copy.
func readResource(root, path string) ([]byte, error) {
	if root == "" {
		return resources.Files.ReadFile(filepath.ToSlash(path))
	}
	return os.ReadFile(path)
}

func globResources(root, pattern string) ([]string, error) {
	if root == "" {
		return fs.Glob(resources.Files, filepath.ToSlash(pattern))
	}
	return filepath.Glob(pattern)
}

func loadResourceJSON(root, path string, destination any) error {
	data, err := readResource(root, path)
	if err != nil {
		return fmt.Errorf("cannot read valid JSON from %s: %w", path, err)
	}
	return decodeJSON(path, data, destination)
}

func readEvidenceResource(root, path string) ([]byte, error) {
	if root == "" {
		return readResource(root, path)
	}
	return readEvidenceFile(path)
}
