package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nopoz/scurgery/internal/api"
)

type fakeTailnet struct {
	policy      []byte
	etag        string
	validateOK  bool
	writes      int
	lastIfMatch string
}

func (f *fakeTailnet) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.Header().Set("ETag", f.etag)
			w.Write(f.policy)
		case strings.HasSuffix(r.URL.Path, "/validate"):
			w.WriteHeader(http.StatusOK)
			if !f.validateOK {
				w.Write([]byte(`{"message":"tag not found"}`))
			}
		default:
			f.lastIfMatch = r.Header.Get("If-Match")
			if f.lastIfMatch != f.etag {
				w.WriteHeader(http.StatusPreconditionFailed)
				w.Write([]byte(`{"message":"precondition failed, invalid old hash"}`))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			f.policy = body
			f.writes++
			w.WriteHeader(http.StatusOK)
		}
	})
}

func testEnv(t *testing.T, f *fakeTailnet, stdin string) (*Env, *bytes.Buffer, func()) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	c := api.New("tok", "example.com")
	c.BaseURL = srv.URL
	out := &bytes.Buffer{}
	env := &Env{
		Client:    c,
		Out:       out,
		In:        strings.NewReader(stdin),
		BackupDir: t.TempDir(),
		AssumeYes: true,
	}
	return env, out, srv.Close
}

func TestWritePolicyValidatesBeforeWriting(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: false}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err == nil {
		t.Fatal("writePolicy should fail when validation rejects the policy")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when validation fails")
	}
}

func TestWritePolicySendsETag(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err != nil {
		t.Fatalf("writePolicy: %v", err)
	}
	if f.lastIfMatch != `"e1"` {
		t.Errorf("If-Match = %q, want the ETag from the read", f.lastIfMatch)
	}
	if f.writes != 1 {
		t.Errorf("writes = %d, want 1", f.writes)
	}
}

func TestWritePolicyWritesBackup(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil); err != nil {
		t.Fatalf("writePolicy: %v", err)
	}
	entries, _ := os.ReadDir(env.BackupDir)
	if len(entries) != 1 {
		t.Fatalf("expected one backup file, got %d", len(entries))
	}
	b, _ := os.ReadFile(env.BackupDir + "/" + entries[0].Name())
	if string(b) != `{"grants": []}` {
		t.Errorf("backup should hold the pre-change policy, got %q", b)
	}
}

// TestWriteBackupNeverOverwritesAnExisting reproduces the collision: two
// backups for the same operation land in the same second, which used to key
// the filename by timestamp alone. os.WriteFile truncates, so the second
// backup silently destroyed the first, exactly the "only copy of the
// original" the backup exists to preserve.
func TestWriteBackupNeverOverwritesAnExisting(t *testing.T) {
	dir := t.TempDir()

	p1, err := writeBackup(dir, "apply aws-router", []byte("first"))
	if err != nil {
		t.Fatalf("writeBackup 1: %v", err)
	}
	p2, err := writeBackup(dir, "apply aws-router", []byte("second"))
	if err != nil {
		t.Fatalf("writeBackup 2: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("two backups for the same operation collided on one filename: %s", p1)
	}

	b1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatalf("reading %s: %v", p1, err)
	}
	b2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatalf("reading %s: %v", p2, err)
	}
	if string(b1) != "first" {
		t.Errorf("first backup content = %q, want %q: it must not have been overwritten by the second", b1, "first")
	}
	if string(b2) != "second" {
		t.Errorf("second backup content = %q, want %q", b2, "second")
	}
	if !strings.Contains(p1, "aws-router") {
		t.Errorf("backup filename should include the namespace, got %q", p1)
	}
}

// TestWritePolicyBackupsForConsecutiveWritesBothSurvive is the same
// collision reproduced through the real write path: an apply immediately
// followed by a remove, both landing in the same backup directory, must
// leave two files behind holding the two different pre-change policies, not
// one file where the second write clobbered the first.
func TestWritePolicyBackupsForConsecutiveWritesBothSurvive(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := writePolicy(context.Background(), env, "apply aws-router", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil); err != nil {
		t.Fatalf("writePolicy 1: %v", err)
	}
	if err := writePolicy(context.Background(), env, "apply aws-router", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1, 2]}`), nil
	}, nil); err != nil {
		t.Fatalf("writePolicy 2: %v", err)
	}

	entries, err := os.ReadDir(env.BackupDir)
	if err != nil {
		t.Fatalf("reading backup dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two distinct backup files, got %d: %v", len(entries), entries)
	}
	var contents []string
	for _, e := range entries {
		b, err := os.ReadFile(env.BackupDir + "/" + e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		contents = append(contents, string(b))
	}
	if contents[0] == contents[1] {
		t.Errorf("both backups hold the same content; the first write's backup should not have been overwritten, got %v", contents)
	}
	want := map[string]bool{`{"grants": []}`: false, `{"grants": [1]}`: false}
	for _, c := range contents {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for c, found := range want {
		if !found {
			t.Errorf("expected a backup holding %q, got %v", c, contents)
		}
	}
}

func TestWritePolicyNoOpWritesNothing(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	if err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return nil, nil
	}, nil); err != nil {
		t.Fatalf("writePolicy: %v", err)
	}
	if f.writes != 0 {
		t.Error("a no-op must not write")
	}
	if !strings.Contains(out.String(), "no change") {
		t.Errorf("should say nothing changed, got %q", out.String())
	}
}

func TestWritePolicyDryRunWritesNothing(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()
	env.DryRun = true

	if err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil); err != nil {
		t.Fatalf("writePolicy: %v", err)
	}
	if f.writes != 0 {
		t.Error("dry run must not write")
	}
	if !strings.Contains(out.String(), "+") {
		t.Error("dry run should still show the diff")
	}
}

func TestWritePolicyDeclinedAtPromptWritesNothing(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "n\n")
	defer done()
	env.AssumeYes = false

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err == nil {
		t.Fatal("declining should be reported as an error so the exit code is non-zero")
	}
	if f.writes != 0 {
		t.Error("declining must not write")
	}
}

func TestWritePolicySelfCheckBlocksTheWrite(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, func(before, after []byte) error {
		return errors.New("changed something it does not own")
	})
	if err == nil {
		t.Fatal("a failing self-check must block the write")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when the self-check fails")
	}
}

func TestWritePolicyExplains412(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		f.etag = `"changed-underneath"` // simulate a concurrent edit
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a 412 should be explained in terms the operator can act on, got %v", err)
	}
}

// A missing ETag means the write can no longer be a compare-and-swap: it
// would silently become an unconditional overwrite, since SetPolicy omits
// If-Match entirely when the etag is empty. Refuse rather than write blind.
func TestWritePolicyRefusesWithoutETag(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: "", validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err == nil {
		t.Fatal("writePolicy should refuse to write when the read returned no ETag")
	}
	if !strings.Contains(err.Error(), "ETag") {
		t.Errorf("the error should explain that the ETag was missing, got %v", err)
	}
	if f.writes != 0 {
		t.Error("nothing may be written without an ETag to condition on")
	}
}

// The backup exists to survive total failure: it must be on disk before the
// write to the tailnet is even attempted, not just on the success path.
func TestWritePolicyBackupPrecedesFailedWrite(t *testing.T) {
	f := &fakeTailnet{policy: []byte(`{"grants": []}`), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := writePolicy(context.Background(), env, "test", func(cur []byte) ([]byte, error) {
		f.etag = `"changed-underneath"` // force SetPolicy to 412
		return []byte(`{"grants": [1]}`), nil
	}, nil)
	if err == nil {
		t.Fatal("expected the write to fail")
	}
	if f.writes != 0 {
		t.Error("a failed write must not have written anything")
	}
	entries, _ := os.ReadDir(env.BackupDir)
	if len(entries) != 1 {
		t.Fatalf("expected the backup to exist despite the failed write, got %d entries", len(entries))
	}
	b, _ := os.ReadFile(env.BackupDir + "/" + entries[0].Name())
	if string(b) != `{"grants": []}` {
		t.Errorf("backup should hold the pre-change policy, got %q", b)
	}
}
