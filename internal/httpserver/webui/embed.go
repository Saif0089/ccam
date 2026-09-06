// Package webui embeds ccam's static web UI directly into the binary,
// so the whole tool ships as one file with no separate assets to
// install or go missing.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Handler serves the embedded UI at "/".
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // unreachable: "static" is embedded above
	}
	return http.FileServer(http.FS(sub))
}
