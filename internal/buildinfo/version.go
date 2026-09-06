// Package buildinfo holds ccam's version, set at build time via
// -ldflags "-X ccam/internal/buildinfo.Version=...". "dev" is what a
// plain `go build`/`go run` (no ldflags) produces, so it's obvious an
// ad-hoc build is running rather than a tagged release.
package buildinfo

var Version = "dev"
