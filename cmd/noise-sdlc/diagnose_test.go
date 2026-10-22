package main

import (
	"encoding/json"
	"testing"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

func TestDiagnoseProducesReportWhenSpecialistIsUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	output, err := captureStdout(t, func() error { return run([]string{"diagnose", "--target", t.TempDir()}) })
	if err != nil {
		t.Fatalf("a diagnostic finding must not fail report production: %v", err)
	}
	var result sdlc.Diagnosis
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Integrity != "not-adopted" || result.DeliveryReadiness != "not-assessed" {
		t.Fatalf("unexpected report: %#v", result)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "tasks-unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing specialist availability finding")
	}
}

func TestDiagnoseRejectsUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"diagnose", "--repair"}, {"diagnose", "extra"}} {
		if err := run(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
