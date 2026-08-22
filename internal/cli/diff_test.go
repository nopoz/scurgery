package cli

import (
	"math/rand"
	"strings"
	"testing"
)

func TestUnifiedShowsAddedLines(t *testing.T) {
	before := "a\nb\nc\n"
	after := "a\nb\nNEW\nc\n"
	got := Unified([]byte(before), []byte(after), 1)
	if !strings.Contains(got, "+NEW") {
		t.Errorf("diff should mark the added line, got:\n%s", got)
	}
	if strings.Contains(got, "-NEW") {
		t.Errorf("diff should not mark the added line as removed, got:\n%s", got)
	}
}

func TestUnifiedShowsRemovedLines(t *testing.T) {
	got := Unified([]byte("a\nGONE\nb\n"), []byte("a\nb\n"), 1)
	if !strings.Contains(got, "-GONE") {
		t.Errorf("diff should mark the removed line, got:\n%s", got)
	}
}

func TestUnifiedOnIdenticalInputIsEmpty(t *testing.T) {
	if got := Unified([]byte("a\nb\n"), []byte("a\nb\n"), 1); got != "" {
		t.Errorf("identical input should produce no diff, got:\n%s", got)
	}
}

func TestUnifiedElidesUnchangedRegions(t *testing.T) {
	before := strings.Repeat("same\n", 40) + "old\n"
	after := strings.Repeat("same\n", 40) + "new\n"
	got := Unified([]byte(before), []byte(after), 2)
	if strings.Count(got, "same") > 6 {
		t.Errorf("unchanged regions should be elided, got:\n%s", got)
	}
	if !strings.Contains(got, "+new") || !strings.Contains(got, "-old") {
		t.Errorf("the actual change should be shown, got:\n%s", got)
	}
}

func TestUnifiedNegativeContextStillShowsChange(t *testing.T) {
	got := Unified([]byte("a\nX\nc\n"), []byte("a\nY\nc\n"), -1)
	if got == "" {
		t.Fatal("negative context should not hide a real change, got empty diff")
	}
	if !strings.Contains(got, "-X") || !strings.Contains(got, "+Y") {
		t.Errorf("negative context should still show the change, got:\n%s", got)
	}
}

// changedLines extracts the '+' and '-' prefixed lines from a diff, in
// order, ignoring context lines and the "  ..." gap marker.
func changedLines(diff string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if l == "" || l == "  ..." {
			continue
		}
		if l[0] == '+' || l[0] == '-' {
			out = append(out, l)
		}
	}
	return out
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reconstruct splits a diff produced with no elision back into the before
// and after line sequences it was built from, by keeping ' '/'-' lines for
// before and ' '/'+' lines for after.
func reconstruct(diff string) (before, after []string) {
	if diff == "" {
		return nil, nil
	}
	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if l == "" {
			continue
		}
		switch l[0] {
		case '-', ' ':
			before = append(before, l[1:])
		}
		switch l[0] {
		case '+', ' ':
			after = append(after, l[1:])
		}
	}
	return before, after
}

// TestUnifiedNeverHidesAChangedLine checks, over a fixed set of generated
// input pairs, that every line of "before" appears in the diff as context or
// a removal and every line of "after" appears as context or an addition,
// except where deliberately elided as unchanged. The identical-input case is
// handled explicitly: Unified is defined to return the empty string there by
// design, which is not a lost line.
//
// Two checks run per trial. First, against ground truth: a diff rendered
// with context wide enough that nothing is elided is reconstructed back into
// its before/after line sequences and compared against the actual generated
// lines, so a mislabeled or dropped line is caught directly rather than by
// comparing the function against itself. Second, that elision at a smaller,
// possibly zero, context never drops or reorders a changed ('+'/'-') line
// relative to the unelided diff, since elision may only remove or collapse
// runs of unchanged context lines.
func TestUnifiedNeverHidesAChangedLine(t *testing.T) {
	alphabet := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	rng := rand.New(rand.NewSource(42))

	randLines := func(n int) []string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return lines
	}

	const trials = 100
	for trial := 0; trial < trials; trial++ {
		beforeLines := randLines(rng.Intn(20))
		afterLines := randLines(rng.Intn(20))
		before := strings.Join(beforeLines, "\n")
		if before != "" {
			before += "\n"
		}
		after := strings.Join(afterLines, "\n")
		if after != "" {
			after += "\n"
		}
		context := rng.Intn(3)

		if before == after {
			if got := Unified([]byte(before), []byte(after), context); got != "" {
				t.Fatalf("trial %d: identical input should produce no diff, got:\n%s", trial, got)
			}
			continue
		}

		full := Unified([]byte(before), []byte(after), len(beforeLines)+len(afterLines)+1)

		gotBefore, gotAfter := reconstruct(full)
		if !linesEqual(gotBefore, beforeLines) {
			t.Fatalf("trial %d: before lines lost, mislabeled, or reordered.\nbefore=%v\nafter=%v\nfull=%q\nreconstructed=%v",
				trial, beforeLines, afterLines, full, gotBefore)
		}
		if !linesEqual(gotAfter, afterLines) {
			t.Fatalf("trial %d: after lines lost, mislabeled, or reordered.\nbefore=%v\nafter=%v\nfull=%q\nreconstructed=%v",
				trial, beforeLines, afterLines, full, gotAfter)
		}

		elided := Unified([]byte(before), []byte(after), context)
		if !linesEqual(changedLines(full), changedLines(elided)) {
			t.Fatalf("trial %d: a changed line was hidden or reordered by elision.\nbefore=%v\nafter=%v\ncontext=%d\nfull=%q\nelided=%q",
				trial, beforeLines, afterLines, context, full, elided)
		}
	}
}
