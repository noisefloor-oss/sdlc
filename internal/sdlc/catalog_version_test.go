package sdlc

import "testing"

func TestMatchingRangeRejectsNonSemanticVersions(t *testing.T) {
	harness := Harness{Ranges: []VersionRange{{Minimum: "1.0.0", MaximumExclusive: "10.0.0"}}}
	if got := matchingRange(harness, "9.9.9"); got == nil {
		t.Fatal("semantic version inside the range did not match")
	}
	for _, version := range []string{"9.9.9-rc", "1.5.0-rc", "nightly", "01.5.0", "1.05.0", "1.5.00"} {
		if got := matchingRange(harness, version); got != nil {
			t.Errorf("non-semantic version %q matched a certified range", version)
		}
	}
}
