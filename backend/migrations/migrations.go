package migrations

import "embed"

// Files contains immutable database migrations.
//
//go:embed *.sql
var Files embed.FS
