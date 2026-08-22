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
	if strings.Contains(out.String(), "does not record") {
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
	if !strings.Contains(got, "cannot restore") && !strings.Contains(got, "does not record") {
		t.Errorf("force overwrite should warn that it cannot be undone by remove, got %q", got)
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
