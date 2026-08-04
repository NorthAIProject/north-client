// Package migrations embeds SQL migration files into the application binary.
//
// Goose runs them from this filesystem on process start (web and worker), so
// local dev and production deploys both apply schema without a separate step.
package migrations

import "embed"

// FS holds every goose migration committed under this directory.
//
// //go:embed is relative to this package, so new *.sql files are picked up
// automatically on the next build — no registry list to maintain.
//
//go:embed *.sql
var FS embed.FS
