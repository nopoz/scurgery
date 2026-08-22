package policy

import "testing"

func TestSemanticEqualIgnoresFormatting(t *testing.T) {
	a := mustParse(t, `{"src":["*"],"dst":["*"]}`)
	b := mustParse(t, "{\n // a comment \n \"dst\": [\"*\"],\n \"src\": [\"*\"], \n}")
	if !semanticEqual(a, b) {
		t.Error("values differing only in comments, order and trailing commas should compare equal")
	}
}

func TestSemanticEqualDetectsRealDifference(t *testing.T) {
	a := mustParse(t, `["autogroup:admin"]`)
	b := mustParse(t, `["group:eng"]`)
	if semanticEqual(a, b) {
		t.Error("different values must not compare equal")
	}
}

func TestSemanticEqualIsOrderSensitiveForArrays(t *testing.T) {
	a := mustParse(t, `["a","b"]`)
	b := mustParse(t, `["b","a"]`)
	if semanticEqual(a, b) {
		t.Error("array order is meaningful in a policy file and must not be ignored")
	}
}

func TestCompactString(t *testing.T) {
	v := mustParse(t, "[\n // c \n \"a\", \n]")
	if got, want := compactString(v), `["a"]`; got != want {
		t.Errorf("compactString = %q, want %q", got, want)
	}
}
