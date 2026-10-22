# What noise-sdlc does and does not do

## What it does

Projects that run coding agents need lifecycle policy: which rules govern
changes, which harnesses and models may fill implementation and review roles,
and how drift in those decisions is detected. Copying another project's
governance files rots quickly. noise-sdlc instead ships a tested policy seed
(`noise/agent@0.1`), a harness catalog (`noise/harnesses@0.5`), and an
execution template (`deck/balanced@0.2`), and installs them into your project
as managed files whose exact bytes are recorded.

The default path is a short, opinionated interview (or a plain answers JSON
file) that produces one explained plan. `adopt --yes` installs exactly the
planned bytes: a managed `AGENTS.md` block, `SDLC.md`,
`.sdlc/project.yaml`, and `.sdlc/adoption.json`. `verify` re-checks those
bytes at any time; `diagnose` explains artifact integrity separately from
compatibility with neighboring tools; `remove` uninstalls.

The harness side is a closed catalog of tested CLI descriptors. `inventory`
probes installed harnesses using only the cataloged version and
model-enumeration commands (five- and ten-second timeouts). `execution plan`
resolves bulk, standard, high-risk, formal-review, and repair routes against
the capabilities you declare, explains each choice, and enforces reviewer
model-family independence. Declaring a capability is your statement about
what you have installed and are entitled to use; an empty set is valid and
leaves routes unresolved rather than inventing ones you cannot run.

## What it does not do

- **It does not start harness sessions or call models.** Execution bindings
  are routing policy only; they do not authenticate or authorize spend.
  `inventory`, `plan`, `adopt`, `execution plan`, and `web` (at startup)
  execute installed harness binaries for the catalog's version and model-list
  probes, including discovery-candidate version probes, with five- or
  ten-second timeouts. `plan`, `adopt`, `execution plan`, and `web` (on each
  plan/adoption request) run `git rev-parse HEAD` in targets containing `.git`
  for a read-only revision lookup, with no explicit timeout. `diagnose` runs
  `noise tasks version`, `noise tasks context --json`, and
  `noise tasks verify --json` with five-second timeouts. All run in the target
  project; those binaries may perform their own filesystem or network I/O.
- **It handles no credentials.** It reads no credential configuration,
  environment secrets, or authentication output, and exchanges no account,
  quota, or balance data in any state, synchronized or standalone.
- **It is not a scheduler, sandbox, or review authority.** It does not run
  your pipeline, enforce permissions at execution time, or publish review
  verdicts. `review request`/`review compare` derive expectations and compare
  declared evidence; a match is neither authenticated approval nor merge
  readiness.
- **Integrity is not conformance.** A passing `verify` means the managed
  files match their recorded digests. It is not lifecycle conformance, review
  approval, security evidence, runtime acceptance, or merge readiness.
- **The tool itself opens network connections only on loopback, on request.**
  `web` listens only on loopback and `sync pull` connects only to loopback
  endpoints. Schema validation resolves references
  within the loaded document and the bundled metaschemas; it never fetches
  from the network or filesystem.

