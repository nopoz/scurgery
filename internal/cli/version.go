package cli

import (
	"runtime/debug"
)

// version is injected at link time by the release build:
//
//	-ldflags "-X github.com/nopoz/scurgery/internal/cli.version=v0.1.0"
//
// It is empty for every other build path. In particular the documented
// install, `go install github.com/nopoz/scurgery/cmd/scurgery@latest`, never
// receives a linker flag, so a version reported from this variable alone
// would be wrong for most binaries in the wild.
var version = ""

// buildInfo is debug.ReadBuildInfo behind a package variable so tests can
// supply a fixed reading. A build stamp is not something an operator should
// be able to redirect at runtime, so this stays a variable rather than a flag
// or an environment lookup.
var buildInfo = debug.ReadBuildInfo

// resolveVersion reports the running build: the linker flag if the release
// build set one, otherwise the module version the toolchain recorded.
//
// That module version covers both a `go install` of a tagged release, which
// gives the tag, and an ordinary build from a checkout, which gives a
// pseudo-version already carrying the commit and a +dirty marker. There is
// deliberately no fallback to the vcs.revision build setting: the toolchain
// records "(devel)" only when VCS stamping is off, and in that case it
// records no vcs settings either, so such a fallback could never fire.
func resolveVersion() string {
	if version != "" {
		return version
	}
	bi, ok := buildInfo()
	if !ok {
		return "unknown"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "unknown"
}
