package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWithNoArgsPrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), nil, &out, &errb, strings.NewReader(""))
	if code == 0 {
		t.Error("no arguments should be a usage error")
	}
	if !strings.Contains(errb.String()+out.String(), "scurgery apply") {
		t.Error("usage should list the commands")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"frobnicate"}, &out, &errb, strings.NewReader(""))
	if code == 0 {
		t.Error("an unknown command should be an error")
	}
}

func TestRunRequiresCredentials(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errb, strings.NewReader(""))
	if code == 0 {
		t.Error("status without credentials should fail")
	}
	if !strings.Contains(errb.String(), "TS_API_KEY") {
		t.Errorf("the error should name the missing variable, got %q", errb.String())
	}
}

func TestSplitArgsAcceptsFlagsAfterPositional(t *testing.T) {
	flags, positional := splitArgs([]string{"bundle.hujson", "--yes", "--name", "custom"})
	if len(positional) != 1 || positional[0] != "bundle.hujson" {
		t.Errorf("positional = %v, want [bundle.hujson]", positional)
	}
	want := []string{"--yes", "--name", "custom"}
	if strings.Join(flags, " ") != strings.Join(want, " ") {
		t.Errorf("flags = %v, want %v", flags, want)
	}
}

func TestSplitArgsHandlesInlineValuesAndTerminator(t *testing.T) {
	flags, positional := splitArgs([]string{"--name=custom", "--", "-weird-file-name"})
	if len(flags) != 1 || flags[0] != "--name=custom" {
		t.Errorf("flags = %v, want [--name=custom]", flags)
	}
	if len(positional) != 1 || positional[0] != "-weird-file-name" {
		t.Errorf("positional = %v, want [-weird-file-name]", positional)
	}
}

func TestRunStatusListsNamespaces(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	if err := runStatus(context.Background(), env); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out.String(), "aws-router") {
		t.Errorf("status should list the installed namespace, got %q", out.String())
	}
}

func TestRunStatusOnCleanPolicy(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	if err := runStatus(context.Background(), env); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out.String(), "nothing installed") {
		t.Errorf("status should say so plainly, got %q", out.String())
	}
}

// runEnd2End points Run at a fake tailnet server via TS_BASE_URL, which Run
// honours as a test-only override of the client's default base URL, so the
// entry point can be exercised without touching the network.
func runEnd2End(t *testing.T, f *fakeTailnet, args []string, stdin string) (code int, out, errb bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("TS_API_KEY", "tok")
	t.Setenv("TS_TAILNET", "example.com")
	t.Setenv("TS_BASE_URL", srv.URL)

	code = Run(context.Background(), args, &out, &errb, strings.NewReader(stdin))
	return code, out, errb
}

// TestRunDiffNeverWrites pins the single most important property of the diff
// command: an operator previewing a change must not have it applied. This is
// checked at the Run level, through the real flag parsing and command
// dispatch, not against runApply directly, so a diff case that forgets to set
// DryRun would be caught here.
func TestRunDiffNeverWrites(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"diff", path, "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("Run(diff) = %d, stderr=%q", code, errb.String())
	}
	if f.writes != 0 {
		t.Errorf("diff must never write, got %d writes", f.writes)
	}
}

// TestRunApplyDryRunWritesNothing checks --dry-run through Run's flag
// wiring, not just writePolicy in isolation: a Run that dropped the flag on
// the floor before constructing Env would otherwise slip through untested.
func TestRunApplyDryRunWritesNothing(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"apply", path, "--dry-run", "--yes", "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("Run(apply --dry-run) = %d, stderr=%q", code, errb.String())
	}
	if f.writes != 0 {
		t.Errorf("--dry-run must never write, got %d writes", f.writes)
	}
}

// TestRunApplyYesAfterPositionalIsHonoured is the end-to-end version of
// TestSplitArgsAcceptsFlagsAfterPositional: it proves --yes placed after the
// bundle path actually suppresses the confirmation prompt all the way through
// Run, not merely in splitArgs' own return values. Stdin is empty, so if
// --yes were silently dropped, the prompt would read EOF, decline, and
// nothing would be written.
func TestRunApplyYesAfterPositionalIsHonoured(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"apply", path, "--yes", "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("Run(apply ... --yes) = %d, stderr=%q", code, errb.String())
	}
	if f.writes != 1 {
		t.Errorf("--yes after the positional bundle path should be honoured, got %d writes, stderr=%q", f.writes, errb.String())
	}
}
