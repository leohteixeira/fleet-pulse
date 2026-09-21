// Package migrations embeds goose SQL applied on process start.
package migrations

import "embed"

// FS is the goose SQL directory. Only *.sql files are embedded.
//
//go:embed *.sql
var FS embed.FS
