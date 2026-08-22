package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nopoz/scurgery/internal/policy"
)

func writeBundleFile(t *testing.T, name, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("writing bundle fixture: %v", err)
	}
	return path
}

func TestRunApplyAddsBundleContents(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	if err := runApply(context.Background(), env, path, "", policy.ApplyOptions{}); err != nil {
		t.Fatalf("runApply: %v", err)
	}
	if f.writes != 1 {
		t.Errorf("writes = %d, want 1", f.writes)
	}
	if !strings.Contains(string(f.policy), "group:eng") {
		t.Errorf("policy should contain the bundle's contribution, got %q", f.policy)
	}
	if strings.Contains(out.String(), "--force overwrote") {
		t.Errorf("no force warning should appear when nothing was overwritten, got %q", out.String())
	}
}

func TestRunApplyForceWarnsThatOverwriteIsUnrecoverable(t *testing.T) {
	f := &fakeTailnet{
		policy:     []byte(`{"tagOwners": {"tag:subnet-router": ["autogroup:admin"]}}`),
		etag:       `"e1"`,
		validateOK: true,
	}
	env, out, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	if err := runApply(context.Background(), env, path, "", policy.ApplyOptions{Force: true}); err != nil {
		t.Fatalf("runApply: %v", err)
	}
	if f.writes != 1 {
		t.Errorf("writes = %d, want 1", f.writes)
	}
	got := out.String()
	if !strings.Contains(got, "--force overwrote") {
		t.Errorf("force overwrite should warn that it happened, got %q", got)
	}
	if !strings.Contains(got, "cannot restore") {
		t.Errorf("force overwrite should warn that it cannot be undone by remove, got %q", got)
	}
	if !strings.Contains(got, "self-check does not run") {
		t.Errorf("force overwrite should warn that scurgery's self-check was skipped for this write, got %q", got)
	}
}

// --skip-conflicts overwrites nothing, so it must always keep the full
// self-check, and must never claim a force overwrite happened. Wrap
// applyVerify to observe the self-check actually ran, rather than inferring
// it from output or side effects.
func TestRunApplySkipConflictsKeepsSelfCheck(t *testing.T) {
	calls := 0
	orig := applyVerify
	applyVerify = func(before, after []byte, ns string) error {
		calls++
		return orig(before, after, ns)
	}
	defer func() { applyVerify = orig }()

	f := &fakeTailnet{
		policy:     []byte(`{"tagOwners": {"tag:subnet-router": ["autogroup:admin"]}}`),
		etag:       `"e1"`,
		validateOK: true,
	}
	env, out, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"], "tag:new-service": ["group:eng"]}}`)

	if err := runApply(context.Background(), env, path, "", policy.ApplyOptions{SkipConflicts: true}); err != nil {
		t.Fatalf("runApply: %v", err)
	}
	if f.writes != 1 {
		t.Errorf("writes = %d, want 1", f.writes)
	}
	got := out.String()
	if strings.Contains(got, "--force overwrote") {
		t.Errorf("--skip-conflicts overwrites nothing, so it must not claim a force overwrite happened, got %q", got)
	}
	if calls == 0 {
		t.Error("the self-check must still run when --skip-conflicts is used without --force")
	}
}

// TestRunApplyReappliedUpdateOverwritesOwnValueWithoutForce reproduces
// re-applying a bundle whose member value changed for a namespace already
// installed. It must not be reported as a conflict, must not need --force,
// and the self-check must still run and pass, since only scurgery's own
// marked value changed.
func TestRunApplyReappliedUpdateOverwritesOwnValueWithoutForce(t *testing.T) {
	calls := 0
	orig := applyVerify
	applyVerify = func(before, after []byte, ns string) error {
		calls++
		return orig(before, after, ns)
	}
	defer func() { applyVerify = orig }()

	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:beta": ["autogroup:admin"]}}`)
	if err := runApply(context.Background(), env, path, "", policy.ApplyOptions{}); err != nil {
		t.Fatalf("runApply (first): %v", err)
	}

	updatedPath := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:beta": ["autogroup:admin", "tag:beta"]}}`)
	if err := runApply(context.Background(), env, updatedPath, "", policy.ApplyOptions{}); err != nil {
		t.Fatalf("runApply (updated, no --force): %v", err)
	}
	if f.writes != 2 {
		t.Errorf("writes = %d, want 2", f.writes)
	}
	if !strings.Contains(string(f.policy), `["autogroup:admin", "tag:beta"]`) {
		t.Errorf("policy should hold the updated value, got %q", f.policy)
	}
	if strings.Contains(out.String(), "conflict at") {
		t.Errorf("re-applying an update to scurgery's own value must not be reported as a conflict, got %q", out.String())
	}
	if calls == 0 {
		t.Error("the self-check must run for a re-applied update, since it did not need --force")
	}
}

// TestRunApplyWarnsWhenMemberIsSharedWithAnotherNamespace reproduces two
// bundles declaring the same tagOwners member: the second apply must warn at
// the moment it happens, naming the member and the namespace that actually
// owns it, since removing that namespace later removes it too.
func TestRunApplyWarnsWhenMemberIsSharedWithAnotherNamespace(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"tagOwners": {}}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	alphaPath := writeBundleFile(t, "alpha.hujson", `{"tagOwners": {"tag:shared": ["autogroup:admin"], "tag:alpha": ["autogroup:admin"]}}`)
	if err := runApply(context.Background(), env, alphaPath, "", policy.ApplyOptions{}); err != nil {
		t.Fatalf("runApply alpha: %v", err)
	}

	betaPath := writeBundleFile(t, "beta.hujson", `{"tagOwners": {"tag:shared": ["autogroup:admin"], "tag:beta": ["autogroup:admin"]}}`)
	if err := runApply(context.Background(), env, betaPath, "", policy.ApplyOptions{}); err != nil {
		t.Fatalf("runApply beta: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "note:") {
		t.Fatalf("apply should have printed a note about the shared member, got %q", got)
	}
	note := got[strings.Index(got, "note:"):]
	if !strings.Contains(note, "tag:shared") {
		t.Errorf("the note should name the shared member, got %q", note)
	}
	if !strings.Contains(note, `"alpha"`) {
		t.Errorf("the note should name the namespace that actually owns it, got %q", note)
	}
}

func TestRunApplyBlocksOnConflictWithoutForce(t *testing.T) {
	f := &fakeTailnet{
		policy:     []byte(`{"tagOwners": {"tag:subnet-router": ["autogroup:admin"]}}`),
		etag:       `"e1"`,
		validateOK: true,
	}
	env, _, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "test.hujson", `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`)

	err := runApply(context.Background(), env, path, "", policy.ApplyOptions{})
	if err == nil {
		t.Fatal("a conflict without --force should block the apply")
	}
	if f.writes != 0 {
		t.Error("a blocked apply must not write")
	}
}
