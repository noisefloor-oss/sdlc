package sdlc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const MaxReviewEvidenceBytes = 1 << 20

// ParseReviewEvidence validates the exact input against the committed schema
// before typed decoding. Evidence references are metadata only and never opened.
// Request associations are caller declarations, not authenticated runner facts.
func ParseReviewEvidence(repositoryRoot string, data []byte) (ReviewEvidenceInput, error) {
	var value ReviewEvidenceInput
	if err := validateReviewEvidenceJSON(repositoryRoot, data, "", &value); err != nil {
		return ReviewEvidenceInput{}, err
	}
	return value, nil
}

// ValidateReviewExpectation checks builder output under the same required-field
// contract as comparison input. It does not establish the candidate or policy's
// authority; the builder separately obtains a verified adoption snapshot.
func ValidateReviewExpectation(repositoryRoot string, value ReviewExpectation) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("cannot encode review expectation")
	}
	var validated ReviewExpectation
	return validateReviewEvidenceJSON(repositoryRoot, data, "#/$defs/expectation", &validated)
}

func validateReviewEvidenceJSON(repositoryRoot string, data []byte, fragment string, destination any) error {
	if err := checkReviewJSON(data); err != nil {
		return err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid review evidence JSON")
	}
	schema, err := compileReviewEvidenceSchema(repositoryRoot, fragment)
	if err != nil {
		return err
	}
	if err := schema.Validate(document); err != nil {
		// Do not echo caller-supplied values or source locators in diagnostics.
		return errors.New("review evidence does not satisfy the declared schema")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		// Typed uint64 decoding also rejects exponent/fraction notation rather
		// than allowing an identifier to pass through floating-point conversion.
		return errors.New("review evidence contains invalid typed values")
	}
	return nil
}

func compileReviewEvidenceSchema(repositoryRoot, fragment string) (*jsonschema.Schema, error) {
	path := filepath.Join(repositoryRoot, "docs", "schemas", "review-evidence.schema.json")
	data, err := readEvidenceResource(repositoryRoot, path)
	if err != nil {
		return nil, errors.New("review evidence schema is unavailable")
	}
	if err := checkReviewJSON(data); err != nil {
		return nil, errors.New("review evidence schema is invalid")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("review evidence schema is invalid")
	}
	compiler := jsonschema.NewCompiler()
	// Empty transport registry: references cannot fetch a remote schema or
	// open an external file. Only this resource and bundled metaschemas resolve.
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	const resource = "https://noise.invalid/sdlc/review-evidence.schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, errors.New("review evidence schema is invalid")
	}
	schema, err := compiler.Compile(resource + fragment)
	if err != nil {
		return nil, errors.New("review evidence schema cannot be compiled locally")
	}
	return schema, nil
}

// checkReviewJSON catches duplicate decoded property names, trailing values,
// invalid UTF-8 and excessive nesting before a map decoder can erase them.
func checkReviewJSON(data []byte) error {
	if len(data) == 0 || len(data) > MaxReviewEvidenceBytes {
		return errors.New("review evidence exceeds the supported byte bound or is empty")
	}
	if !utf8.Valid(data) {
		return errors.New("review evidence is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanReviewJSONValue(decoder, 0); err != nil {
		return fmt.Errorf("invalid review evidence JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("review evidence contains trailing JSON")
	}
	return nil
}

func scanReviewJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("nesting exceeds the supported bound")
	}
	token, err := decoder.Token()
	if err != nil {
		return errors.New("malformed value")
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok {
				return errors.New("malformed object property")
			}
			if keys[key] {
				return errors.New("duplicate object property")
			}
			keys[key] = true
			if err := scanReviewJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for decoder.More() {
			if err := scanReviewJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return errors.New("unexpected delimiter")
	}
	return nil
}
