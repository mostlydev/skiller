package skiller

import "embed"

// EmbeddedFS is the source of truth for M0 data and schema contracts.
//
// data/*.toml is the human-editable catalog consumed by the planner. schema/*.json
// is the language-neutral contract that downstream tools can validate against.
//
//go:embed data/*.toml schema/*.schema.json
var EmbeddedFS embed.FS
