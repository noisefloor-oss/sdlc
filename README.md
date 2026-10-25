# Noisefloor SDLC

SDLC gives coding agents a shared set of rules for taking work from a task
to a release. It records how changes should be implemented, checked and
reviewed, so each new agent session can follow the same process.

SDLC proposes a delivery policy based on the approach you choose for your
project, then installs it as instructions after you accept the plan. It can
also map implementation and review roles to the agents and models you use,
keeping the reviewer in a different model family from the implementer.

## Consistent rules as the project grows

SDLC stands for software development lifecycle, and the tool turns decisions
about that process into instructions you and your agents can read. It then
checks whether those managed files have changed since they were installed.
You can inspect a proposed update before adopting it.

Agents, tests and review tools still have to carry out those rules;
SDLC's verification checks whether the instructions are intact, without
running the tools or deciding that a change is ready to ship.

**Status: experimental.** Daily-driven by its author; interfaces may change
between releases.

## Part of the Noisefloor workflow

[Deck](https://github.com/noisefloor-oss/deck) brings projects, tasks and
agent sessions into a browser workspace for building products with AI.
[Tasks](https://github.com/noisefloor-oss/tasks) manages the work and its
working copies, and [Visual Diff](https://github.com/noisefloor-oss/visual-diff)
gives agents screenshot feedback as they implement a design. SDLC provides
the delivery rules that describe how the work should be checked and reviewed.

You can also adopt SDLC's rules into a project on their own, without
using Deck or the other Noisefloor tools.

## Example

The plan shows what will change, with no files written until you adopt it.

```sh
noise-sdlc plan --target . --answers answers.json    # explained plan, writes nothing
noise-sdlc adopt --target . --answers answers.json --yes
# Adopted; managed artifact integrity verified.
noise-sdlc verify --target .
# { "integrity": "verified", "seed": "noise/agent@0.2", ... }
```

`adopt` writes a managed block in `AGENTS.md`, `SDLC.md`, and two files under
`.sdlc/`. `verify` re-checks their exact bytes whenever you ask. The
[quick start](docs/quick-start.md) has the answers file and the full command
list.

## Install

Give your coding agent this prompt:

> Install noise-sdlc from its latest release, following
> <https://github.com/noisefloor-oss/sdlc/blob/main/skills/install-noise-sdlc/SKILL.md>.

Requirements: Linux on x86-64 and Go 1.26.10 or newer.

```sh
go install github.com/noisefloor-oss/sdlc/cmd/noise-sdlc@v0.4.1
noise-sdlc --version
```

The binary embeds everything it needs, so it runs from any directory. Release
archives with SHA-256 checksums are on GitHub Releases.

## Learn more

- [How it works, and what it does not do](docs/how-it-works.md)
- [Quick start in detail](docs/quick-start.md)
- [Drift, repair, and uninstall](docs/drift-and-uninstall.md)
- [Deck integration](docs/deck-integration.md)
- [Compatibility and output schemas](docs/compatibility.md)
- [Building and testing from source](docs/building.md)
- [Development model](DEVELOPMENT.md) and [security policy](SECURITY.md)

## License

MIT © 2026 Doug Doan. Built at Noisefloor. See [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
