package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nopoz/scurgery/internal/api"
)

func TestRunWithNoArgsPrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), nil, &out, &errb, strings.NewReader(""))
	if code != 2 {
		t.Errorf("code = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(errb.String()+out.String(), "scurgery apply") {
		t.Error("usage should list the commands")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"frobnicate"}, &out, &errb, strings.NewReader(""))
	if code != 2 {
		t.Errorf("code = %d, want 2 (usage error)", code)
	}
}

// TestRunUnknownCommandReportsBeforeCredentialCheck pins the order of
// validation: a typo'd command name must be reported as an unknown command,
// not misdiagnosed as a missing credential just because credential checks
// used to run first.
func TestRunUnknownCommandReportsBeforeCredentialCheck(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"frobnicate"}, &out, &errb, strings.NewReader(""))
	if code != 2 {
		t.Errorf("code = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Errorf("a typo'd command should be reported as unknown, got %q", errb.String())
	}
	if strings.Contains(errb.String(), "TS_API_KEY is not set") {
		t.Errorf("an unknown command must not be misreported as a credentials problem, got %q", errb.String())
	}
}

func TestRunRequiresCredentials(t *testing.T) {
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"status"}, &out, &errb, strings.NewReader(""))
	if code != 3 {
		t.Errorf("code = %d, want 3 (runtime error)", code)
	}
	if !strings.Contains(errb.String(), "TS_API_KEY") {
		t.Errorf("the error should name the missing variable, got %q", errb.String())
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	for _, cmd := range []string{"help", "-h", "--help"} {
		var out, errb bytes.Buffer
		code := Run(context.Background(), []string{cmd}, &out, &errb, strings.NewReader(""))
		if code != 0 {
			t.Errorf("Run(%q) = %d, want 0", cmd, code)
		}
		if !strings.Contains(out.String(), "scurgery apply") {
			t.Errorf("Run(%q) should print usage, got %q", cmd, out.String())
		}
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

// TestRunStatusReportsGetPolicyFailure and TestRunStatusReportsUnparseablePolicy
// pin that status never turns a failure to read or understand the policy
// into a false "nothing installed": that would be exactly the lie this
// command must never tell.
func TestRunStatusReportsGetPolicyFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := api.New("tok", "example.com")
	c.BaseURL = srv.URL
	env := &Env{Client: c, Out: &bytes.Buffer{}, In: strings.NewReader(""), BackupDir: t.TempDir()}

	if err := runStatus(context.Background(), env); err == nil {
		t.Fatal("runStatus should report an error when the policy cannot be fetched, not a false all-clear")
	}
}

func TestRunStatusReportsUnparseablePolicy(t *testing.T) {
	f := &fakeTailnet{policy: []byte("not valid hujson {{{"), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := runStatus(context.Background(), env); err == nil {
		t.Fatal("runStatus should report an error on an unparseable policy, not a false all-clear")
	}
}

// runEnd2End points Run at a fake tailnet server by swapping newClient, the
// constructor api.New sits behind, for the duration of the test. Unlike an
// environment variable this is not reachable from outside the test binary.
func runEnd2End(t *testing.T, f *fakeTailnet, args []string, stdin string) (code int, out, errb bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	orig := newClient
	newClient = func(token, tailnet string) *api.Client {
		c := orig(token, tailnet)
		c.BaseURL = srv.URL
		return c
	}
	t.Cleanup(func() { newClient = orig })

	t.Setenv("TS_API_KEY", "tok")
	t.Setenv("TS_TAILNET", "example.com")

	code = Run(context.Background(), args, &out, &errb, strings.NewReader(stdin))
	return code, out, errb
}

// TestRunDiffNeverWrites pins the single most important property of the diff
// command: an operator previewing a change must not have it applied. This is
// checked at the Run level, through the real flag parsing and command
// dispatch, not against runApply directly, so a diff case that forgets to set
// DryRun would be caught here. The bundle here does represent a real change,
// so this also exercises the exit-1 side of TestRunDiffExitCodeMatchesWhetherItWouldChangeAnything;
// f.writes staying at 0 is the property this test exists for.
func TestRunDiffNeverWrites(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"diff", path, "--backup-dir", t.TempDir()}, "")
	if code != 1 {
		t.Fatalf("Run(diff) = %d, want 1: the bundle would change the policy, stderr=%q", code, errb.String())
	}
	if f.writes != 0 {
		t.Errorf("diff must never write, got %d writes", f.writes)
	}
}

// TestRunDiffExitCodeMatchesWhetherItWouldChangeAnything pins the design's
// "dry run, exit 1 if it would change anything" both ways: a bundle already
// fully installed must exit 0, and one that would add something new must
// exit 1. Before this fix, cli/app.go returned 0 unconditionally, which
// broke diff's use as a CI check for policy drift.
func TestRunDiffExitCodeMatchesWhetherItWouldChangeAnything(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"diff", path, "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("Run(diff) on an already-installed bundle = %d, want 0, stderr=%q", code, errb.String())
	}

	changingPath := writeBundleFile(t, "test2.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)
	code, _, errb = runEnd2End(t, f, []string{"diff", changingPath, "--backup-dir", t.TempDir()}, "")
	if code != 1 {
		t.Fatalf("Run(diff) on a bundle that would add something = %d, want 1, stderr=%q", code, errb.String())
	}
	if f.writes != 0 {
		t.Errorf("diff must never write regardless of its exit code, got %d writes", f.writes)
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

// TestRunApplyWithoutYesRequiresConfirmation is the mirror image of
// TestRunApplyYesAfterPositionalIsHonoured: without --yes, apply must not
// write until the operator says yes at the prompt. A one-character slip that
// hardcodes AssumeYes to true would make every apply and remove write
// without asking, and this is the test that catches it.
func TestRunApplyWithoutYesRequiresConfirmation(t *testing.T) {
	for _, stdin := range []string{"n\n", ""} {
		f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
		path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

		code, _, errb := runEnd2End(t, f, []string{"apply", path, "--backup-dir", t.TempDir()}, stdin)
		if code == 0 {
			t.Errorf("stdin %q: declining confirmation should be a non-zero exit, stderr=%q", stdin, errb.String())
		}
		if f.writes != 0 {
			t.Errorf("stdin %q: apply without --yes must not write before confirmation is given, got %d writes", stdin, f.writes)
		}
	}
}

// TestRunAppliesNameFlag holds requirement 2 in place: --name must actually
// reach bundle.Load through Run, not just be accepted and ignored.
func TestRunAppliesNameFlag(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"apply", path, "--yes", "--name", "custom-ns", "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("Run(apply --name) = %d, stderr=%q", code, errb.String())
	}
	if !strings.Contains(string(f.policy), "scurgery:custom-ns") {
		t.Errorf("--name should set the namespace marker, got %q", f.policy)
	}
}

// TestRunAppliesBackupDirFlag holds --backup-dir in place: the backup must
// land where the operator said, not silently default to the process's
// working directory.
func TestRunAppliesBackupDirFlag(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)
	dir := t.TempDir()

	code, _, errb := runEnd2End(t, f, []string{"apply", path, "--yes", "--backup-dir", dir}, "")
	if code != 0 {
		t.Fatalf("Run(apply --backup-dir) = %d, stderr=%q", code, errb.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("expected one backup file in %s, got %v (err=%v)", dir, entries, err)
	}
}

// TestRunApplyRejectsMultiplePositionals closes the silent-drop hole where
// `scurgery apply a.hujson b.hujson` would apply only a.hujson without a
// word about b.hujson.
func TestRunApplyRejectsMultiplePositionals(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	pathA := writeBundleFile(t, "a.hujson", `{"tagOwners": {"tag:a": ["group:eng"]}}`)
	pathB := writeBundleFile(t, "b.hujson", `{"tagOwners": {"tag:b": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"apply", pathA, pathB, "--yes", "--backup-dir", t.TempDir()}, "")
	if code != 2 {
		t.Errorf("Run(apply <a> <b>) = %d, want 2 (usage error); stderr=%q", code, errb.String())
	}
	if f.writes != 0 {
		t.Error("a rejected multi-positional call must not write")
	}
}

// A CI gate reading a broken token or an unreadable policy as "drift" is
// the failure the exit-code contract exists to prevent, so the two tests
// below matter as much as the happy path.
func TestRunRuntimeErrorExitsThree(t *testing.T) {
	f := &fakeTailnet{policy: []byte("not valid hujson {{{"), etag: `"e1"`, validateOK: true}
	code, _, errb := runEnd2End(t, f, []string{"status"}, "")
	if code != 3 {
		t.Errorf("code = %d, want 3 (runtime error), stderr=%q", code, errb.String())
	}
}

func TestRunDiffRuntimeErrorIsNotReportedAsDrift(t *testing.T) {
	f := &fakeTailnet{policy: []byte("not valid hujson {{{"), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "aws-router.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)

	code, _, errb := runEnd2End(t, f, []string{"diff", path, "--backup-dir", t.TempDir()}, "")
	if code == 1 {
		t.Fatal("a failure to read the policy must not exit 1: that is indistinguishable from the policy needing an update")
	}
	if code != 3 {
		t.Errorf("code = %d, want 3 (runtime error), stderr=%q", code, errb.String())
	}
}

func TestRunStatusJSON(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	code, out, errb := runEnd2End(t, f, []string{"status", "--json"}, "")
	if code != 0 {
		t.Fatalf("code = %d, want 0, stderr=%q", code, errb.String())
	}

	var got struct {
		Installed []string `json:"installed"`
	}
	// Unmarshalling the whole of stdout pins that nothing human-readable is
	// mixed in with the document.
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout must be one JSON document and nothing else: %v, got %q", err, out.String())
	}
	if len(got.Installed) != 1 || got.Installed[0] != "aws-router" {
		t.Errorf("installed = %v, want [aws-router]", got.Installed)
	}
}

// An empty result must serialise as [] rather than null, so a consumer can
// iterate it without a nil check.
func TestRunStatusJSONEmptyListIsNotNull(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	code, out, errb := runEnd2End(t, f, []string{"status", "--json"}, "")
	if code != 0 {
		t.Fatalf("code = %d, want 0, stderr=%q", code, errb.String())
	}
	if !strings.Contains(strings.ReplaceAll(out.String(), " ", ""), `"installed":[]`) {
		t.Errorf("an empty result must be [] not null, got %q", out.String())
	}
}

func TestRunDiffJSON(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "aws-router.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)

	code, out, errb := runEnd2End(t, f, []string{"diff", path, "--json", "--backup-dir", t.TempDir()}, "")
	if code != 1 {
		t.Fatalf("code = %d, want 1 (would change), stderr=%q", code, errb.String())
	}

	var got struct {
		Bundle  string `json:"bundle"`
		Changed bool   `json:"changed"`
		Diff    string `json:"diff"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout must be one JSON document and nothing else: %v, got %q", err, out.String())
	}
	if !got.Changed {
		t.Error("changed should be true when the bundle would add something")
	}
	if got.Bundle != "aws-router" {
		t.Errorf("bundle = %q, want the namespace aws-router", got.Bundle)
	}
	if !strings.Contains(got.Diff, "tag:new") {
		t.Errorf("the diff text belongs in the document, got %q", got.Diff)
	}
	if f.writes != 0 {
		t.Errorf("diff must never write, got %d writes", f.writes)
	}
}

func TestRunDiffJSONWhenNothingWouldChange(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {"tag:new": ["group:eng"]}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "aws-router.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)

	code, out, errb := runEnd2End(t, f, []string{"diff", path, "--json", "--backup-dir", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (no change), stderr=%q", code, errb.String())
	}

	var got struct {
		Changed bool   `json:"changed"`
		Diff    string `json:"diff"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout must be one JSON document and nothing else: %v, got %q", err, out.String())
	}
	if got.Changed {
		t.Error("changed should be false when the bundle is already installed")
	}
	if got.Diff != "" {
		t.Errorf("diff should be empty when nothing would change, got %q", got.Diff)
	}
}

// Flags are registered for every subcommand, so accepting --json silently
// here would let an operator believe apply produced a document.
func TestRunJSONRejectedOnMutatingCommands(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "aws-router.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)

	for _, args := range [][]string{
		{"apply", path, "--json", "--yes"},
		{"remove", "aws-router", "--json", "--yes"},
	} {
		code, _, errb := runEnd2End(t, f, append(args, "--backup-dir", t.TempDir()), "")
		if code != 2 {
			t.Errorf("Run(%v) = %d, want 2 (usage error), stderr=%q", args, code, errb.String())
		}
		if !strings.Contains(errb.String(), "--json") {
			t.Errorf("the message should name the flag, got %q", errb.String())
		}
	}
	if f.writes != 0 {
		t.Errorf("a rejected flag must leave the policy alone, got %d writes", f.writes)
	}
}

// The per-conflict detail lines must reach stderr rather than being
// discarded with the rest of the prose.
func TestRunDiffJSONKeepsConflictDetailOnStderr(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {"tag:new": ["group:ops"]}}`), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "aws-router.hujson", `{"tagOwners": {"tag:new": ["group:eng"]}}`)

	code, out, errb := runEnd2End(t, f, []string{"diff", path, "--json", "--backup-dir", t.TempDir()}, "")
	if code != 3 {
		t.Fatalf("code = %d, want 3 (runtime error: the bundle conflicts), stderr=%q", code, errb.String())
	}
	if !strings.Contains(errb.String(), "conflict at") {
		t.Errorf("conflict detail must survive --json, got stderr=%q", errb.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay clean when there is no document to emit, got %q", out.String())
	}
}

// Asking a subcommand for help is not a usage error. It goes to stdout and
// succeeds, the same as `scurgery help`, so that a script or a pager can read
// it and a shell does not see a failure.
func TestSubcommandHelpSucceedsOnStdout(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "-h"},
		{"apply", "--help"},
		{"remove", "--help"},
		{"diff", "-h"},
		{"status", "--help"},
	} {
		var out, errb bytes.Buffer
		code := Run(context.Background(), args, &out, &errb, strings.NewReader(""))
		if code != 0 {
			t.Errorf("Run(%v) = %d, want 0; stderr=%q", args, code, errb.String())
		}
		if !strings.Contains(out.String(), "scurgery apply") {
			t.Errorf("Run(%v) should print the usage text on stdout, got stdout=%q stderr=%q", args, out.String(), errb.String())
		}
		if !strings.Contains(out.String(), "-backup-dir") {
			t.Errorf("Run(%v) should still name the flags the usage text does not spell out, got %q", args, out.String())
		}
		if errb.String() != "" {
			t.Errorf("Run(%v) put help on stderr as well as stdout, which is what made it look like an error: %q", args, errb.String())
		}
	}
}

// The other half: a flag that does not exist is a usage error and must stay
// one, so that turning help into a success does not turn typos into successes.
func TestSubcommandUnknownFlagIsStillAUsageError(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"apply", "bundle.hujson", "--dryrun"}, &out, &errb, strings.NewReader(""))
	if code != 2 {
		t.Errorf("Run with an unknown flag = %d, want 2; stderr=%q", code, errb.String())
	}
	if !strings.Contains(errb.String(), "dryrun") {
		t.Errorf("the refusal should name the flag it did not recognise, got %q", errb.String())
	}
}
