// Package migrations embeds golang-migrate SQL files so the server and test
// harness can migrate a database without a working-directory guess.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
