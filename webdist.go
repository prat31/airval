// Package airval exposes build artifacts that must be embedded from the module root.
package airval

import "embed"

// WebDist is the built single-page app (web/dist, produced by `npm --prefix web run build`).
//
//go:embed all:web/dist
var WebDist embed.FS
