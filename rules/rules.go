// Package rules embeds the knowledge-base YAML files into the binary.
package rules

import "embed"

//go:embed *.yaml
var FS embed.FS
