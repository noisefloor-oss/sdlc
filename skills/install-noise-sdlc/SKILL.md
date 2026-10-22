---
name: install-noise-sdlc
description: Install or upgrade noise-sdlc, the CLI that installs a curated software-delivery lifecycle into a project as managed files. Use when the user asks to install, update or reinstall noise sdlc.
---

# Install noise-sdlc

## What this is

Install or upgrade noise-sdlc vX.Y.Z+ on the user's Linux host from the
published release binary of
[github.com/noisefloor-oss/sdlc](https://github.com/noisefloor-oss/sdlc)
(releases page:
`https://github.com/noisefloor-oss/sdlc/releases`). The agent runs the checks
and commands; the human owns every adoption decision. Installing the tool
never adopts anything into a project — adoption is a separate, user-gated
step below.

## Read this to the human first

noise-sdlc is a standalone CLI with no global state: everything it writes
stays inside the target project, and `adopt --yes` writes (and later
regenerates) the managed `AGENTS.md` block, `SDLC.md` and `.sdlc/` files.
Host requirements, each with its refusal:

- Linux (`uname -s` prints `Linux`; anything else → stop).
- `uname -m` printing `x86_64` selects the `amd64` asset — x86-64 is the only
  platform this experimental release is qualified on. `aarch64` selects
  `arm64`, which is built and shipped but unqualified; say so and let the
  user decide before continuing.

No root and no Go toolchain are needed: the release ships a static,
self-contained binary.

## Never

- Run `sudo` or pass a password.
- Bypass a failed checksum, or fall back to a different asset, tag or
  artifact after a failure.
- Run `adopt --yes` (or `bootstrap`) against a project the user has not
  confirmed, or without showing them the `plan` output first.
- Write outside the scratch download directory, `~/.local/bin`, and the
  target project the user names.
- Edit the user's shell rc files to fix `PATH` — tell them the line instead.

## Install

1. Resolve the release: list the tags with
   `git ls-remote --tags --refs https://github.com/noisefloor-oss/sdlc.git 'refs/tags/v*'`,
   choose the highest stable `vX.Y.Z` (exclude drafts and pre-releases), and
   peel it for the commit:
   `git ls-remote https://github.com/noisefloor-oss/sdlc.git 'refs/tags/vX.Y.Z^{}'`
   returns the peeled commit for annotated tags; when that answers nothing,
   the plain `git ls-remote ... refs/tags/vX.Y.Z` line is the commit. Read the
   release notes on the releases page for the tag.
2. Download into a fresh scratch directory (`mktemp -d`), from that tag's
   release only — the assets are
   `https://github.com/noisefloor-oss/sdlc/releases/download/vX.Y.Z/noise-sdlc-linux-ARCH`
   and `.../SHA256SUMS`:
   `curl -fLO` both files with the directory as the working directory.
3. Verify: SHA256SUMS covers every architecture, so check only the asset you
   took — `grep 'noise-sdlc-linux-ARCH' SHA256SUMS | sha256sum -c -`.
   Mismatch → hard stop; report the exact failing file. Clean the scratch
   directory up afterwards.
4. Install: `mkdir -p ~/.local/bin` first (or use `install -D`), then
   `install -m 0755 noise-sdlc-linux-ARCH ~/.local/bin/noise-sdlc`. Check
   `command -v noise-sdlc`; if it does not resolve, tell the user the line
   their shell needs (`~/.local/bin` on `PATH`) rather than editing any rc
   file yourself.
5. Confirm: `noise-sdlc --version` prints the release's `X.Y.Z`, and
   `noise-sdlc check-catalog` validates the embedded catalog read-only.

## Adoption (user-gated, optional)

Installing the tool adopts nothing. If the user wants the lifecycle in a
project, run `noise-sdlc plan --target DIR --answers FILE` (or the interview
form), show them the plan, and only on their explicit go-ahead run
`noise-sdlc adopt --target DIR --answers FILE --yes`. Without `--answers`,
`plan` and `adopt` ask the interview questions on stdin; the answers format
is `docs/schemas/answers.schema.json` in the repository.

## Upgrade

The same steps at the newer tag. Existing adoptions pin the catalog and
templates they installed; a newer binary never rewrites them — it reports
staleness instead. `noise-sdlc remove --target PROJECT --yes` uninstalls an
adoption and is irreversible (the tool reports `recoverable: false`); the
user runs it deliberately. Deleting the binary removes the tool — there is
no global state.

## If anything fails

Stop and report the exact failing check to the user. Never bypass a check and
never fall back to a different artifact, tag or commit to get past it.
