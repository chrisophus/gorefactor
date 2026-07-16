// Package version reports the release version of gorefactor binaries.
package version

import "runtime/debug"

// injected is set by GoReleaser via -ldflags "-X .../version.injected=vX.Y.Z".
// It takes priority over build-info so release binaries always report cleanly.
var injected string

// Version returns the binary's release version. GoReleaser release builds
// report the exact tag (e.g. "v0.10.2"). Binaries installed via
// "go install .../gorefactor@vX.Y.Z" report the module version from build
// info. Local "go build" builds report "(devel)".
func Version() string {
	if injected != "" {
		return injected
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "(devel)"
}
