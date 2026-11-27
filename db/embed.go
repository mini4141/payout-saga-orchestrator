// Package db embeds the goose migrations.
package db

import "embed"

// Migrations holds the SQL files under migrations/.
//
//go:embed migrations/*.sql
var Migrations embed.FS
