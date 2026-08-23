package cli

import (
	"bytes"
	"context"
	"runtime/debug"
	"strings"
	"testing"
)

// stubBuildInfo points the buildInfo seam at a fixed reading for one test.
func stubBuildInfo(t *testing.T, bi *debug.BuildInfo, ok bool) {
	t.Helper()
	prev := buildInfo
	buildInfo = func() (*debug.BuildInfo, bool) { return bi, ok }
	t.Cleanup(func() { buildInfo = prev })
}

// setVersion sets the ldflags-injected variable for one test.
func setVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

func TestResolveVersionPrefersLdflags(t *testing.T) {
	setVersion(t, "v9.9.9")
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true)
	if got := resolveVersion(); got != "v9.9.9" {
		t.Errorf("resolveVersion() = %q, want the ldflags value v9.9.9", got)
	}
}

// TestResolveVersionUsesModuleVersion covers the documented install path.
// `go install ...@latest` cannot receive an ldflags value, so the module
// version recorded by the toolchain is the only source there.
func TestResolveVersionUsesModuleVersion(t *testing.T) {
	setVersion(t, "")
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true)
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want v1.2.3", got)
	}
}

// TestResolveVersionUsesPseudoVersion covers an ordinary build from a
// checkout, where the toolchain synthesises a pseudo-version carrying the
// commit and, for an uncommitted tree, its own +dirty marker.
func TestResolveVersionUsesPseudoVersion(t *testing.T) {
	setVersion(t, "")
	pseudo := "v0.0.0-20260823045606-541a08125e17+dirty"
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: pseudo}}, true)
	if got := resolveVersion(); got != pseudo {
		t.Errorf("resolveVersion() = %q, want %q", got, pseudo)
	}
}

func TestResolveVersionWithoutBuildInfo(t *testing.T) {
	setVersion(t, "")
	stubBuildInfo(t, nil, false)
	if got := resolveVersion(); got != "unknown" {
		t.Errorf("resolveVersion() = %q, want unknown", got)
	}
}

// TestResolveVersionWhenVCSStampingIsOff pins the one case with no version
// to report: a build with -buildvcs=false, which records "(devel)" and no
// vcs settings at all.
func TestResolveVersionWhenVCSStampingIsOff(t *testing.T) {
	setVersion(t, "")
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true)
	if got := resolveVersion(); got != "unknown" {
		t.Errorf("resolveVersion() = %q, want unknown", got)
	}
}

// TestRunVersionNeedsNoCredentials pins that `version` answers before any
// credential or config-file handling. Reporting a version is the first thing
// an operator does when a run misbehaves, and it must not itself fail on the
// configuration they are trying to diagnose.
func TestRunVersionNeedsNoCredentials(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setVersion(t, "v0.1.0")

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"version"}, &out, &errb, strings.NewReader(""))
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "v0.1.0") {
		t.Errorf("stdout = %q, want it to carry the version", out.String())
	}
	if errb.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", errb.String())
	}
}

func TestUsageListsTheVersionCommand(t *testing.T) {
	if !strings.Contains(usage, "scurgery version") {
		t.Error("usage text should list the version command")
	}
}
