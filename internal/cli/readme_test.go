package cli

import (
	"fmt"
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

// Callers branch on these codes, so documentation that drifts from the
// constants is a wrong answer rather than a stale sentence.
func TestExitCodesAreDocumentedWherePromised(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(b)

	for _, code := range []int{exitOK, exitChanged, exitUsage, exitError} {
		if row := fmt.Sprintf("| %d |", code); !strings.Contains(readme, row) {
			t.Errorf("README's exit code table has no row %q", row)
		}
		if line := fmt.Sprintf("\n  %d  ", code); !strings.Contains(usage, line) {
			t.Errorf("usage text does not document exit code %d", code)
		}
	}
}

// The config file's path and its place in the precedence order are both
// things an operator acts on, so documentation that drifts from the code is a
// wrong answer rather than a stale sentence. The path is derived from
// defaultConfigPath rather than written out twice.
func TestReadmeDocumentsTheConfigFile(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(b)

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/operator")
	documented := "~/" + strings.TrimPrefix(defaultConfigPath(), "/home/operator/")
	if !strings.Contains(readme, documented) {
		t.Errorf("README should name the config file as %s", documented)
	}

	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if !strings.Contains(readme, "$XDG_CONFIG_HOME/scurgery/config") {
		t.Errorf("README should document the XDG location, which resolves to %s", defaultConfigPath())
	}
	if !strings.Contains(usage, "XDG_CONFIG_HOME") {
		t.Error("usage text should point at the config file too")
	}

	if !strings.Contains(readme, "environment beating the file is deliberate") {
		t.Error("README should explain that the environment takes precedence over the config file")
	}
	if !strings.Contains(readme, "the only keys the file may set") {
		t.Error("README should document that the key set is closed")
	}
}
