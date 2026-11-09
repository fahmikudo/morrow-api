// Package migrations embeds the Goose SQL migrations so both the server and
// the test harness can migrate a database without a working-directory guess.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
