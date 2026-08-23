package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	wantValidatePath = "/api/v2/tailnet/example.com/acl/validate"
	wantSetPath      = "/api/v2/tailnet/example.com/acl"
)

func newTestClient(h http.Handler) (*Client, func()) {
	srv := httptest.NewServer(h)
	c := New("test-token", "example.com")
	c.BaseURL = srv.URL
	return c, srv.Close
}

func TestGetPolicyReturnsBodyAndETag(t *testing.T) {
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v2/tailnet/example.com/acl"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/hujson"; got != want {
			t.Errorf("Accept = %q, want %q: without it comments are lost", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte(`{"grants": []}`))
	}))
	defer done()

	body, etag, err := c.GetPolicy(context.Background())
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if string(body) != `{"grants": []}` {
		t.Errorf("body = %q", body)
	}
	if etag != `"abc123"` {
		t.Errorf("etag = %q, want %q", etag, `"abc123"`)
	}
}

// The trap: validate signals failure with a non-empty body and HTTP 200.
func TestValidateTreatsNonEmptyBodyAsFailure(t *testing.T) {
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, wantValidatePath; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"tag not found: \"tag:nope\""}`))
	}))
	defer done()

	err := c.Validate(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("Validate must fail on a non-empty body even though the status is 200")
	}
	if !strings.Contains(err.Error(), "tag not found") {
		t.Errorf("error should carry the server's reason, got %v", err)
	}
}

func TestValidateAcceptsEmptyBody(t *testing.T) {
	for _, body := range []string{"", "{}", "  \n"} {
		c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Method, http.MethodPost; got != want {
				t.Errorf("method = %q, want %q", got, want)
			}
			if got, want := r.URL.Path, wantValidatePath; got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(body))
		}))
		if err := c.Validate(context.Background(), []byte(`{}`)); err != nil {
			t.Errorf("Validate(body=%q) = %v, want nil", body, err)
		}
		done()
	}
}

func TestSetPolicySendsIfMatch(t *testing.T) {
	wantBody := []byte(`{}`)
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, wantSetPath; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("If-Match"), `"abc123"`; got != want {
			t.Errorf("If-Match = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/hujson"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if !bytes.Equal(got, wantBody) {
			t.Errorf("body = %q, want %q", got, wantBody)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer done()

	if err := c.SetPolicy(context.Background(), wantBody, `"abc123"`); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
}

func TestSetPolicyReports412Distinctly(t *testing.T) {
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, wantSetPath; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusPreconditionFailed)
		w.Write([]byte(`{"message":"precondition failed, invalid old hash"}`))
	}))
	defer done()

	err := c.SetPolicy(context.Background(), []byte(`{}`), `"stale"`)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("SetPolicy = %v, want ErrPreconditionFailed so the caller can explain it", err)
	}
}

func TestSetPolicyReportsValidationError(t *testing.T) {
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, wantSetPath; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"bad acl"}`))
	}))
	defer done()

	err := c.SetPolicy(context.Background(), []byte(`{}`), `"abc"`)
	if err == nil || !strings.Contains(err.Error(), "bad acl") {
		t.Errorf("SetPolicy = %v, want an error carrying the server message", err)
	}
}

func TestGetPolicyReportsAuthFailure(t *testing.T) {
	c, done := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer done()

	if _, _, err := c.GetPolicy(context.Background()); err == nil {
		t.Error("GetPolicy should report a 401")
	}
}
