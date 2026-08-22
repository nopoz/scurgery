package policy

import (
	"strings"
	"testing"
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
	// Simulate a bug that dropped a top-level key the operator owned.
	damaged := `{"grants": []}`

	if err := VerifyRemove(applied.Policy, []byte(damaged), "aws-router"); err == nil {
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
