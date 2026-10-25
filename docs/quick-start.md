# Quick start in detail

The CLI's own writes below stay inside the scratch project, and only
`adopt --yes` writes managed files. `plan` and `adopt` also execute installed
harness binaries on your PATH for the catalog's bounded version and model-list
probes; those binaries may perform their own filesystem or network I/O.

```sh
# Validate the embedded catalog and policy seed (read-only)
noise-sdlc check-catalog

# Create a scratch project and a minimal answers file
mkdir -p /tmp/sdlc-demo && cd /tmp/sdlc-demo
cat > answers.json <<'EOF'
{
  "schemaVersion": 1,
  "seed": "noise/agent@0.2",
  "executionTemplate": "deck/balanced@0.2",
  "answers": {
    "authorship": "agent",
    "delivery": "software",
    "risk": "ordinary",
    "routingPreference": "balanced",
    "highRiskClasses": []
  },
  "operatorCapabilities": [],
  "projectRules": []
}
EOF

# Print the full adoption plan; writes nothing
noise-sdlc plan --target . --answers answers.json

# Install the managed lifecycle files
noise-sdlc adopt --target . --answers answers.json --yes

# Re-check the installed bytes at any time
noise-sdlc verify --target .

# Sync state: standalone and disabled until you opt into Deck integration
noise-sdlc sync status --target .
```

`operatorCapabilities` is where you declare the harness/model pairs you
actually have, for example
`{"harness": "omp", "model": "zai/glm-5.3", "entitlement": "included", "allowUnverified": true}`
(a harness/model pair from `noise-sdlc harness show omp`).
The answers format is defined by
[answers.schema.json](schemas/answers.schema.json). Without
`--answers`, `plan` and `adopt` ask the interview questions on stdin.

The command surface, grouped as in `noise-sdlc help`:

- **Discovery:** `list`, `show`, `harness`, `inventory`, `execution`,
  `review`
- **Adoption:** `bootstrap`, `plan`, `adopt`, `remove`, `onboarding`, `web`
- **Verification:** `diagnose`, `verify` (alias `status`), `check-catalog`,
  `check-evidence`
- **Deck synchronization:** `sync`

`bootstrap` is the agent-first entrypoint for a new repository: it adds a
small pointer block to `AGENTS.md` plus a one-time `BOOTSTRAP.md` that a later
`adopt` consumes. `web` serves the same guided adoption as a loopback browser
UI. Run `noise-sdlc help <command>` for per-command flags, exactly which
files each command reads and writes, and examples.

