package policy

import (
	"bytes"
	"os"
	"testing"

	"github.com/tailscale/hujson"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func TestParsePackIsByteIdentical(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	v, err := hujson.Parse(orig)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := v.Pack(); !bytes.Equal(got, orig) {
		t.Errorf("Parse then Pack changed the bytes\n--- want ---\n%s\n--- got ---\n%s", orig, got)
	}
}

func TestApplyThenRemoveIsByteIdentical(t *testing.T) {
	fixtures := []string{
		"realistic.hujson",
		"minimal.hujson",
		"no-trailing-commas.hujson",
		"empty-containers.hujson",
		"comment-before-comma.hujson",
	}
	bundles := map[string]string{
		"merge into object": `{"tagOwners": {"tag:new": ["autogroup:admin"]}}`,
		"append to array":   `{"grants": [{"src": ["tag:x"], "dst": ["tag:y"], "ip": ["*"]}]}`,
		"new container":     `{"nodeAttrs": [{"target": ["tag:x"], "attr": ["funnel"]}]}`,
		// No autoApprovers here: realistic.hujson already defines
		// autoApprovers.routes, and merging is one level deep by design, so a
		// second routes value is a conflict rather than a deep merge. That
		// behaviour is pinned by TestApplyDoesNotDeepMergeNestedObjects.
		"several at once": `{
			"tagOwners": {"tag:new": ["autogroup:admin"]},
			"grants":    [{"src": ["tag:x"], "dst": ["tag:y"], "ip": ["*"]}],
			"ssh":       [{"action": "accept", "src": ["tag:x"], "dst": ["tag:y"], "users": ["root"]}],
		}`,
	}

	for _, f := range fixtures {
		for name, b := range bundles {
			t.Run(f+"/"+name, func(t *testing.T) {
				orig := loadFixture(t, f)

				applied, err := Apply(orig, []byte(b), "roundtrip", ApplyOptions{})
				if err != nil {
					t.Fatalf("Apply: %v", err)
				}
				if len(applied.Conflicts) > 0 {
					t.Fatalf("unexpected conflicts: %+v", applied.Conflicts)
				}

				removed, err := Remove(applied.Policy, "roundtrip")
				if err != nil {
					t.Fatalf("Remove: %v", err)
				}
				if string(removed.Policy) != string(orig) {
					t.Errorf("not byte-identical\n--- want ---\n%s\n--- got ---\n%s", orig, removed.Policy)
				}
			})
		}
	}
}

// Applying twice then removing once must still restore the original, because
// the second apply is a no-op.
func TestDoubleApplyThenRemoveIsByteIdentical(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	const b = `{"tagOwners": {"tag:new": ["autogroup:admin"]}}`

	a1, err := Apply(orig, []byte(b), "roundtrip", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply 1: %v", err)
	}
	a2, err := Apply(a1.Policy, []byte(b), "roundtrip", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	r, err := Remove(a2.Policy, "roundtrip")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if string(r.Policy) != string(orig) {
		t.Errorf("not byte-identical after double apply\n--- want ---\n%s\n--- got ---\n%s", orig, r.Policy)
	}
}

// Two namespaces installed and removed in either order must both restore.
func TestTwoNamespacesRemoveIndependently(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")

	a, err := Apply(orig, []byte(`{"tagOwners": {"tag:a": ["autogroup:admin"]}}`), "alpha", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply alpha: %v", err)
	}
	b, err := Apply(a.Policy, []byte(`{"tagOwners": {"tag:b": ["autogroup:admin"]}}`), "beta", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply beta: %v", err)
	}

	rb, err := Remove(b.Policy, "beta")
	if err != nil {
		t.Fatalf("Remove beta: %v", err)
	}
	if string(rb.Policy) != string(a.Policy) {
		t.Error("removing beta should restore the alpha-only state exactly")
	}
	ra, err := Remove(rb.Policy, "alpha")
	if err != nil {
		t.Fatalf("Remove alpha: %v", err)
	}
	if string(ra.Policy) != string(orig) {
		t.Error("removing both should restore the original exactly")
	}
}
