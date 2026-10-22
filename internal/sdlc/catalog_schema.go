package sdlc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func compileCatalogSchemas(root string) (*jsonschema.Schema, *jsonschema.Schema, error) {
	path := filepath.Join(root, "docs", "schemas", "harness-catalog.schema.json")
	data, err := readResource(root, path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read catalog schema %s: %w", path, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid catalog schema %s: %w", path, err)
	}
	compiler := jsonschema.NewCompiler()
	// Only the explicitly loaded document and bundled metaschemas may resolve.
	// Catalog checking never fetches remote schemas or reads referenced files.
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	const resource = "https://noise.invalid/sdlc/harness-catalog.schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, nil, fmt.Errorf("invalid catalog schema %s: %w", path, err)
	}
	base, err := compiler.Compile(resource)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid catalog schema %s: %w", path, err)
	}
	harness, err := compiler.Compile(resource + "#/$defs/harness")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid harness schema %s: %w", path, err)
	}
	return base, harness, nil
}

func loadSchemaJSON(root, path string, schema *jsonschema.Schema, destination any) error {
	data, err := readResource(root, path)
	if err != nil {
		return fmt.Errorf("cannot read valid JSON from %s: %w", path, err)
	}
	// Validate the original document before typed decoding can collapse absent
	// required-but-nullable properties into their Go zero values. Both passes
	// consume the same bytes, including the check for trailing JSON values.
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot read valid JSON from %s: %w", path, err)
	}
	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("catalog schema violation in %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("cannot decode catalog from %s: %w", path, err)
	}
	return nil
}
