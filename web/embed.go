// Package web embeds the compiled dashboard (web/dist) into the binary. It is a
// thin package so the //go:embed directive lives at the repository root where
// the web/ directory is sibling to it.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// FS returns the embedded dist filesystem rooted at web/dist.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
