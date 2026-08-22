package policy

import (
	"strings"
	"testing"
)

const removeBundleGrants = `{
	"grants": [
		{"src": ["tag:aws-app"], "dst": ["tag:aws-db"], "ip": ["*"]},
	],
}`

func TestRemoveRestoresOriginal(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	res, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if string(res.Policy) != string(orig) {
		t.Errorf("remove did not restore the original\n--- want ---\n%s\n--- got ---\n%s", orig, res.Policy)
	}
	if res.Removed != 1 {
		t.Errorf("Removed = %d, want 1", res.Removed)
	}
}

func TestRemoveTakesWholeContainerItCreated(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	bundle := `{"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}]}`
	applied := applyOK(t, string(orig), bundle, "aws-router")

	res, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if strings.Contains(string(res.Policy), "nodeAttrs") {
		t.Error("a container scurgery created should be removed entirely")
	}
	if string(res.Policy) != string(orig) {
		t.Error("remove did not restore the original")
	}
}

func TestRemoveLeavesContainerSharedWithAnotherNamespace(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	bundleA := `{"nodeAttrs": [{"target": ["tag:a"], "attr": ["funnel"]}]}`
	bundleB := `{"nodeAttrs": [{"target": ["tag:b"], "attr": ["funnel"]}]}`

	a := applyOK(t, string(orig), bundleA, "ns-a")
	b := applyOK(t, string(a.Policy), bundleB, "ns-b")

	res, err := Remove(b.Policy, "ns-a")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got := string(res.Policy)
	if !strings.Contains(got, "tag:b") {
		t.Error("ns-b's element must survive when ns-a is removed from a container they share")
	}
	if !strings.Contains(got, `"nodeAttrs"`) {
		t.Error("a container another namespace still uses must not be dropped")
	}

	nss, err := Namespaces(res.Policy)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	found := false
	for _, n := range nss {
		if n == "ns-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("Namespaces = %v, want ns-b still present", nss)
	}
}

func TestRemoveLeavesOperatorContainerInPlace(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	res, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got := string(res.Policy)
	if !strings.Contains(got, `"tagOwners"`) {
		t.Error("a container the operator owned must survive")
	}
	if !strings.Contains(got, `"tag:subnet-router"`) {
		t.Error("the operator's own members must survive")
	}
}

func TestRemoveIgnoresOtherNamespaces(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	a := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	b := applyOK(t, string(a.Policy), `{"tagOwners": {"tag:other": ["autogroup:admin"]}}`, "other-bundle")

	res, err := Remove(b.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got := string(res.Policy)
	if strings.Contains(got, "tag:aws-app") {
		t.Error("own namespace should be removed")
	}
	if !strings.Contains(got, "tag:other") {
		t.Error("another namespace must be left alone")
	}
}

func TestRemoveOnAbsentNamespaceRemovesNothing(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	res, err := Remove(orig, "not-installed")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0", res.Removed)
	}
	if string(res.Policy) != string(orig) {
		t.Error("removing an absent namespace must not change the policy")
	}
}

func TestRemoveArrayElementRestoresOriginalByteForByte(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), removeBundleGrants, "aws-router")

	res, err := Remove(applied.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if string(res.Policy) != string(orig) {
		t.Errorf("remove did not restore the original byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", orig, res.Policy)
	}
	if res.Removed != 1 {
		t.Errorf("Removed = %d, want 1", res.Removed)
	}
}

func TestNamespacesLists(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	a := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	b := applyOK(t, string(a.Policy), `{"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}]}`, "other")
	// Applied last, but alphabetically sorts in the middle: if Namespaces
	// relied on map iteration order instead of sorting, this would only
	// coincidentally land in the right place.
	c := applyOK(t, string(b.Policy), `{"tagOwners": {"tag:mid": ["autogroup:admin"]}}`, "middle")

	got, err := Namespaces(c.Policy)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	want := []string{"aws-router", "middle", "other"}
	if len(got) != len(want) {
		t.Fatalf("Namespaces = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Namespaces = %v, want %v sorted", got, want)
			break
		}
	}
}

func TestNamespacesFindsArrayElementMarker(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	// removeBundleGrants merges into the fixture's existing grants array, so
	// the only marker scurgery leaves is on the new element itself: no
	// owns-key marker on any object key exists to find it by instead.
	applied := applyOK(t, string(orig), removeBundleGrants, "aws-router")

	got, err := Namespaces(applied.Policy)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	found := false
	for _, ns := range got {
		if ns == "aws-router" {
			found = true
		}
	}
	if !found {
		t.Errorf("Namespaces = %v, want aws-router (marked only on an array element)", got)
	}
}

// TestRemoveDropsOwnedContainerWhoseOnlyMarkersAreItsOwn pins
// containerSharedWithOtherNamespace's distinction between "another namespace
// marked something in here" and "this namespace marked something in here
// twice". A container ns created (owns-key), into which a later apply of the
// same ns added a second element, must still be removed as a whole: the
// child's own "// scurgery:ns" marker is not some other namespace sharing
// the container, it's ns's own second contribution to it.
//
// A mutant that drops the "!= ns" half of otherNamespaceMarker's condition
// treats that child marker as foreign, so containerSharedWithOtherNamespace
// wrongly reports the container shared and Remove leaves the (now emptied)
// key behind. Namespaces then still finds the surviving owns-key marker and
// reports ns present, RemoveBlockers finds no real blocker since nothing
// else actually marks the container, and runRemove's caller is left with an
// "unidentifiable presence" error it can never resolve: the container's
// removal is refused forever, which is exactly the bug this test exists to
// catch.
func TestRemoveDropsOwnedContainerWhoseOnlyMarkersAreItsOwn(t *testing.T) {
	orig := loadFixture(t, "minimal.hujson")

	first := applyOK(t, string(orig), `{"nodeAttrs": [{"target": ["tag:a"], "attr": ["funnel"]}]}`, "aws-router")
	if !strings.Contains(string(first.Policy), "owns-key") {
		t.Fatal("fixture setup failed: the first apply should have created nodeAttrs with an owns-key marker")
	}

	second := applyOK(t, string(first.Policy), `{"nodeAttrs": [
		{"target": ["tag:a"], "attr": ["funnel"]},
		{"target": ["tag:b"], "attr": ["funnel"]}
	]}`, "aws-router")
	if second.Added != 1 {
		t.Fatalf("second apply Added = %d, want 1 (only tag:b is new)", second.Added)
	}

	res, err := Remove(second.Policy, "aws-router")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if strings.Contains(string(res.Policy), "nodeAttrs") {
		t.Error("nodeAttrs was created entirely by aws-router across two applies and should be removed as a whole")
	}
	if string(res.Policy) != string(orig) {
		t.Errorf("remove did not restore the original byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", orig, res.Policy)
	}

	names, err := Namespaces(res.Policy)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("Namespaces = %v, want none: aws-router should be fully gone", names)
	}
}

func TestRemoveStructuralWhenMarkersAreGone(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")

	res, err := RemoveStructural([]byte(stripped), []byte(bundleTagOwners))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if res.Removed != 1 {
		t.Errorf("Removed = %d, want 1", res.Removed)
	}
	if strings.Contains(string(res.Policy), "tag:aws-app") {
		t.Error("structural removal should have removed the member")
	}
	if len(res.Unmatched) != 0 {
		t.Errorf("Unmatched = %v, want none", res.Unmatched)
	}
}

func TestRemoveStructuralRestoresOriginalByteForByte(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")

	res, err := RemoveStructural([]byte(stripped), []byte(bundleTagOwners))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if string(res.Policy) != string(orig) {
		t.Errorf("RemoveStructural did not restore the original byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", orig, res.Policy)
	}
}

// The round-trip matrix in roundtrip_test.go never reaches setTrailingComma's
// relocate-the-comment branch: every trailing-comma transition it exercises
// either sets a nil AfterExtra to the sentinel, or leaves an already-non-nil
// AfterExtra alone, because Apply always resets a freshly appended member's
// AfterExtra to nil first. Relocation only fires when the member that ends
// up last already carries real comment text and the container's want flips
// to false, which structural removal here reaches directly: tag:b is the
// true last member with no trailing comma at the container's end (had is
// false), and removing it exposes tag:a, whose own comment sits before its
// own (non-trailing) comma from the original parse. Confirmed by hand: a
// discard-only setTrailingComma (the bug this branch exists to prevent)
// silently drops "/* keep me */" here instead of relocating it.
func TestRemoveStructuralRelocatesACommentWhenClearingTrailingComma(t *testing.T) {
	policy := `{
	"tagOwners": {
		"tag:a": ["autogroup:admin"] /* keep me */,
		"tag:b": ["autogroup:admin"]
	}
}
`
	bundle := `{"tagOwners": {"tag:b": ["autogroup:admin"]}}`
	want := `{
	"tagOwners": {
		"tag:a": ["autogroup:admin"] /* keep me */
	}
}
`

	res, err := RemoveStructural([]byte(policy), []byte(bundle))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if res.Removed != 1 {
		t.Errorf("Removed = %d, want 1", res.Removed)
	}
	if string(res.Policy) != want {
		t.Errorf("relocated comment did not survive\n--- want ---\n%s\n--- got ---\n%s", want, res.Policy)
	}
}

func TestRemoveStructuralReportsAbsentMembers(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	res, err := RemoveStructural(orig, []byte(bundleTagOwners))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0", res.Removed)
	}
	if len(res.Unmatched) != 1 || !strings.Contains(res.Unmatched[0], "tag:aws-app") {
		t.Errorf("Unmatched = %v, want one entry naming tag:aws-app", res.Unmatched)
	}
}

func TestRemoveStructuralWillNotRemoveAnEditedMember(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), bundleTagOwners, "aws-router")

	// Markers gone, and the operator has since changed the value.
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")
	edited := strings.Replace(stripped, `["autogroup:admin", "tag:aws-app"]`, `["group:eng"]`, 1)
	if edited == stripped {
		t.Fatal("fixture setup failed: the value was not edited")
	}

	res, err := RemoveStructural([]byte(edited), []byte(bundleTagOwners))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0: an edited member must not be removed", res.Removed)
	}
	if !strings.Contains(string(res.Policy), "group:eng") {
		t.Error("the operator's edited value must survive untouched")
	}
	if len(res.Unmatched) != 1 || !strings.Contains(res.Unmatched[0], "tag:aws-app") {
		t.Errorf("Unmatched = %v, want one entry naming tag:aws-app", res.Unmatched)
	}
}

func TestRemoveStructuralWillNotRemoveAnEditedArrayElement(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	applied := applyOK(t, string(orig), removeBundleGrants, "aws-router")

	// Markers gone, and the operator has since changed the value.
	stripped := strings.ReplaceAll(string(applied.Policy), "// scurgery:aws-router\n", "")
	edited := strings.Replace(stripped, `["tag:aws-db"]`, `["tag:aws-other"]`, 1)
	if edited == stripped {
		t.Fatal("fixture setup failed: the value was not edited")
	}

	res, err := RemoveStructural([]byte(edited), []byte(removeBundleGrants))
	if err != nil {
		t.Fatalf("RemoveStructural: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0: an edited element must not be removed", res.Removed)
	}
	if len(res.Unmatched) != 1 {
		t.Errorf("Unmatched = %v, want one entry", res.Unmatched)
	}
	if !strings.Contains(string(res.Policy), `"src": ["*"]`) || !strings.Contains(string(res.Policy), `"dst": ["*"]`) {
		t.Error("the operator's original catch-all grant must survive; an unconditional match would delete it instead")
	}
}
