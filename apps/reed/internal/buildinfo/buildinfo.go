package buildinfo

import "runtime/debug"

// Version is replaced at build time for release binaries.
var Version = "dev"

var readBuildInfo = debug.ReadBuildInfo

// Resolve returns the ldflags version, or the module version reported by
// `go install module@version` when no ldflags value was injected.
func Resolve() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	info, ok := readBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}
