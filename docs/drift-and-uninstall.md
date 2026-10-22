# Drift, repair, and uninstall

## Drift and repair limits

- `verify` checks the exact bytes of the managed `AGENTS.md` block,
  `SDLC.md`, `.sdlc/project.yaml`, and `.sdlc/adoption.json`, and scans the
  project tree for shadow lifecycle files. Any drift exits nonzero and names
  the file. Its scope is those generated artifacts only — see
  [What it does not do](how-it-works.md#what-it-does-not-do).
- `remove --yes` first requires the adoption to verify intact; it refuses to
  delete drifted files, because they may carry your edits.
- Re-running `plan` and `adopt --yes` regenerates the managed files from
  current answers and recorded inputs, which restores integrity after drift.
  Nothing repairs automatically; repair is always a deliberate step you take
  that overwrites the managed files with freshly planned bytes.
- `adopt` refuses to replace files it did not generate, such as a hand-written
  root `SDLC.md`; `bootstrap` fails closed on edited, partial, or conflicting
  bootstrap state. Repeating `bootstrap` is idempotent only while its
  generated artifacts remain intact.
- Adoptions pin the catalog and template they installed by digest. Upgrading
  the `noise-sdlc` binary never rewrites an existing adoption; a newer binary
  reports a stale catalog in `execution show` explicitly instead of
  substituting one.

## Uninstall and data retention

Everything noise-sdlc writes lives inside the target project:

- `adopt`: the managed `AGENTS.md` block (or the whole file, if it created
  it), `SDLC.md`, `.sdlc/project.yaml`, `.sdlc/adoption.json`
- `bootstrap`: the `AGENTS.md` pointer block, `BOOTSTRAP.md`,
  `.sdlc/bootstrap.json` (consumed by a successful `adopt`)
- `sync accept` / `sync pull` / the web UI: `.sdlc/deck-sync.json`

To remove an adoption, run `noise-sdlc remove --target PROJECT --yes`, which
deletes the managed files and reports the removal as unrecoverable. The
`.sdlc/deck-sync.json` receipt is left in place; delete it by hand if you
also want sync state gone. To uninstall the tool, remove the adoption from
each project with `remove`, then delete the binary and the source clone. There is
no global state: no configuration directories, no accounts, no telemetry, and
nothing written outside the target project.

