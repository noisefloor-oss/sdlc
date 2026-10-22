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

Cut a release by copying a curated snapshot of private main onto this mirror,
preparing a draft release with curated notes for the intended tag, then pushing
the matching `v*` tag. The tag's version without `v` must match the CLI's source
version. The release workflow builds Linux amd64 and arm64 binaries and attaches
them with `SHA256SUMS`, preserving existing notes and draft status; publish the
draft when the assets are ready.

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
they exist, are stated explicitly in the README.
