package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
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

// slugify reproduces the anchor GitHub derives from a heading: lowercased,
// with everything but letters, digits, spaces and hyphens dropped, and spaces
// turned into hyphens.
func slugify(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// A table of contents rots silently. A link to a heading that has been renamed
// still renders as a link and simply does nothing when clicked, and a section
// added without an entry is invisible rather than broken. Nobody reviewing a
// diff sees either one.
func TestReadmeTableOfContentsMatchesHeadings(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(b)

	headings := []string{}
	for _, m := range regexp.MustCompile(`(?m)^#{2,3} (.+)$`).FindAllStringSubmatch(readme, -1) {
		if title := strings.TrimSpace(m[1]); title != "Contents" {
			headings = append(headings, title)
		}
	}
	if len(headings) == 0 {
		t.Fatal("no headings found; the parser or the README changed shape")
	}

	linked := map[string]string{}
	order := []string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*- \[([^\]]+)\]\(#([^)]+)\)$`).FindAllStringSubmatch(readme, -1) {
		linked[m[1]] = m[2]
		order = append(order, m[1])
	}

	for _, title := range headings {
		anchor, ok := linked[title]
		if !ok {
			t.Errorf("section %q has no entry in the table of contents", title)
			continue
		}
		if want := slugify(title); anchor != want {
			t.Errorf("table of contents links %q to #%s, but GitHub's anchor for it is #%s", title, anchor, want)
		}
	}

	inReadme := map[string]bool{}
	for _, title := range headings {
		inReadme[title] = true
	}
	for _, title := range order {
		if !inReadme[title] {
			t.Errorf("table of contents lists %q, which is not a heading in the README", title)
		}
	}
}

// TestReadmeDocumentsTheVersionCommand derives the documented command from
// what Run actually answers to, rather than restating the name, so removing
// or renaming the subcommand fails here instead of leaving the README
// pointing at a command that no longer exists.
func TestReadmeDocumentsTheVersionCommand(t *testing.T) {
	setVersion(t, "v1.2.3")

	var out, errb bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &errb, strings.NewReader("")); code != exitOK {
		t.Fatalf("running version: code = %d, want %d", code, exitOK)
	}
	// The output is "scurgery <version>", so the first field is the command's
	// own name and the invocation the README must document is that name plus
	// the subcommand.
	invocation := strings.Fields(out.String())[0] + " version"

	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	if !strings.Contains(string(b), invocation) {
		t.Errorf("README does not document %q", invocation)
	}
	if !strings.Contains(usage, invocation) {
		t.Errorf("usage text does not document %q", invocation)
	}
}

// TestReadmeReleaseArtifactNamesMatchTheWorkflow derives the archive and
// checksum names from the release workflow that builds them. An operator
// follows these names to download and verify a binary, and the workflow is
// the only thing that decides what they are.
func TestReadmeReleaseArtifactNamesMatchTheWorkflow(t *testing.T) {
	wf, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("reading release.yml: %v", err)
	}
	workflow := string(wf)

	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(b)

	// Rebuild an example archive name from the workflow's own template.
	nameTemplate := regexp.MustCompile(`name="([^"]+)"`).FindStringSubmatch(workflow)
	if nameTemplate == nil {
		t.Fatal("release.yml no longer assigns an archive name; the README example cannot be checked")
	}
	replacer := strings.NewReplacer(
		"${GITHUB_REF_NAME}", "v0.1.0",
		"${goos}", "linux",
		"${goarch}", "amd64",
	)
	example := replacer.Replace(nameTemplate[1]) + ".tar.gz"
	if strings.Contains(example, "${") {
		t.Fatalf("archive name template has an unrecognised variable: %q", nameTemplate[1])
	}
	if !strings.Contains(readme, example) {
		t.Errorf("README's download example does not match what the workflow builds (%q)", example)
	}

	// The checksum file the workflow writes is the one the README tells an
	// operator to verify against.
	checksums := regexp.MustCompile(`> (checksums\.\w+)`).FindStringSubmatch(workflow)
	if checksums == nil {
		t.Fatal("release.yml no longer writes a checksums file")
	}
	if !strings.Contains(readme, "sha256sum -c "+checksums[1]) {
		t.Errorf("README does not tell the operator to verify against %q", checksums[1])
	}
}
