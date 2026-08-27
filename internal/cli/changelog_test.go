package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The release workflow feeds a CHANGELOG.md section to `gh release create`
// verbatim, and GitHub renders a single newline inside a paragraph as a line
// break. A newline typed at a fixed column therefore reaches readers as a
// ragged break at the wrong width. The check has to live here rather than in
// any local tooling, because the publish happens inside CI where nothing on a
// developer's machine runs.
//
// minWrappedLen is the shortest line that still looks machine-wrapped. A run
// of prose lines that are all at least this long, bar the last, is the
// signature wrapping leaves behind; a deliberately short stanza is not.
const minWrappedLen = 55

// hardWrappedParagraph returns the first line of the first hard-wrapped prose
// run in text, or "" when there is none. Headings, list items, table rows,
// blockquotes, fenced and indented code, and link reference definitions are
// not prose and are skipped: a Keep a Changelog file ends with a stack of link
// references, one per version, and the line breaks between them are the only
// thing holding them apart.
func hardWrappedParagraph(text string) string {
	isStructural := func(line string) bool {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			return true
		case strings.HasPrefix(t, "#"), strings.HasPrefix(t, "|"), strings.HasPrefix(t, ">"):
			return true
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "+ "):
			return true
		case strings.HasPrefix(line, "    "), strings.HasPrefix(line, "\t"):
			return true
		case strings.HasPrefix(t, "[") && strings.Contains(t, "]:"):
			return true
		}
		return false
	}

	var run []string
	wrapped := func() string {
		if len(run) < 2 {
			return ""
		}
		for _, line := range run[:len(run)-1] {
			if len(strings.TrimRight(line, " \t")) < minWrappedLen {
				return ""
			}
		}
		return strings.TrimSpace(run[0])
	}

	inFence := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			if hit := wrapped(); hit != "" {
				return hit
			}
			run = nil
			continue
		}
		if inFence || isStructural(line) {
			if hit := wrapped(); hit != "" {
				return hit
			}
			run = nil
			continue
		}
		run = append(run, line)
	}
	return wrapped()
}

func TestChangelogParagraphsAreNotHardWrapped(t *testing.T) {
	path := filepath.Join("..", "..", "CHANGELOG.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if hit := hardWrappedParagraph(string(data)); hit != "" {
		t.Errorf("CHANGELOG.md has a hard-wrapped paragraph starting %q.\n"+
			"The release workflow publishes these sections as release notes, where a newline "+
			"inside a paragraph renders as a line break. Keep each paragraph and each list "+
			"item on one line, however long.", hit)
	}
}

// The file above passes today, so on its own it would not prove the detector
// can fail. These cases pin what it does and does not call hard-wrapped.
func TestHardWrappedParagraphDetection(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "one long line per paragraph is fine however long it runs",
			text: "A single unbroken line, well past the threshold, is exactly what this file wants everywhere.\n",
		},
		{
			name: "prose broken at a fixed column is caught",
			text: "The comments and whitespace attached to a member begin at the previous\n" +
				"member's comma, so a comment written after an apply lands in front of\n" +
				"the marker.\n",
			want: true,
		},
		{
			name: "a wrapped list item is caught, since its continuation is prose",
			text: "- `remove` no longer deletes a comment the operator wrote next to their own\n" +
				"  rule. The comments attached to a member begin at the previous member's\n" +
				"  comma, so it sits in front of the marker.\n",
			want: true,
		},
		{
			name: "single-line list items are fine",
			text: "- `remove` no longer deletes a comment the operator wrote next to their own rule.\n" +
				"- The self-check refuses a removal that loses one, which it could not see before.\n",
		},
		{
			name: "a stack of link references is not a paragraph",
			text: "[Unreleased]: https://github.com/nopoz/scurgery/compare/v0.2.0...HEAD\n" +
				"[0.2.0]: https://github.com/nopoz/scurgery/compare/v0.1.0...v0.2.0\n" +
				"[0.1.0]: https://github.com/nopoz/scurgery/releases/tag/v0.1.0\n",
		},
		{
			name: "wrapping inside a fenced block is the author's business",
			text: "```\nthis line is long enough to look machine wrapped to the detector\nand so is this one, but it is code\n```\n",
		},
		{
			name: "short lines are a deliberate stanza, not wrapping",
			text: "Roses are red\nViolets are blue\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hardWrappedParagraph(c.text) != ""
			if got != c.want {
				t.Errorf("hardWrappedParagraph reported %v, want %v", got, c.want)
			}
		})
	}
}
