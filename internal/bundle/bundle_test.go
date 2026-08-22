package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDerivesNameFromFilename(t *testing.T) {
	p := write(t, "aws-router.hujson", `{"tagOwners": {"tag:a": ["autogroup:admin"]}}`)
	b, err := Load(p, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Name != "aws-router" {
		t.Errorf("Name = %q, want %q", b.Name, "aws-router")
	}
}

func TestLoadNameOverride(t *testing.T) {
	p := write(t, "aws-router.hujson", `{"tagOwners": {"tag:a": ["autogroup:admin"]}}`)
	b, err := Load(p, "custom")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Name != "custom" {
		t.Errorf("Name = %q, want %q", b.Name, "custom")
	}
}

func TestLoadRejectsUnparseable(t *testing.T) {
	p := write(t, "bad.hujson", "{not json")
	if _, err := Load(p, ""); err == nil {
		t.Error("Load should reject an unparseable bundle")
	}
}

func TestLoadRejectsNonObject(t *testing.T) {
	p := write(t, "arr.hujson", `["not", "an", "object"]`)
	if _, err := Load(p, ""); err == nil {
		t.Error("Load should reject a bundle that is not a JSON object")
	}
}

func TestLoadRejectsNameDerivedFromBadFilename(t *testing.T) {
	p := write(t, "has space.hujson", `{"tagOwners": {}}`)
	if _, err := Load(p, ""); err == nil {
		t.Error("Load should reject a filename that yields an invalid namespace")
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.hujson"), ""); err == nil {
		t.Error("Load should report a missing file")
	}
}
