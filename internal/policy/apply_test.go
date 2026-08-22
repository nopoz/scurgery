package policy

import (
	"strings"
	"testing"
)

const bundleTagOwners = `{
	"tagOwners": {
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
	},
}`

func applyOK(t *testing.T, policy, bundle, ns string) *ApplyResult {
	t.Helper()
	res, err := Apply([]byte(policy), []byte(bundle), ns, ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}
	return res
}

func TestApplyMergesIntoExistingObject(t *testing.T) {
	res := applyOK(t, string(loadFixture(t, "realistic.hujson")), bundleTagOwners, "aws-router")
	got := string(res.Policy)

	if strings.Count(got, `"tagOwners"`) != 1 {
		t.Error("must merge into the existing tagOwners, not emit a second key")
	}
	if !strings.Contains(got, "// scurgery:aws-router\n") {
		t.Error("added member should carry a marker")
	}
	if !strings.Contains(got, `"tag:subnet-router"`) {
		t.Error("existing members must survive")
	}
	if res.Added != 1 {
		t.Errorf("Added = %d, want 1", res.Added)
	}
}

func TestApplyCreatesAbsentContainerWithKeyMarker(t *testing.T) {
	bundle := `{"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}]}`
	res := applyOK(t, string(loadFixture(t, "realistic.hujson")), bundle, "aws-router")
	got := string(res.Policy)

	if !strings.Contains(got, "// scurgery:aws-router owns-key") {
		t.Error("a container scurgery created should carry the owns-key marker")
	}
	if !strings.Contains(got, `"nodeAttrs"`) {
		t.Error("the new container should be present")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	orig := string(loadFixture(t, "realistic.hujson"))
	once := applyOK(t, orig, bundleTagOwners, "aws-router")
	twice := applyOK(t, string(once.Policy), bundleTagOwners, "aws-router")

	if string(twice.Policy) != string(once.Policy) {
		t.Error("applying twice must not change the policy a second time")
	}
	if twice.Added != 0 || twice.Skipped != 1 {
		t.Errorf("second apply: Added=%d Skipped=%d, want 0 and 1", twice.Added, twice.Skipped)
	}
}

// The API accepts duplicate array elements silently, so this check is the only
// thing preventing them.
func TestApplyDoesNotDuplicateArrayElements(t *testing.T) {
	bundle := `{"grants": [{"src": ["*"], "dst": ["*"], "ip": ["*"]}]}`
	orig := string(loadFixture(t, "realistic.hujson"))
	res := applyOK(t, orig, bundle, "aws-router")

	if res.Added != 0 || res.Skipped != 1 {
		t.Errorf("Added=%d Skipped=%d, want 0 and 1: that grant already exists", res.Added, res.Skipped)
	}
	if string(res.Policy) != orig {
		t.Error("a no-op apply must not alter the policy")
	}
}

func TestApplyRefusesOnConflict(t *testing.T) {
	bundle := `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`
	res, err := Apply(loadFixture(t, "realistic.hujson"), []byte(bundle), "aws-router", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1", len(res.Conflicts))
	}
	if res.Policy != nil {
		t.Error("a blocked apply must return no policy, so a caller cannot write it by accident")
	}
	c := res.Conflicts[0]
	if !strings.Contains(c.Existing, "autogroup:admin") || !strings.Contains(c.Incoming, "group:eng") {
		t.Errorf("conflict should show both values, got %+v", c)
	}
}

func TestApplyForceOverwritesConflict(t *testing.T) {
	bundle := `{"tagOwners": {"tag:subnet-router": ["group:eng"]}}`
	res, err := Apply(loadFixture(t, "realistic.hujson"), []byte(bundle), "aws-router", ApplyOptions{Force: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Policy == nil {
		t.Fatal("force should produce a policy")
	}
	if !strings.Contains(string(res.Policy), "group:eng") {
		t.Error("force should have overwritten the value")
	}
}

func TestApplySkipConflictsInstallsTheRest(t *testing.T) {
	bundle := `{
		"tagOwners": {
			"tag:subnet-router": ["group:eng"],
			"tag:aws-app":       ["autogroup:admin", "tag:aws-app"],
		},
	}`
	res, err := Apply(loadFixture(t, "realistic.hujson"), []byte(bundle), "aws-router", ApplyOptions{SkipConflicts: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Policy == nil {
		t.Fatal("skip-conflicts should produce a policy")
	}
	got := string(res.Policy)
	if !strings.Contains(got, `"tag:aws-app"`) {
		t.Error("the non-conflicting member should be installed")
	}
	if strings.Contains(got, "group:eng") {
		t.Error("the conflicting member must not be installed")
	}
	if res.Added != 1 || len(res.Conflicts) != 1 {
		t.Errorf("Added=%d Conflicts=%d, want 1 and 1", res.Added, len(res.Conflicts))
	}
}

// Merging is one level deep: a bundle key merges into the matching top-level
// container, but a member of that container is replaced or refused, never
// deep-merged. Pinning this stops a future change from silently widening
// access by union-ing nested values.
func TestApplyDoesNotDeepMergeNestedObjects(t *testing.T) {
	bundle := `{"autoApprovers": {"routes": {"10.0.0.0/8": ["tag:x"]}}}`
	res, err := Apply(loadFixture(t, "realistic.hujson"), []byte(bundle), "aws-router", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1: routes already exists and must not be deep-merged", len(res.Conflicts))
	}
	if res.Conflicts[0].Path != `autoApprovers."routes"` {
		t.Errorf("Conflict.Path = %q, want %q", res.Conflicts[0].Path, `autoApprovers."routes"`)
	}
	if res.Policy != nil {
		t.Error("a blocked apply must return no policy")
	}
}

func TestApplyRejectsBadNamespace(t *testing.T) {
	if _, err := Apply(loadFixture(t, "realistic.hujson"), []byte(bundleTagOwners), "bad ns", ApplyOptions{}); err == nil {
		t.Error("Apply should reject a namespace containing whitespace")
	}
}

func TestApplyRejectsUnparseableInput(t *testing.T) {
	if _, err := Apply([]byte("{not json"), []byte(bundleTagOwners), "x", ApplyOptions{}); err == nil {
		t.Error("Apply should reject an unparseable policy")
	}
	if _, err := Apply(loadFixture(t, "realistic.hujson"), []byte("{not json"), "x", ApplyOptions{}); err == nil {
		t.Error("Apply should reject an unparseable bundle")
	}
}

// bundleGolden exercises all appending paths in one Apply call: two members
// appended into an existing object (tagOwners), two elements appended into
// an existing array (grants), and two brand new top-level keys (nodeAttrs,
// acls). Each of the merged containers already carries a trailing comma, or
// becomes a new root member, so comparing full bytes against a golden file
// is the only way to catch a broken trailing-comma restore: string-matching
// tests can't see a comma.
//
// The first appended member of tagOwners, the first appended element of
// grants, and the first (non-last) of the two new top-level keys,
// nodeAttrs, each carry a bundle-only comment before their separating
// comma. hujson attaches that comment to the element's own AfterExtra
// rather than the container's, precisely because another element follows
// it, so this is also the only way to catch a dropped `AfterExtra = nil`
// at any of those three sites: without the reset, the bundle's own
// formatting leaks into the merged policy. A single new top-level key
// would not exercise this for the root-append site, since it would always
// be the last root member and get corrected by the final
// setTrailingComma(root, rootHad) regardless of the per-member reset.
const bundleGolden = `{
	"tagOwners": {
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"] /* bundle-only comment */,
		"tag:aws-app-2": ["autogroup:admin"],
	},
	"grants": [
		{"src": ["tag:aws-app"], "dst": ["tag:aws-subnet-router"], "ip": ["443"]} /* bundle-only comment */,
		{"src": ["tag:aws-app"], "dst": ["tag:aws-subnet-router"], "ip": ["444"]},
	],
	"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}] /* bundle-only comment */,
	"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}],
}`

func TestApplyMatchesGoldenOutput(t *testing.T) {
	res := applyOK(t, string(loadFixture(t, "realistic.hujson")), bundleGolden, "aws-router")
	want := string(loadFixture(t, "realistic-applied.golden.hujson"))
	got := string(res.Policy)
	if got != want {
		t.Errorf("Policy does not match golden output\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if res.Added != 6 {
		t.Errorf("Added = %d, want 6", res.Added)
	}
}

// TestApplyOnTopLevelScalar exercises the default (non-container) branch,
// which realistic.hujson never triggers since it has no top-level scalar.
// randomizeClientPort and disableIPv4 are real top-level scalar policy
// fields, so this path is reachable in production.
func TestApplyOnTopLevelScalar(t *testing.T) {
	const scalarPolicy = `{"randomizeClientPort": true}`
	const scalarBundle = `{"randomizeClientPort": false}`

	res, err := Apply([]byte(scalarPolicy), []byte(scalarBundle), "aws-router", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1", len(res.Conflicts))
	}
	if res.Conflicts[0].Path != "randomizeClientPort" {
		t.Errorf("Conflict.Path = %q, want %q", res.Conflicts[0].Path, "randomizeClientPort")
	}
	if res.Policy != nil {
		t.Error("a blocked apply must return no policy")
	}

	forced, err := Apply([]byte(scalarPolicy), []byte(scalarBundle), "aws-router", ApplyOptions{Force: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if forced.Policy == nil {
		t.Fatal("force should produce a policy")
	}
	if !strings.Contains(string(forced.Policy), "false") {
		t.Error("force should have overwritten the value")
	}
}
