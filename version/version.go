// Package version reports the release version of gorefactor binaries.
package version

import "runtime/debug"

// Version returns the module version embedded by the Go toolchain at build
// time. When installed via "go install" or from a GoReleaser binary it returns
// the tagged version (e.g. "v0.10.0"). Local "go build" builds return
// "(devel)".
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
