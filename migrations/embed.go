// Package migrations SQL dosyalarını derlenen programın içine gömer.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
