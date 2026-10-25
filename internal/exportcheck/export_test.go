// Archive hygiene gate for the public source export.
//
// The public release snapshot is `git archive HEAD`, curated by the
// export-ignore rules in .gitattributes. These tests scan the actual archive
// payload — not the working tree — and fail when:
//
//   - an export-ignored path is present in the payload;
//   - a required runtime input is missing from the payload;
//   - a shipped Markdown file links to an excluded or nonexistent path; or
//   - shipped text carries private process vocabulary: numbered task or
//     pull-request citations, requirement identifiers, private project or
//     host paths, or non-example email addresses.
//
// The archive gate is opt-in; run it with:
//
//	SDLC_EXPORT_CHECK=1 go test -run Export ./internal/exportcheck/
//
// Archive tests skip (never fail) outside a git checkout: an extracted archive
// has no .git, so product tests keep passing in the exported tree. Patterns
// are assembled from fragments so this file's own source never matches them;
// the file is also excluded from the scan by name as a second guard.
package exportcheck

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// self is this file's archive path; its pattern fragments are never scanned.
const self = "internal/exportcheck/export_test.go"

// Excluded paths mirror the export-ignore rules in .gitattributes. Keep the
// two in sync.
var excludedDirs = []string{".tasks", ".sdlc", "docs/research", "docs/adr"}

var excludedFiles = []string{
	".gitattributes",
	"AGENTS" + ".md",
	"SDLC" + ".md",
	"PROPOSAL" + ".md",
	"docs/" + "NORTH_STAR" + ".md",
	"docs/" + "incubation-scorecard" + ".md",
	"docs/" + "deck-sync-contract" + ".md",
	"docs/" + "guided-adoption-contract" + ".md",
	"docs/" + "harness-catalog-contract" + ".md",
	"docs/" + "invocation-contracts" + ".md",
	"docs/" + "review-evidence-contract" + ".md",
}

// Runtime inputs and public documents that must survive the export.
var requiredFiles = []string{
	"go.mod",
	"LICENSE",
	"README.md",
	"CONTRIBUTING.md",
	"SECURITY.md",
	"DEVELOPMENT.md",
	"THIRD_PARTY_NOTICES.md",
	"skills/install-noise-sdlc/SKILL.md",
	".github/ISSUE_TEMPLATE/config.yml",
	"bin/noise-sdlc",
	"catalog/harnesses.json",
	"catalog/harness-candidates.json",
	"catalog/packs/noise-agent-0.1.json",
	"catalog/packs/noise-agent-0.2.json",
	"catalog/execution/deck-balanced-0.2.json",
	"docs/schemas/harness-catalog.schema.json",
	"docs/schemas/evidence.schema.json",
	"examples/sdlc-answers.json",
	"evidence/sdlc-self.json",
	"cmd/noise-sdlc/main.go",
	"internal/sdlc/catalog.go",
}

type marker struct {
	name string
	re   *regexp.Regexp
	// except lists archive paths exempt from this marker, with the reason
	// stated where the marker is registered.
	except map[string]bool
}

// privateMarkers matches private process vocabulary in shipped text.
//
// Managed output names such as AGENTS.md, SDLC.md, .sdlc/, and .tasks/ are
// product concepts (the tool writes them into target projects), so they are
// covered by the dangling-link test only, never by literal matching here.
var privateMarkers = []marker{
	{"numbered task citation", regexp.MustCompile(`(?i)\b` + "task" + `[ -]?#?\d+[a-z]?\b`), nil},
	{"pull-request citation", regexp.MustCompile(`\b` + "PR" + `\s*#?\d+\b|\(#\d+\)`), nil},
	{"requirement identifier", regexp.MustCompile(`\b(?:` + "FR" + `|` + "NFR" + `)-[A-Z0-9.-]+`), nil},
	{"private project name", regexp.MustCompile("noisefloor" + `-app|` + "research" + `-agent|` + "agent" + `-console`), nil},
	{"private host path", regexp.MustCompile(`/work` + `space/`), nil},
	{"private planning citation", regexp.MustCompile(`(?i)\bprivate\s+(?:` + `planning|research|review)\s+(?:records|notes)\b`), nil},
	{"excluded research tree", regexp.MustCompile(`docs/` + `research/|` + `docs/` + "NORTH_STAR" + `\.md`), nil},
	{"private charter citation", regexp.MustCompile(`\b` + "PROPOSAL" + `\.md\b`), map[string]bool{
		// The versioned policy pack cites the charter as rule provenance.
		// Pack bytes are a checksummed input to generated policy and cannot
		// be reworded without changing recorded digests.
		"catalog/packs/noise-agent-0.1.json": true,
	}},
}

func TestPrivatePlanningMarker(t *testing.T) {
	var re *regexp.Regexp
	for _, m := range privateMarkers {
		if m.name == "private planning citation" {
			re = m.re
		}
	}
	if re == nil {
		t.Fatal("private planning citation marker is missing")
	}
	for _, text := range []string{"private " + "planning records", "Private " + "Review Notes", "private " + "research records", "private\n" + "planning records"} {
		if !re.MatchString(text) {
			t.Errorf("missed private citation: %q", text)
		}
	}
	for _, text := range []string{"public review records", "do not store private transcripts", "request-round-2", "after two failed review rounds"} {
		if re.MatchString(text) {
			t.Errorf("rejected standalone text or fixture identity: %q", text)
		}
	}
}

var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// reservedExampleHost reports whether a host is reserved for documentation or
// fixtures (RFC 2606 and friends), where an address can never be a real one.
func reservedExampleHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	for _, suffix := range []string{".test", ".example", ".invalid", ".localhost"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	for _, domain := range []string{"example.com", "example.org", "example.net"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func excluded(name string) bool {
	for _, dir := range excludedDirs {
		if name == dir || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	for _, file := range excludedFiles {
		if name == file {
			return true
		}
	}
	return false
}

// archivePayload returns the member names and regular-file contents of
// `git archive HEAD`. It skips the calling test when the gate is off or the
// tree is not a git checkout.
func archivePayload(t *testing.T) ([]string, map[string][]byte) {
	t.Helper()
	if os.Getenv("SDLC_EXPORT_CHECK") != "1" {
		t.Skip("archive hygiene gate is opt-in: SDLC_EXPORT_CHECK=1 go test -run Export ./internal/exportcheck/")
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not a git checkout; an extracted archive has no .git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	cmd := exec.Command("git", "archive", "HEAD")
	cmd.Dir = root
	archive, err := cmd.Output()
	if err != nil {
		t.Fatalf("git archive HEAD: %v", err)
	}
	var names []string
	files := map[string][]byte{}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading archive: %v", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		names = append(names, name)
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(io.LimitReader(reader, 8<<20))
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			files[name] = data
		}
	}
	return names, files
}

func TestExportExcludesInternalPaths(t *testing.T) {
	names, _ := archivePayload(t)
	for _, name := range names {
		if excluded(name) {
			t.Errorf("excluded path present in archive: %s", name)
		}
	}
}

func TestExportKeepsRuntimeInputs(t *testing.T) {
	_, files := archivePayload(t)
	for _, name := range requiredFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("required file missing from archive: %s", name)
		}
	}
}

var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// TestExportLinksResolve rejects dangling pointers: a shipped document must
// not link to an excluded path (it would not exist for a public reader) or to
// any path missing from the payload.
func TestExportLinksResolve(t *testing.T) {
	_, files := archivePayload(t)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		for _, match := range markdownLink.FindAllStringSubmatch(string(files[name]), -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := path.Clean(path.Join(path.Dir(name), target))
			if excluded(resolved) {
				t.Errorf("%s links to excluded path %s", name, target)
			} else if _, ok := files[resolved]; !ok {
				t.Errorf("%s links to missing path %s", name, target)
			}
		}
	}
}

func TestExportHasNoPrivateMarkers(t *testing.T) {
	_, files := archivePayload(t)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var findings []string
	for _, name := range names {
		data := files[name]
		if name == self || !utf8.Valid(data) {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, m := range privateMarkers {
			if m.except[name] {
				continue
			}
			// Scan whole files so line wrapping cannot hide a citation.
			for _, match := range m.re.FindAllIndex(data, -1) {
				line := bytes.Count(data[:match[0]], []byte("\n"))
				findings = append(findings, fmt.Sprintf("%s:%d: %s: %s", name, line+1, m.name, preview(lines[line])))
			}
		}
		for i, line := range lines {
			location := fmt.Sprintf("%s:%d", name, i+1)
			for _, address := range emailPattern.FindAllString(line, -1) {
				host := address[strings.LastIndex(address, "@")+1:]
				if !reservedExampleHost(host) {
					findings = append(findings, fmt.Sprintf("%s: email address: %s", location, address))
				}
			}
		}
	}
	for _, finding := range findings {
		t.Log(finding)
	}
	if len(findings) > 0 {
		t.Errorf("%d private markers in the archive payload (listed above)", len(findings))
	}
}

func preview(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		line = line[:120] + "..."
	}
	return line
}
