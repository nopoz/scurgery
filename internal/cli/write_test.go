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
