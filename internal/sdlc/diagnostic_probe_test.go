package sdlc

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticProbeProcess(t *testing.T) {
	switch os.Args[len(os.Args)-1] {
	case "diagnostic-separated-output":
		fmt.Fprint(os.Stdout, `{"public":"result"}`)
		fmt.Fprint(os.Stderr, "unstructured provider detail")
		os.Exit(0)
	case "diagnostic-oversized-stdout":
		fmt.Fprint(os.Stdout, strings.Repeat("x", diagnosticLimit+1))
		os.Exit(0)
	case "diagnostic-oversized-stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", diagnosticLimit+1))
		os.Exit(0)
	case "diagnostic-timeout":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "diagnostic-stdin":
		data, _ := io.ReadAll(os.Stdin)
		if len(data) != 0 {
			os.Exit(8)
		}
		os.Exit(0)
	}
}

func TestDiagnosticProbeSeparatesAndBoundsOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ mode, failure string }{
		{"diagnostic-separated-output", ""},
		{"diagnostic-oversized-stdout", "oversized"},
		{"diagnostic-oversized-stderr", "oversized"},
		{"diagnostic-stdin", ""},
		{"diagnostic-timeout", "timeout"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			start := time.Now()
			data, failure := runDiagnosticProbe(t.TempDir(), []string{executable, "-test.run=^TestDiagnosticProbeProcess$", "--", test.mode})
			if failure != test.failure {
				t.Fatalf("failure=%q, want %q", failure, test.failure)
			}
			if failure != "" && len(data) != 0 {
				t.Fatal("failed probe exposed partial data")
			}
			if test.mode == "diagnostic-separated-output" && string(data) != `{"public":"result"}` {
				t.Fatal("stderr contaminated stdout")
			}
			if time.Since(start) > 8*time.Second {
				t.Fatal("probe exceeded bounded completion window")
			}
		})
	}
}
