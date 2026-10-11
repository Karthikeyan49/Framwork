// Package migrations embeds reviewed platform migrations; never customer metadata SQL.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
