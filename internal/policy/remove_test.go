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

func TestNamespacesLists(t *testing.T) {
	orig := loadFixture(t, "realistic.hujson")
	a := applyOK(t, string(orig), bundleTagOwners, "aws-router")
	b := applyOK(t, string(a.Policy), `{"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}]}`, "other")

	got, err := Namespaces(b.Policy)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if len(got) != 2 || got[0] != "aws-router" || got[1] != "other" {
		t.Errorf("Namespaces = %v, want [aws-router other] sorted", got)
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
