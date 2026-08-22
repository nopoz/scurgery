package policy

import (
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func mustParse(t *testing.T, s string) hujson.Value {
	t.Helper()
	v, err := hujson.Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestTrailingCommaDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"array with trailing comma", "[1, 2,]", true},
		{"array without", "[1, 2]", false},
		{"object with trailing comma", `{"a": 1,}`, true},
		{"object without", `{"a": 1}`, false},
		{"empty array", "[]", false},
		{"empty object", "{}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trailingComma(mustParse(t, tc.in)); got != tc.want {
				t.Errorf("trailingComma(%s) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// The regression this unit exists to prevent: appending to a container and then
// removing what was appended must not disturb the trailing comma.
func TestTrailingCommaSurvivesTailMutation(t *testing.T) {
	const orig = "[1, 2,]"
	v := mustParse(t, orig)
	arr := v.Value.(*hujson.Array)

	had := trailingComma(v)
	arr.Elements = append(arr.Elements, mustParse(t, "3"))
	setTrailingComma(v, had)

	if arr.Elements[1].AfterExtra != nil {
		t.Error("sentinel should have moved off the old last element")
	}
	if arr.Elements[2].AfterExtra == nil {
		t.Error("sentinel should be on the new last element")
	}

	arr.Elements = arr.Elements[:len(arr.Elements)-1]
	setTrailingComma(v, had)

	if got := string(v.Pack()); got != orig {
		t.Errorf("append then remove = %q, want %q", got, orig)
	}
}

func TestSetTrailingCommaClears(t *testing.T) {
	v := mustParse(t, "[1, 2,]")
	setTrailingComma(v, false)
	if got, want := string(v.Pack()), "[1, 2]"; got != want {
		t.Errorf("Pack() = %q, want %q", got, want)
	}
}

func TestSetTrailingCommaOnEmptyContainerIsSafe(t *testing.T) {
	v := mustParse(t, "[]")
	setTrailingComma(v, true) // must not panic
	if got, want := string(v.Pack()), "[]"; got != want {
		t.Errorf("Pack() = %q, want %q", got, want)
	}
}

func TestSetTrailingCommaKeepsCommentWhenClearing(t *testing.T) {
	v := mustParse(t, "[1, 2 /* keep */,]")
	setTrailingComma(v, false)
	got := string(v.Pack())
	if !strings.Contains(got, "/* keep */") {
		t.Errorf("clearing the trailing comma dropped a comment: %q", got)
	}
	if strings.Contains(got, ",]") {
		t.Errorf("trailing comma was not cleared: %q", got)
	}
}
