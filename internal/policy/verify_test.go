package policy

import (
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestVerifyApplyAcceptsAGoodMerge(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	if err := VerifyApply(orig, applied.Policy, "aws-router"); err != nil {
		t.Errorf("VerifyApply = %v, want nil", err)
	}
}

func TestVerifyApplyRejectsCollateralDamage(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	// Simulate a bug that also disturbed something the operator owned.
	damaged := strings.Replace(string(applied.Policy), `"tag:subnet-router"`, `"tag:clobbered"`, 1)

	if err := VerifyApply(orig, []byte(damaged), "aws-router"); err == nil {
		t.Error("VerifyApply must reject a result that changed anything beyond its own markers")
	}
}

func TestVerifyRemoveAcceptsAGoodRemoval(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	removed, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := VerifyRemove(applied.Policy, removed.Policy, "aws-router"); err != nil {
		t.Errorf("VerifyRemove = %v, want nil", err)
	}
}

func TestVerifyRemoveRejectsLostOperatorKey(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	// Simulate a bug that dropped a top-level key the operator owned, leaving
	// every other member untouched so this isolates the top-level-key-survival
	// check from the separate comment/formatting check: a hand-written literal
	// like `{"grants": []}` would also differ in whitespace on the keys it
	// does keep, and get caught by that check first instead.
	root, err := hujson.Parse(applied.Policy)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := root.Value.(*hujson.Object)
	var kept []hujson.ObjectMember
	for _, m := range obj.Members {
		if memberName(m) == "tagOwners" {
			continue
		}
		kept = append(kept, m)
	}
	obj.Members = kept
	damaged := root.Pack()

	if err := VerifyRemove(applied.Policy, damaged, "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result that dropped a key it did not own")
	}
}

func TestVerifyRemoveRejectsLeftoverMarkers(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	if err := VerifyRemove(applied.Policy, applied.Policy, "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result that still carries the namespace")
	}
}

// No existing test covers ns already being present in before: updating an
// already-installed namespace must be accepted, not treated as collateral
// damage just because before itself still carries the namespace's markers.
func TestVerifyApplyAcceptsAReapply(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	first := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	const updated = `{
		"tagOwners": {
			"tag:aws-app": ["autogroup:admin", "tag:aws-app", "tag:extra"],
		},
	}`
	// The changed member already carries aws-router's own marker from the
	// first apply, so this is an update to scurgery's own value, not a
	// collision with something else's content: no --force is needed.
	second, err := Apply(first.Policy, []byte(updated), "aws-router", ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(second.Conflicts) != 0 {
		t.Fatalf("Conflicts = %d, want 0: updating a value scurgery's own marker already covers must not conflict", len(second.Conflicts))
	}
	if err := VerifyApply(first.Policy, second.Policy, "aws-router"); err != nil {
		t.Errorf("VerifyApply = %v, want nil: an update to an already-installed namespace is legitimate", err)
	}
}

// The entire contents of a surviving container are wiped, destroying the
// operator's own tags. This is the disaster this gate exists to prevent.
func TestVerifyRemoveRejectsWipedContainer(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	root, err := hujson.Parse(applied.Policy)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := root.Value.(*hujson.Object)
	tagOwners := findMember(obj, "tagOwners")
	if tagOwners == nil {
		t.Fatal("fixture setup failed: tagOwners not found")
	}
	empty, err := hujson.Parse([]byte("{}"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tagOwners.Value.Value = empty.Value
	damaged := root.Pack()

	if err := VerifyRemove(applied.Policy, damaged, "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result whose container contents were wiped")
	}
}

// An operator value is rewritten in place rather than left untouched.
func TestVerifyRemoveRejectsRewrittenOperatorValue(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	removed, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	damaged := strings.Replace(string(removed.Policy), `["autogroup:admin"]`, `["group:eng"]`, 1)
	if damaged == string(removed.Policy) {
		t.Fatal("fixture setup failed: value was not rewritten")
	}

	if err := VerifyRemove(applied.Policy, []byte(damaged), "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result that rewrote an operator's value")
	}
}

// Another namespace's contributions are collaterally destroyed while
// removing a different namespace.
func TestVerifyRemoveRejectsDestroyedOtherNamespace(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	a := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	b := applyOK(t, string(a.Policy), `{"tagOwners": {"tag:other": ["autogroup:admin"]}}`, "other-bundle")

	removed, err := Remove(b.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	root, err := hujson.Parse(removed.Policy)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := root.Value.(*hujson.Object)
	tagOwners := findMember(obj, "tagOwners")
	if tagOwners == nil {
		t.Fatal("fixture setup failed: tagOwners not found")
	}
	inner := tagOwners.Value.Value.(*hujson.Object)
	var kept []hujson.ObjectMember
	found := false
	for _, m := range inner.Members {
		if memberName(m) == "tag:other" {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		t.Fatal("fixture setup failed: tag:other not found")
	}
	inner.Members = kept
	damaged := root.Pack()

	if err := VerifyRemove(b.Policy, damaged, "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result that destroyed another namespace's contribution")
	}
}

// An operator comment attached to a top-level key is deleted. Content
// comparison alone cannot see this: semanticEqual strips comments by
// design, so this can only be caught by comparing the retained member's own
// leading comment byte-for-byte.
func TestVerifyRemoveRejectsDeletedOperatorComment(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	removed, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	damaged := strings.Replace(string(removed.Policy), "// The self-references are load bearing.\n\t", "", 1)
	if damaged == string(removed.Policy) {
		t.Fatal("fixture setup failed: comment was not removed")
	}

	if err := VerifyRemove(applied.Policy, []byte(damaged), "aws-router"); err == nil {
		t.Error("VerifyRemove must reject a result that deleted an operator's comment")
	}
}

// TestVerifyRemoveStructuralAcceptsAGoodRemoval confirms the object-branch
// path of the independent structural self-check accepts a removal it agrees
// with, using RemoveStructural's own output as the "after" it checks.
func TestVerifyRemoveStructuralAcceptsAGoodRemoval(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")

	res, err := RemoveStructural([]byte(stripped), []byte(bundleTagOwners))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if err := VerifyRemoveStructural([]byte(stripped), res.Policy, []byte(bundleTagOwners)); err != nil {
		t.Errorf("VerifyRemoveStructural = %v, want nil", err)
	}
}

// TestVerifyRemoveStructuralAcceptsAGoodArrayRemoval is the array-branch
// counterpart of the test above, using removeBundleGrants instead of
// bundleTagOwners so the exercised container is an array, not an object.
func TestVerifyRemoveStructuralAcceptsAGoodArrayRemoval(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), removeBundleGrants, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")

	res, err := RemoveStructural([]byte(stripped), []byte(removeBundleGrants))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if err := VerifyRemoveStructural([]byte(stripped), res.Policy, []byte(removeBundleGrants)); err != nil {
		t.Errorf("VerifyRemoveStructural = %v, want nil", err)
	}
}

// TestVerifyRemoveStructuralRejectsDeletingAnEditedMember is the governing-
// principle case: an operator has changed a member's value since scurgery
// installed it, so it no longer matches the bundle (see
// TestRemoveStructuralWillNotRemoveAnEditedMember, which confirms
// RemoveStructural itself will not touch it). If a bug deleted it anyway,
// VerifyRemoveStructural must catch that independently, without calling
// RemoveStructural or consulting a marker that no longer exists.
func TestVerifyRemoveStructuralRejectsDeletingAnEditedMember(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")
	edited := strings.Replace(stripped, `["autogroup:admin", "tag:aws-app"]`, `["group:eng"]`, 1)
	if edited == stripped {
		t.Fatal("fixture setup failed: the value was not edited")
	}

	root, err := hujson.Parse([]byte(edited))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := root.Value.(*hujson.Object)
	tagOwners := findMember(obj, "tagOwners")
	if tagOwners == nil {
		t.Fatal("fixture setup failed: tagOwners not found")
	}
	inner := tagOwners.Value.Value.(*hujson.Object)
	var kept []hujson.ObjectMember
	found := false
	for _, m := range inner.Members {
		if memberName(m) == "tag:aws-app" {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		t.Fatal("fixture setup failed: tag:aws-app not found")
	}
	inner.Members = kept
	damaged := root.Pack()

	if err := VerifyRemoveStructural([]byte(edited), damaged, []byte(bundleTagOwners)); err == nil {
		t.Error("VerifyRemoveStructural must reject deleting an operator-edited member the bundle no longer matches")
	}
}

// TestVerifyRemoveStructuralRejectsDeletingAnEditedArrayElement is the
// array-branch counterpart: an operator has changed a grant's destination
// since scurgery installed it (see
// TestRemoveStructuralWillNotRemoveAnEditedArrayElement), and a bug deletes
// it anyway.
func TestVerifyRemoveStructuralRejectsDeletingAnEditedArrayElement(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), removeBundleGrants, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")
	edited := strings.Replace(stripped, `["tag:aws-db"]`, `["tag:aws-other"]`, 1)
	if edited == stripped {
		t.Fatal("fixture setup failed: the value was not edited")
	}

	root, err := hujson.Parse([]byte(edited))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj := root.Value.(*hujson.Object)
	grants := findMember(obj, "grants")
	if grants == nil {
		t.Fatal("fixture setup failed: grants not found")
	}
	arr := grants.Value.Value.(*hujson.Array)
	idx := -1
	for i, el := range arr.Elements {
		if strings.Contains(compactString(el), "tag:aws-other") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("fixture setup failed: edited element not found")
	}
	arr.Elements = append(arr.Elements[:idx], arr.Elements[idx+1:]...)
	damaged := root.Pack()

	if err := VerifyRemoveStructural([]byte(edited), damaged, []byte(removeBundleGrants)); err == nil {
		t.Error("VerifyRemoveStructural must reject deleting an operator-edited array element the bundle no longer matches")
	}
}
