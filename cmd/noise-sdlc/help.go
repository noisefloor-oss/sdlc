package main

import (
	"fmt"
	"strings"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

// commandDoc describes one dispatched command. commandDocs is the single
// source of truth for help output; TestHelpCoversDispatchedCommands parses
// the dispatch switch in main.go and fails if a command is missing here.
type commandDoc struct {
	name     string
	group    string
	summary  string
	synopsis string
	flags    []flagDoc
	reads    string
	writes   string
	requires string
	example  string
	notes    []string
}

type flagDoc struct {
	name     string
	fallback string
	purpose  string
}

var commandDocs = []commandDoc{
	{
		name:     "list",
		group:    "Discovery",
		summary:  "Print the policy seed this tool installs.",
		synopsis: "noise-sdlc list",
		reads:    "the policy seed shipped in this repository (catalog/).",
		writes:   "nothing; JSON to stdout.",
		requires: "the noise-sdlc repository root; read-only.",
		example:  "noise-sdlc list",
	},
	{
		name:     "show",
		group:    "Discovery",
		summary:  "Print a policy seed as JSON.",
		synopsis: "noise-sdlc show [seed-id]",
		reads:    "the policy seed shipped in this repository (catalog/).",
		writes:   "nothing; JSON to stdout.",
		requires: "the noise-sdlc repository root; read-only.",
		example:  "noise-sdlc show noise/agent@0.1",
		notes: []string{
			"seed-id is optional and defaults to noise/agent@0.1, the only",
			"seed in this build. Any other id is an error.",
		},
	},
	{
		name:     "harness",
		group:    "Discovery",
		summary:  "Inspect the harness catalog and discovery candidates.",
		synopsis: "noise-sdlc harness <list|show <id>|interaction|candidates [--target DIR]>",
		flags: []flagDoc{
			{"--target DIR", ".", "project directory used by the candidates subcommand"},
		},
		reads: "catalog/harnesses.json, catalog/harnesses.d/, catalog/harness-candidates.json;\n" +
			"candidates also reads the target's .sdlc/deck-sync.json receipt.",
		writes:   "nothing; JSON to stdout.",
		requires: "the noise-sdlc repository root; read-only.\ncandidates additionally requires a last-known-good Deck receipt in the target.",
		example:  "noise-sdlc harness show cline",
		notes: []string{
			"list prints every cataloged harness with its version ranges.",
			"show <id> prints one full harness descriptor.",
			"interaction prints the interaction contract shared with Deck.",
			"candidates compares a Deck receipt against the catalog and lists",
			"harnesses the Deck knows that this catalog does not.",
		},
	},
	{
		name:     "inventory",
		group:    "Discovery",
		summary:  "Probe which cataloged harnesses are installed locally.",
		synopsis: "noise-sdlc inventory [--target DIR] [--answers FILE]",
		flags: []flagDoc{
			{"--target DIR", ".", "working directory probes run from"},
			{"--answers FILE", "", "answers JSON supplying operator capability declarations"},
		},
		reads: "the harness catalog; harness binaries found in PATH are executed\n" +
			"with their cataloged probes: exact-match version probes (five-second\ntimeout) and, for some harnesses, model-enumeration probes (ten seconds).",
		writes:   "nothing; JSON inventory to stdout, probe progress to stderr.",
		requires: "the noise-sdlc repository root; read-only, but it does execute\ninstalled harness binaries to probe versions and model lists.",
		example:  "noise-sdlc inventory --answers examples/sdlc-answers.json",
	},
	{
		name:     "execution",
		group:    "Discovery",
		summary:  "Plan routes or show adopted invocation guidance.",
		synopsis: "noise-sdlc execution <plan|show> [options]",
		flags: []flagDoc{
			{"--target DIR", ".", "target project"},
			{"--answers FILE", "", "plan only; without answers the planner prompts"},
			{"--role ROLE", "", "show only; select one adopted role"},
			{"--harness-version VERSION", "", "show only; declared version, requires --role"},
			{"--json", "false", "show only; structured guidance"},
		},
		reads: "plan reads policy and answers, inspects the target and executes\n" +
			"cataloged harness probes. If the target contains .git, plan also runs\n" +
			"git rev-parse HEAD there for a read-only revision lookup, with no\n" +
			"explicit timeout. show reads the catalog and bounded managed\n" +
			"project files, verifies their receipt, and never executes a harness.",
		writes:   "nothing; plan emits JSON, show emits text or JSON guidance.",
		requires: "the noise-sdlc repository root; show also requires valid adoption.",
		example:  "noise-sdlc execution show --target . --role review.formal --json",
		notes: []string{
			"show uses adopted bindings and their pinned catalog digest.",
			"A mismatch or unsupported route stays explicit; no model substitution.",
			"Without a version, show lists declared ranges, not an installed match.",
			"A supplied version is a caller declaration, not runtime evidence.",
			"Tasks owns work. The runner enforces permissions and execution authority.",
			"Review owns verdict publication and readback; parsed output is no proof.",
		},
	},
	{
		name:     "review",
		group:    "Discovery",
		summary:  "Build review expectations or compare declared evidence.",
		synopsis: "noise-sdlc review <request --input FILE|compare FILE> [options]",
		flags: []flagDoc{
			{"--input FILE", "", "request only; bounded candidate metadata JSON"},
			{"--target DIR", ".", "request only; verified adopted project"},
		},
		reads: "bounded metadata JSON, its committed schema and the harness catalog.\n" +
			"request also reads verified adopted policy. Evidence references are\n" +
			"metadata only and are never opened; no provider or network calls.",
		writes: "nothing; JSON expectation or comparison to stdout.",
		requires: "request requires a supported adopted formal-review invocation.\n" +
			"compare accepts declarations; a match does not authenticate them.",
		example: "noise-sdlc review request --target . --input candidate.json",
		notes: []string{
			"Inputs identify the exact request, candidate, diff and agent contributors.",
			"Execution, publication and coverage remain separate owner observations.",
			"Missing or mismatched evidence makes compare exit nonzero after JSON.",
			"A match establishes neither approval proof nor merge readiness.",
			"A trusted external gate must validate evidence and current authority.",
			"No automatic repost, task completion or merge follows any result.",
		},
	},
	{
		name:     "bootstrap",
		group:    "Adoption",
		summary:  "Seed one-time agent instructions for planning adoption.",
		synopsis: "noise-sdlc bootstrap [--target DIR]",
		flags: []flagDoc{
			{"--target DIR", ".", "unadopted project to prepare for agent onboarding"},
		},
		reads: "the target's AGENTS.md bytes and lifecycle/bootstrap marker paths. It\n" +
			"does not inspect other project bodies, inventory, credentials, or providers.",
		writes: "a byte-zero onboarding block in AGENTS.md, one-time BOOTSTRAP.md,\n" +
			"and .sdlc/bootstrap.json. Existing AGENTS.md prose is preserved.",
		requires: "an existing, unadopted target directory. Partial, edited, symlinked,\n" +
			"or conflicting bootstrap/lifecycle state is rejected without overwrite.",
		example: "noise-sdlc bootstrap --target /path/to/project",
		notes: []string{
			"Repeating bootstrap is idempotent only while all generated bootstrap",
			"artifacts remain intact. Successful adoption consumes them.",
			"Bootstrap is guidance only; it does not plan or adopt policy, connect",
			"Deck, launch providers, authenticate, or authorize spend.",
		},
	},
	{
		name:     "plan",
		group:    "Adoption",
		summary:  "Print the full adoption plan without writing anything.",
		synopsis: "noise-sdlc plan [--target DIR] [--answers FILE]",
		flags: []flagDoc{
			{"--target DIR", ".", "target project the plan is computed for"},
			{"--answers FILE", "", "answers JSON; without it the planner prompts on stdin"},
		},
		reads:    "the harness catalog, policy seed, and execution template;\nthe answers file when given. Inspects the target directory and\nexecutes installed harness binaries with cataloged version and\nmodel-enumeration probes to build the inventory. If the target contains\n.git, runs git rev-parse HEAD there for a read-only revision lookup,\nwith no explicit timeout.",
		writes:   "nothing; the plan JSON to stdout.",
		requires: "the noise-sdlc repository root; read-only.",
		example:  "noise-sdlc plan --target /path/to/project --answers answers.json",
	},
	{
		name:     "adopt",
		group:    "Adoption",
		summary:  "Install managed lifecycle files into a project.",
		synopsis: "noise-sdlc adopt [--target DIR] [--answers FILE] [--yes]",
		flags: []flagDoc{
			{"--target DIR", ".", "project to adopt into; must already exist"},
			{"--answers FILE", "", "answers JSON; without it the planner prompts on stdin"},
			{"--yes", "false", "actually install; without it adopt prints the plan and stops"},
		},
		reads: "the harness catalog, policy seed, and execution template;\nthe answers file when given. Inspects the target directory and\nexecutes installed harness binaries with cataloged version and\nmodel-enumeration probes to build the inventory. If the target contains\n.git, runs git rev-parse HEAD there for a read-only revision lookup,\nwith no explicit timeout.",
		writes: "with --yes: the managed AGENTS.md block, SDLC.md, .sdlc/project.yaml,\n" +
			"and .sdlc/adoption.json inside the target, then verifies them. A pending\n" +
			"BOOTSTRAP.md and .sdlc/bootstrap.json are consumed after validation.",
		requires: "the noise-sdlc repository root and an existing target directory.",
		example:  "noise-sdlc adopt --target /path/to/project --answers answers.json --yes",
		notes: []string{
			"adopt installs the exact generated bytes shown by plan; nothing is",
			"written unless --yes is passed.",
		},
	},
	{
		name:     "diagnose",
		group:    "Verification",
		summary:  "Explain compatibility separately from artifact integrity.",
		synopsis: "noise-sdlc diagnose [--target DIR]",
		flags:    []flagDoc{{"--target DIR", ".", "project to inspect"}},
		reads:    "bounded active instructions, managed adoption files, recorded Deck receipt;\nretired Tasks paths are stat-only. Integrity also scans project tree names\nfor shadow lifecycle files. Runs noise tasks version, context --json, and\nverify --json with bounded output and five-second timeouts.",
		writes:   "nothing; JSON diagnostics to stdout. No migration, credential inspection,\nprovider launch, inventory discovery or Deck contact.",
		requires: "an existing real target directory; read-only. Works before adoption.",
		example:  "noise-sdlc diagnose --target /path/to/project",
		notes:    []string{"Exit 0 means a report was produced, including when action is required.", "deliveryReadiness remains not-assessed. Recorded runtime facts are dated,", "not live health or capacity observations. Unknown arguments are rejected."},
	},
	{
		name:     "verify",
		group:    "Verification",
		summary:  "Verify adoption artifact integrity (alias: status).",
		synopsis: "noise-sdlc verify [--target DIR]   (alias: status)",
		flags: []flagDoc{
			{"--target DIR", ".", "adopted project to verify"},
		},
		reads:    "the target's AGENTS.md, SDLC.md, .sdlc/project.yaml, and\n.sdlc/adoption.json, checking markers and hashes; also scans the\ntarget tree for shadow lifecycle artifacts.",
		writes:   "nothing; JSON verification result to stdout.",
		requires: "a target previously adopted with noise-sdlc adopt; read-only.",
		example:  "noise-sdlc verify --target /path/to/project",
		notes: []string{
			"status is an alias that additionally prints the execution bindings",
			"recorded in .sdlc/project.yaml.",
			"Scope limits: verify checks only the artifacts noise-sdlc generated",
			"and their digests. It is not conformance evidence, review approval,",
			"security evidence, runtime acceptance, or merge readiness.",
		},
	},
	{
		name:     "remove",
		group:    "Adoption",
		summary:  "Remove a managed adoption from a project.",
		synopsis: "noise-sdlc remove [--target DIR] [--answers FILE] --yes",
		flags: []flagDoc{
			{"--target DIR", ".", "adopted project to strip"},
			{"--answers FILE", "", "accepted for symmetry; not used"},
			{"--yes", "false", "required; removal is destructive and unrecoverable"},
		},
		reads:    "the target's managed artifacts (verified intact before removal).",
		writes:   "deletes the managed AGENTS.md block (or the file, if adopt created it),\nSDLC.md, .sdlc/project.yaml, and .sdlc/adoption.json. Other .sdlc state\nsuch as deck-sync.json is left in place.",
		requires: "a target previously adopted with noise-sdlc adopt.",
		example:  "noise-sdlc remove --target /path/to/project --yes",
	},
	{
		name:     "sync",
		group:    "Deck synchronization",
		summary:  "Exchange Deck sync bundles and receipts.",
		synopsis: "noise-sdlc sync <export|status|accept FILE|pull URL> [--target DIR]",
		flags: []flagDoc{
			{"--target DIR", ".", "adopted project holding the Deck sync state"},
		},
		reads: "export: the catalog and execution template.\n" +
			"status: the target's .sdlc/deck-sync.json.\n" +
			"accept: a Deck receipt file.\n" +
			"pull: a loopback HTTP URL serving a Deck receipt.",
		writes:   "accept and pull write the target's .sdlc/deck-sync.json;\nexport and status write nothing (JSON to stdout).",
		requires: "the noise-sdlc repository root. status works without adoption (reports\ndisabled); accept/pull write the receipt without requiring adoption.",
		example:  "noise-sdlc sync accept deck-receipt.json --target /path/to/project",
		notes: []string{
			"pull accepts loopback HTTP endpoints only (127.0.0.1, ::1, localhost).",
		},
	},
	{
		name:     "onboarding",
		group:    "Adoption",
		summary:  "Report guided-adoption onboarding state for a project.",
		synopsis: "noise-sdlc onboarding status [--target DIR]",
		flags: []flagDoc{
			{"--target DIR", ".", "project to inspect"},
		},
		reads:    "the target's AGENTS.md, SDLC.md, and .sdlc/ state, if present.",
		writes:   "nothing; JSON to stdout.",
		requires: "the noise-sdlc repository root; read-only. Works before adoption\nand reports the not-started state.",
		example:  "noise-sdlc onboarding status --target /path/to/project",
	},
	{
		name:     "web",
		group:    "Adoption",
		summary:  "Serve the guided adoption web UI on loopback.",
		synopsis: "noise-sdlc web [--host HOST] [--port N] [--target DIR] [--answers FILE]",
		flags: []flagDoc{
			{"--host HOST", "127.0.0.1", "bind address; loopback only (127.0.0.1, ::1, localhost)"},
			{"--port N", "8765", "port to listen on"},
			{"--target DIR", ".", "project a web-driven adoption would write to"},
			{"--answers FILE", "", "answers JSON used as the baseline for the UI"},
		},
		reads:    "the harness catalog and execution template; the answers file when\ngiven. At startup it executes installed harness binaries with cataloged\nversion and model-enumeration probes to build the inventory. Each\nplan/adoption request runs git rev-parse HEAD in the target if it\ncontains .git, for a read-only revision lookup with no explicit timeout.",
		writes:   "an adoption confirmed through the UI writes the same managed files as\nadopt --yes; posting a Deck receipt to /api/deck-receipt writes the\ntarget's .sdlc/deck-sync.json.",
		requires: "the noise-sdlc repository root. Runs until interrupted.",
		example:  "noise-sdlc web --port 8765",
	},
	{
		name:     "check-catalog",
		group:    "Verification",
		summary:  "Validate the harness catalog and policy seed.",
		synopsis: "noise-sdlc check-catalog",
		reads:    "catalog/harnesses.json, catalog/harnesses.d/, catalog/harness-candidates.json,\nthe policy seed, and the execution template (which it also validates).",
		writes:   "nothing; JSON result to stdout.",
		requires: "the noise-sdlc repository root; read-only.",
		example:  "noise-sdlc check-catalog",
		notes: []string{
			"Validates descriptor shape, version probes, and vocabulary. Live",
			"certification against installed harnesses is not run.",
		},
	},
	{
		name:     "check-evidence",
		group:    "Verification",
		summary:  "Validate one bounded canary evidence record.",
		synopsis: "noise-sdlc check-evidence FILE",
		reads:    "the named evidence JSON file (up to 1 MiB) and its committed v1/v2 schema.\nSource references are validated as metadata, never opened.",
		writes:   "nothing; JSON validation result to stdout.",
		requires: "the noise-sdlc repository root and an evidence record; read-only.",
		example:  "noise-sdlc check-evidence evidence/sdlc-self.json",
		notes:    []string{"Checks schema, source references, and exclusive elapsed-state totals.", "Validation does not establish observation truth or milestone acceptance.", "No intervention rates or human-labor estimates are computed."},
	},
}

var groupOrder = []string{"Discovery", "Adoption", "Verification", "Deck synchronization"}

// topHelp renders the top-level help listing every command in commandDocs.
func topHelp() string {
	var out strings.Builder
	out.WriteString("noise-sdlc " + sdlc.Version + " — install and verify a software-delivery lifecycle\n")
	out.WriteString("\nUsage:\n  noise-sdlc <command> [flags]\n  noise-sdlc help [command]\n")
	out.WriteString("\nCommands:\n")
	for _, group := range groupOrder {
		out.WriteString("\n  " + group + ":\n")
		for _, doc := range commandDocs {
			if doc.group == group {
				fmt.Fprintf(&out, "    %-14s %s\n", doc.name, doc.summary)
			}
		}
	}
	out.WriteString(`
Global flags:
  --version        print the tool version; add --json for runtime provenance

Every command except help and --version resolves the noise-sdlc repository
root first. Exit codes: 0 on success, 2 when a command rejects its input
or fails. Most commands silently ignore extra unknown arguments.
All structured output is JSON on stdout; progress and prompts go to stderr.

Run 'noise-sdlc help <command>' or 'noise-sdlc <command> --help' for
per-command flags, state reads/writes, and examples.
`)
	return out.String()
}

// commandHelp renders the help for one command; ok is false for unknown names.
func commandHelp(name string) (string, bool) {
	if name == "status" { // status is dispatched as an alias of verify
		name = "verify"
	}
	for _, doc := range commandDocs {
		if doc.name != name {
			continue
		}
		var out strings.Builder
		out.WriteString(doc.name + " — " + doc.summary + "\n\nSynopsis:\n  " + doc.synopsis + "\n")
		if len(doc.flags) > 0 {
			out.WriteString("\nFlags:\n")
			for _, f := range doc.flags {
				fallback := f.fallback
				if fallback == "" {
					fallback = `""`
				}
				fmt.Fprintf(&out, "  %s (default %s)\n      %s\n", f.name, fallback, f.purpose)
			}
		}
		out.WriteString("\nReads:\n  " + indent(doc.reads) + "\n")
		out.WriteString("\nWrites:\n  " + indent(doc.writes) + "\n")
		out.WriteString("\nRequires:\n  " + indent(doc.requires) + "\n")
		out.WriteString("\nExit codes:\n  0 on success, 2 when the command rejects its input or fails.\n  Note: most commands silently ignore extra unknown arguments.\n")
		out.WriteString("\nExample:\n  " + doc.example + "\n")
		if len(doc.notes) > 0 {
			out.WriteString("\nNotes:\n")
			for _, note := range doc.notes {
				out.WriteString("  " + note + "\n")
			}
		}
		return out.String(), true
	}
	return "", false
}

func indent(text string) string {
	return strings.ReplaceAll(text, "\n", "\n  ")
}
