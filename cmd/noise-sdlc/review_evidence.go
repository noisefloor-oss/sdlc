package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func reviewEvidenceCommand(root string, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("review request or compare is required")
	}
	switch arguments[0] {
	case "request":
		options := flag.NewFlagSet("review request", flag.ContinueOnError)
		target := options.String("target", ".", "adopted project")
		inputPath := options.String("input", "", "candidate metadata file")
		if err := options.Parse(arguments[1:]); err != nil {
			return err
		}
		if *inputPath == "" || options.NArg() != 0 {
			return errors.New("usage: review request --input FILE [--target DIR]")
		}
		data, err := sdlc.ReadReviewInput(*inputPath)
		if err != nil {
			return err
		}
		input, err := sdlc.ParseReviewRequestInput(data)
		if err != nil {
			return err
		}
		expectation, err := sdlc.BuildReviewExpectation(root, *target, input)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"schemaVersion": 1, "kind": "noise.sdlc.review-expectation", "expectation": expectation,
			"candidateIdentity": "caller-declared", "executionAuthority": "not-granted",
		})
	case "compare":
		if len(arguments) != 2 || arguments[1] == "" || arguments[1][0] == '-' {
			return errors.New("usage: review compare FILE")
		}
		data, err := sdlc.ReadReviewInput(arguments[1])
		if err != nil {
			return err
		}
		input, err := sdlc.ParseReviewEvidence(root, data)
		if err != nil {
			return err
		}
		catalog, err := sdlc.LoadCatalog(root)
		if err != nil {
			return err
		}
		comparison, err := sdlc.CompareReviewEvidence(catalog, input.Expectation, input.Observations)
		if err != nil {
			return err
		}
		if err := emit(comparison); err != nil {
			return err
		}
		if comparison.Status != "match" {
			return fmt.Errorf("review evidence comparison is %s", comparison.Status)
		}
		return nil
	default:
		return errors.New("review request or compare is required")
	}
}
