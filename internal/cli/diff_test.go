package cli

import (
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
