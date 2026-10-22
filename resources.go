// Package resources contains the immutable runtime data shipped with the CLI.
package resources

import "embed"

// Files preserves the catalog and schema bytes from the source distribution.
//
//go:embed catalog/harnesses.json catalog/harness-candidates.json catalog/harnesses.d/*.json
//go:embed catalog/packs/noise-agent-0.1.json catalog/execution/deck-balanced-0.2.json
//go:embed docs/schemas/harness-catalog.schema.json docs/schemas/review-evidence.schema.json
//go:embed docs/schemas/evidence.schema.json docs/schemas/evidence-v1.schema.json
var Files embed.FS
