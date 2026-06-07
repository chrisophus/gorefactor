// Package version holds the release version for gorefactor binaries.
// GoReleaser overrides Version via -ldflags at link time; the constant
// below is the default when building with plain `go build`.
package version

// Version is the current gorefactor release (CLI and agent).
const Version = "0.3.1"
