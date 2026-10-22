# Compatibility

This is an experimental release: interfaces, file formats, and behavior may
change between releases without a deprecation cycle. Within that caveat, the
documented surface is:

- **CLI:** the commands and flags printed by `noise-sdlc help` and
  `noise-sdlc help <command>`, including each command's stated reads and
  writes.
- **Exit codes:** 0 on success, 2 when a command rejects its input or fails,
  as documented in the help text. Note that `diagnose` exits 0 whenever it
  produced a report, including when the report requires action.
- **Structured output:** schema coverage is listed below; not every JSON
  output has a schema or a schema version. Progress and prompts go to stderr.
- **Versioned inputs:** the policy seed, harness catalog, and execution
  templates are content-versioned (`noise/agent@0.1`, `noise/harnesses@0.5`,
  `deck/balanced@0.2`); adoptions record the exact digests they installed.
- **Managed file layout:** `SDLC.md`, the managed `AGENTS.md` block, and the
  `.sdlc/` files from `adopt`, verified by digest.

These JSON outputs carry `schemaVersion: 1` and conform to the linked schemas:

| Command output | Schema |
| --- | --- |
| `show` | [seed.schema.json](schemas/seed.schema.json) |
| `plan`, `adopt` (the plan printed to stdout) | [plan.schema.json](schemas/plan.schema.json) |
| `inventory` | [inventory.schema.json](schemas/inventory.schema.json) |
| `diagnose` | [diagnosis.schema.json](schemas/diagnosis.schema.json) |

Some outputs contain schema-covered objects without a schema for the whole
response. `review request` carries `schemaVersion: 1`; its `expectation` object
also carries version 1 and conforms to `$defs/expectation` in
[review-evidence.schema.json](schemas/review-evidence.schema.json).
That schema's root describes the **input** to `review compare`, not either
command's complete output. `sync export` carries `schemaVersion: 1`; its
`catalog` and each `executionTemplates` entry carry version 1 and conform to
[harness-catalog.schema.json](schemas/harness-catalog.schema.json) and
[execution-template.schema.json](schemas/execution-template.schema.json),
respectively. `harness show` emits an unversioned descriptor covered by
`$defs/harness` in the catalog schema.

Receipt schemas describe files, not command summaries:
[bootstrap-receipt.schema.json](schemas/bootstrap-receipt.schema.json)
describes `.sdlc/bootstrap.json`, and
[deck-sync-receipt.schema.json](schemas/deck-sync-receipt.schema.json)
describes the receipt accepted by `sync accept`/`sync pull` and saved as
`.sdlc/deck-sync.json`; both receipts carry `schemaVersion: 1`.
Similarly, [evidence.schema.json](schemas/evidence.schema.json) and
[evidence-v1.schema.json](schemas/evidence-v1.schema.json) describe version
2 and legacy version 1 inputs to `check-evidence`, not its validation summary.

Other command outputs are informational and may change. `--version --json`,
`bootstrap`, `onboarding status`, `execution show --json`, `harness interaction`,
`harness candidates`, `review compare`, and `check-evidence` carry
`schemaVersion: 1` but have no standalone output schemas. Neither do the
complete `review request` and `sync export` responses described above.
`list`, `harness list`, `execution plan`, `check-catalog`, `verify`/`status`,
`sync status`, `sync accept`, `sync pull`, and `remove --yes` have neither
output schemas nor top-level schema versions.

Only the latest tagged release is supported; there are no backports. Linux
x86-64 is the only qualified platform for this release.

