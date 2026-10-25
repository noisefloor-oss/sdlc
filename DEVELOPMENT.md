# Development model

This repository is a **curated release mirror**, not a development repository.

- Source is available under the [MIT license](LICENSE).
- Development happens in a private repository. Each public release lands here
  as one curated snapshot commit with release notes; private development
  history, review discussions, and internal references are not mirrored.
- **Pull requests are disabled.** This project is solo-maintained and is not
  seeking contributors, co-maintainers, or a community governance structure.
  Forking under MIT is of course fine, but forks are not an upstream
  contribution path.
- **Issues are open** and are the supported inbound channel for bug reports
  and feedback. Filing an issue creates no commitment to a fix, a timeline,
  or a response SLA — but reports are read and genuinely useful.
- For security reports, see [SECURITY.md](SECURITY.md) — please do not open a
  public issue for a vulnerability.

## Releases

Cut a release by copying a curated snapshot of private main onto this mirror
and prequalifying that exact commit with a manual run of the release workflow.
A manual run builds Linux amd64 and arm64 binaries on standard GitHub-hosted
runners, checks that each binary reports the source version from
`internal/sdlc/catalog.go` and passes the help and standalone catalog checks,
and keeps the binaries only as workflow artifacts; it never creates or changes
a release. Next, prepare a draft release with curated notes for the intended
tag, then push the matching `v*` tag at the same commit. The tag's version
without `v` and any pre-release suffix must match the source version, or the
build fails. A tag push repeats the same builds and checks, then attaches the
binaries with `SHA256SUMS` only after both architectures pass, preserving
existing notes and draft status. Publish the draft once every asset is attached
and the tag's checks have passed.

## AI assistance

This software is developed with heavy, deliberate use of autonomous coding
agents (Claude Code, Codex, and similar), operating under machine-enforced
review and release gates. A human (Doug Doan) directs the work, owns every
release decision, and is responsible for what ships. Releases are built and
verified from an exact, audited candidate commit before publication.

## Status

This release is **experimental**: it is daily-driven by its author against
real repositories, but interfaces, file formats, and behavior may change
between releases without a deprecation cycle. Compatibility commitments, where
they exist, are stated explicitly in [docs/compatibility.md](docs/compatibility.md).
