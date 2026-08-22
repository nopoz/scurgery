package api_test

import (
	"context"
	"os"
	"testing"

	"github.com/nopoz/scurgery/internal/api"
	"github.com/nopoz/scurgery/internal/policy"
)

// TestAgainstRealTailnet exercises the read and validate paths against a live
// tailnet. It never writes. Enable it with:
//
//	SCURGERY_INTEGRATION_TAILNET=example.com TS_API_KEY=... go test -run Real ./...
func TestAgainstRealTailnet(t *testing.T) {
	tailnet := os.Getenv("SCURGERY_INTEGRATION_TAILNET")
	token := os.Getenv("TS_API_KEY")
	if tailnet == "" || token == "" {
		t.Skip("set SCURGERY_INTEGRATION_TAILNET and TS_API_KEY to run")
	}

	c := api.New(token, tailnet)
	ctx := context.Background()

	current, etag, err := c.GetPolicy(ctx)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if etag == "" {
		t.Error("expected an ETag, without which writes cannot be conditional")
	}

	// The real policy must validate as-is.
	if err := c.Validate(ctx, current); err != nil {
		t.Fatalf("the tailnet's current policy failed validation: %v", err)
	}

	// An applied bundle must also validate, and removing it must restore the
	// original bytes. Nothing is written back.
	bundle := []byte(`{"tagOwners": {"tag:scurgery-integration-probe": ["autogroup:admin"]}}`)
	applied, err := policy.Apply(current, bundle, "integration-probe", policy.ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied.Conflicts) > 0 {
		t.Fatalf("unexpected conflicts: %+v", applied.Conflicts)
	}
	if err := c.Validate(ctx, applied.Policy); err != nil {
		t.Fatalf("the applied policy failed validation: %v", err)
	}

	removed, err := policy.Remove(applied.Policy, "integration-probe")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if string(removed.Policy) != string(current) {
		t.Error("apply then remove did not restore the live policy byte-for-byte")
	}
}
