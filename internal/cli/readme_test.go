package cli

import (
	"os"
	"strings"
	"testing"
)

// TestReadmeDocumentsSharedMemberLimitation pins the README's description of
// the shared-member limitation next to the CLI note that reports it, so a
// future edit cannot silently drop the documentation while the behaviour it
// describes stays in place.
func TestReadmeDocumentsSharedMemberLimitation(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(b)
	if !strings.Contains(readme, "A member two bundles both declare") {
		t.Error("README should document that a member two bundles both declare is owned by whichever installed it first")
	}
	if !strings.Contains(readme, "Removing the owning namespace later removes that member too") {
		t.Error("README should explain the consequence: removing the owning namespace removes the shared member too")
	}
}
