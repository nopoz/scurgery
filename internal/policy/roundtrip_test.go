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
