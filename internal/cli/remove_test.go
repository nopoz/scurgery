package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const installedPolicy = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		// scurgery:aws-router
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
	},
}`

const strippedPolicy = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
	},
}`

const removeBundle = `{
	"tagOwners": {
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
	},
}`

func TestRemoveByNameUsesMarkers(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := runRemove(context.Background(), env, "aws-router", "", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if strings.Contains(string(f.policy), "tag:aws-app") {
		t.Error("the marked member should be gone")
	}
	if !strings.Contains(string(f.policy), "tag:mine") {
		t.Error("the operator's member must survive")
	}
}

// TestRemoveMatchStructuralRejectedForBareName reproduces the recovery-path
// trap: an operator whose markers are gone runs `remove <name> --match-structural`
// expecting the structural fallback, but a bare namespace has no bundle to
// compare against, so structural matching can never run for it. Before the
// fix this silently reported "no change needed" with a zero exit, which
// reads as "nothing is installed" when it may not be true at all.
func TestRemoveMatchStructuralRejectedForBareName(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "aws-router", "", true)
	if err == nil {
		t.Fatal("--match-structural with a bare namespace should be rejected, not silently ignored")
	}
	if f.writes != 0 {
		t.Error("a rejected call must not write")
	}
	if !strings.Contains(err.Error(), "remove <bundle.hujson>") {
		t.Errorf("the rejection should point at the bundle-path form, got %q", err.Error())
	}
}

// TestRemoveNameFlagRejectedForBareName closes the related trap where
// `remove <name> --name <other>` silently discarded the positional and
// removed <other> instead.
func TestRemoveNameFlagRejectedForBareName(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "aws-router", "some-other-name", false)
	if err == nil {
		t.Fatal("--name with a bare namespace should be rejected, not silently swap the target")
	}
	if f.writes != 0 {
		t.Error("a rejected call must not write")
	}
}

func TestRemoveRefusesStructuralWithoutTheFlag(t *testing.T) {
	f := &fakeTailnet{policy: []byte(strippedPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	err := runRemove(context.Background(), env, p, "", false)
	if err == nil {
		t.Fatal("with no markers present, remove should refuse rather than guess")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when remove refuses")
	}
	combined := err.Error() + out.String()
	if !strings.Contains(combined, "--match-structural") {
		t.Errorf("the refusal should name the flag that would proceed, got %q", combined)
	}
	if !strings.Contains(combined, "would remove 1 entry") {
		t.Errorf("the refusal should name the count an operator needs to decide whether to re-run, got %q", combined)
	}
}

func TestRemoveStructuralWithFlagProceeds(t *testing.T) {
	f := &fakeTailnet{policy: []byte(strippedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if strings.Contains(string(f.policy), "tag:aws-app") {
		t.Error("structural removal should have removed the member")
	}
	if !strings.Contains(string(f.policy), "tag:mine") {
		t.Error("the operator's member must survive")
	}
}

func TestRemoveStructuralReportsUnmatched(t *testing.T) {
	edited := strings.ReplaceAll(strippedPolicy, `["autogroup:admin", "tag:aws-app"]`, `["group:eng"]`)
	f := &fakeTailnet{policy: []byte(edited), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("an edited member should be reported, not silently skipped, got %q", out.String())
	}
	if f.writes != 0 {
		t.Error("nothing may be written when nothing structurally matched")
	}
	if !strings.Contains(string(f.policy), "group:eng") {
		t.Error("the operator-edited member must survive with its edited value, not be silently dropped")
	}
}

// mixedStrippedPolicy and mixedRemoveBundle exercise a structural removal
// where one bundle entry matches exactly and a second was edited by the
// operator since scurgery installed it. Unlike TestRemoveStructuralReportsUnmatched,
// where every entry mismatches and the whole operation short-circuits before
// any write, this forces the write path: the matching entry is removed, the
// edited one is reported and left alone, and the self-check that runs in
// place of the marker check must accept exactly that partial result.
const mixedStrippedPolicy = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
		"tag:edited": ["group:eng"],
	},
}`

const mixedRemoveBundle = `{
	"tagOwners": {
		"tag:aws-app": ["autogroup:admin", "tag:aws-app"],
		"tag:edited": ["autogroup:admin"],
	},
}`

func TestRemoveStructuralMixedMatchWrites(t *testing.T) {
	f := &fakeTailnet{policy: []byte(mixedStrippedPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", mixedRemoveBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if f.writes != 1 {
		t.Errorf("a partial structural match should still write, writes = %d", f.writes)
	}
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("the edited member should be reported, got %q", out.String())
	}
	if strings.Contains(string(f.policy), "tag:aws-app") {
		t.Error("the exactly matching member should have been removed")
	}
	if !strings.Contains(string(f.policy), "tag:mine") {
		t.Error("the operator's member must survive")
	}
	if !strings.Contains(string(f.policy), "tag:edited") || !strings.Contains(string(f.policy), "group:eng") {
		t.Error("the edited member must survive with its edited value, not the bundle's stale value")
	}
}

func TestRemoveMarkerSelfCheckRuns(t *testing.T) {
	calls := 0
	orig := removeVerify
	removeVerify = func(before, after []byte, ns string) error {
		calls++
		return orig(before, after, ns)
	}
	defer func() { removeVerify = orig }()

	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := runRemove(context.Background(), env, "aws-router", "", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if calls == 0 {
		t.Error("the marker-based self-check must run on the marker path")
	}
}

func TestRemoveStructuralSkipsMarkerSelfCheck(t *testing.T) {
	calls := 0
	orig := removeVerify
	removeVerify = func(before, after []byte, ns string) error {
		calls++
		return orig(before, after, ns)
	}
	defer func() { removeVerify = orig }()

	f := &fakeTailnet{policy: []byte(strippedPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if calls != 0 {
		t.Errorf("the marker-based self-check must not run on the structural path, got %d calls", calls)
	}
	if !strings.Contains(out.String(), "a reduced content check ran in its place") {
		t.Errorf("structural removal should disclose that a reduced check ran in place of the marker check, got %q", out.String())
	}
}

func TestRemoveUnknownNameSaysSo(t *testing.T) {
	f := &fakeTailnet{policy: []byte(installedPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	if err := runRemove(context.Background(), env, "not-installed", "", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if f.writes != 0 {
		t.Error("removing an absent namespace must not write")
	}
	if !strings.Contains(out.String(), "no change") {
		t.Errorf("should report that nothing matched, got %q", out.String())
	}
}

// sharedContainerPolicy reproduces a container scurgery created for ns-a
// (marked with an owns-key marker on the top-level "tagOwners" key) that a
// second namespace, ns-b, later added a member into. Removing ns-a must keep
// the container, since ns-b's rule still lives inside it, but that means
// nothing carries an ns-a marker any more: the key marker is on a container
// Remove won't drop, and ns-a never marked anything at member level because
// it created the container wholesale.
const sharedContainerPolicy = `{
	// scurgery:ns-a owns-key
	"tagOwners": {
		"tag:a": ["autogroup:admin"],
		// scurgery:ns-b
		"tag:b": ["autogroup:admin"],
	},
}`

func TestRemoveSharedContainerExplainsNoOp(t *testing.T) {
	f := &fakeTailnet{policy: []byte(sharedContainerPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "ns-a", "", false)
	if err == nil {
		t.Fatal("removing a namespace stuck behind a container another namespace shares must not report success")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when the shared container blocks removal")
	}
	msg := err.Error()
	if strings.Contains(msg, "still present after removal") {
		t.Errorf("the raw self-check message should never surface here, got %q", msg)
	}
	if !strings.Contains(msg, "ns-a") {
		t.Errorf("the explanation should name the namespace that was not removed, got %q", msg)
	}
	if !strings.Contains(msg, "ns-b") {
		t.Errorf("the explanation should name the namespace sharing the container, got %q", msg)
	}
	if !strings.Contains(string(f.policy), "tag:b") {
		t.Error("ns-b's rule must survive untouched")
	}
}

// sharedContainerWithExtraPolicy is sharedContainerPolicy plus a second, ns-a
// rule that lives outside the shared container entirely (an element ns-a
// added to an existing "acls" array the operator owns). It reproduces the
// case where policy.Remove has something real to remove (the acls element),
// so Removed > 0, while the shared container still leaves ns-a present.
const sharedContainerWithExtraPolicy = `{
	"acls": [
		"operator-rule",
		// scurgery:ns-a
		"ns-a-rule",
	],
	// scurgery:ns-a owns-key
	"tagOwners": {
		"tag:a": ["autogroup:admin"],
		// scurgery:ns-b
		"tag:b": ["autogroup:admin"],
	},
}`

func TestRemoveSharedContainerBlocksEvenWhenSomethingElseWasRemoved(t *testing.T) {
	f := &fakeTailnet{policy: []byte(sharedContainerWithExtraPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "ns-a", "", false)
	if err == nil {
		t.Fatal("ns-a still has a live rule behind the shared container; removing an unrelated acls element must not report success")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when the shared container blocks removal")
	}
	msg := err.Error()
	if strings.Contains(msg, "still present after removal") {
		t.Errorf("the raw self-check message should never surface here, even when something else was removable, got %q", msg)
	}
	if !strings.Contains(msg, "ns-b") {
		t.Errorf("the explanation should name the namespace sharing the container, got %q", msg)
	}
	if !strings.Contains(string(f.policy), "ns-a-rule") {
		t.Error("nothing was written, so ns-a's acls rule must still be there")
	}
}

// sharedContainerWithUnrelatedPolicy adds a namespace, ns-c, that is
// installed under a completely separate top-level key and shares nothing
// with ns-a's container. The blocker explanation must not name it.
const sharedContainerWithUnrelatedPolicy = `{
	"grants": [
		// scurgery:ns-c
		"ns-c-rule",
	],
	// scurgery:ns-a owns-key
	"tagOwners": {
		"tag:a": ["autogroup:admin"],
		// scurgery:ns-b
		"tag:b": ["autogroup:admin"],
	},
}`

func TestRemoveSharedContainerBlockersExcludeUnrelatedNamespace(t *testing.T) {
	f := &fakeTailnet{policy: []byte(sharedContainerWithUnrelatedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "ns-a", "", false)
	if err == nil {
		t.Fatal("expected the shared-container refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ns-b") {
		t.Errorf("ns-b actually shares the container and should be named, got %q", msg)
	}
	if strings.Contains(msg, "ns-c") {
		t.Errorf("ns-c shares nothing with ns-a's container; naming it would send the operator to delete an unrelated namespace's rules, got %q", msg)
	}
	if f.writes != 0 {
		t.Error("nothing may be written when the shared container blocks removal")
	}
}

// TestRemoveSharedContainerRoundTrip pins the promise the blocker message
// makes: that removing the other namespace first and re-running finishes the
// job, dropping the now-unshared container and leaving the operator's own
// content untouched.
func TestRemoveSharedContainerRoundTrip(t *testing.T) {
	f := &fakeTailnet{policy: []byte(sharedContainerWithExtraPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	if err := runRemove(context.Background(), env, "ns-a", "", false); err == nil {
		t.Fatal("ns-a should still be blocked by the shared container on the first attempt")
	}
	if f.writes != 0 {
		t.Fatal("the blocked attempt must not have written anything")
	}

	if err := runRemove(context.Background(), env, "ns-b", "", false); err != nil {
		t.Fatalf("removing ns-b: %v", err)
	}

	if err := runRemove(context.Background(), env, "ns-a", "", false); err != nil {
		t.Fatalf("removing ns-a after ns-b should now succeed: %v", err)
	}

	final := string(f.policy)
	if strings.Contains(final, "tagOwners") {
		t.Errorf("the container ns-a created is now unshared and should have been dropped entirely, got %q", final)
	}
	if strings.Contains(final, "ns-a-rule") {
		t.Error("ns-a's acls rule should be gone")
	}
	if !strings.Contains(final, "operator-rule") {
		t.Error("the operator's unrelated acls rule must survive")
	}
}

// residuePolicy has a container whose only member is the one the bundle
// contributes. Structural matching has no marker to tell it that scurgery
// created the container, so removing that member leaves an empty shell
// behind rather than dropping the container too.
const residuePolicy = `{
	"tagOwners": {
		"tag:solo": ["autogroup:admin"],
	},
}`

const residueBundle = `{
	"tagOwners": {
		"tag:solo": ["autogroup:admin"],
	},
}`

func TestRemoveStructuralWarnsOnEmptiedContainer(t *testing.T) {
	f := &fakeTailnet{policy: []byte(residuePolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "solo.hujson", residueBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if strings.Contains(string(f.policy), "tag:solo") {
		t.Error("the matched member should be gone")
	}
	if !strings.Contains(out.String(), `"tagOwners" is now empty`) {
		t.Errorf("leaving an empty container behind must be flagged by the warning itself, not merely by the rendered diff containing the key name, got %q", out.String())
	}
}

// TestRemoveStructuralSelfCheckRuns proves policy.VerifyRemoveStructural is
// actually invoked on the structural path, not merely present in the source:
// a check that is never called would leave the whole suite green.
func TestRemoveStructuralSelfCheckRuns(t *testing.T) {
	calls := 0
	orig := removeStructuralVerify
	removeStructuralVerify = func(before, after, bundle []byte) error {
		calls++
		return orig(before, after, bundle)
	}
	defer func() { removeStructuralVerify = orig }()

	f := &fakeTailnet{policy: []byte(strippedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if calls == 0 {
		t.Error("the structural self-check must run on the structural path")
	}
}

// TestRemoveStructuralSelfCheckBlocksTheWrite injects a deliberate failure
// into the structural self-check and confirms it actually blocks the write,
// rather than being reachable but ignored. This is the test that would catch
// runRemove's verify closure reverting to an unconditional "return nil" on
// the structural branch: counting calls alone would not, since a call that
// is made but whose result is discarded still increments a counter.
func TestRemoveStructuralSelfCheckBlocksTheWrite(t *testing.T) {
	orig := removeStructuralVerify
	removeStructuralVerify = func(before, after, bundle []byte) error {
		return errors.New("injected structural self-check failure")
	}
	defer func() { removeStructuralVerify = orig }()

	f := &fakeTailnet{policy: []byte(strippedPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "aws-router.hujson", removeBundle)
	err := runRemove(context.Background(), env, p, "", true)
	if err == nil {
		t.Fatal("a failing structural self-check must block the write")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when the structural self-check fails")
	}
}

// unreachableMarkerPolicy carries a "ghost" marker two levels deep: on an
// element of an array that is itself a member of a top-level object. Remove
// only scans a top-level key and the immediate members/elements of a
// top-level container, since that is as deep as Apply ever marks anything,
// so it never reaches this marker or clears it. Namespaces walks the whole
// tree, though, and does find it, so after Remove the namespace still
// "looks present" with nothing RemoveBlockers can identify as holding it.
// This is only reachable by hand-editing the policy; Apply never produces a
// marker this deep.
const unreachableMarkerPolicy = `{
	"grants": {
		"src": [
			"a",
			// scurgery:ghost
			"b",
		],
	},
}`

func TestRemoveUnidentifiablePresenceGetsAnHonestMessage(t *testing.T) {
	f := &fakeTailnet{policy: []byte(unreachableMarkerPolicy), etag: `"e1"`, validateOK: true}
	env, _, done := testEnv(t, f, "")
	defer done()

	err := runRemove(context.Background(), env, "ghost", "", false)
	if err == nil {
		t.Fatal("a namespace that still appears after removal must not report success")
	}
	if f.writes != 0 {
		t.Error("nothing may be written when scurgery cannot identify what is holding the namespace")
	}
	msg := err.Error()
	if strings.Contains(msg, "shares a top-level container") {
		t.Errorf("no shared container was actually found here; asserting one would be untrue, got %q", msg)
	}
	if strings.Contains(msg, "still present after removal") {
		t.Errorf("the raw self-check message should never surface here, got %q", msg)
	}
}

// A bundle that is not installed at all: no markers name it, and nothing it
// declares matches the policy by content either. Something else is installed,
// so there is a real answer to give about what the policy does hold.
const uninstalledPolicy = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		// scurgery:other-bundle
		"tag:other": ["group:eng"],
	},
}`

const uninstalledBundle = `{
	"tagOwners": {
		"tag:never-applied": ["group:eng"],
	},
}`

// Removing something that was never installed is a no-op, and naming the
// bundle file has to reach the same conclusion as naming the bundle: the same
// exit code, and the same account of what is installed instead. A teardown
// script that passes the path it applied is otherwise not rerunnable.
func TestRemoveOfANeverInstalledBundleAgreesWithTheNameForm(t *testing.T) {
	byName := &fakeTailnet{policy: []byte(uninstalledPolicy), etag: `"e1"`, validateOK: true}
	nameCode, nameOut, nameErr := runEnd2End(t, byName, []string{"remove", "never-applied", "--yes", "--backup-dir", t.TempDir()}, "")
	if nameCode != 0 {
		t.Fatalf("remove by name = %d, want 0, stderr=%q", nameCode, nameErr.String())
	}

	byFile := &fakeTailnet{policy: []byte(uninstalledPolicy), etag: `"e1"`, validateOK: true}
	path := writeBundleFile(t, "never-applied.hujson", uninstalledBundle)
	fileCode, fileOut, fileErr := runEnd2End(t, byFile, []string{"remove", path, "--yes", "--backup-dir", t.TempDir()}, "")
	if fileCode != nameCode {
		t.Errorf("remove by bundle file = %d but remove by name = %d; the same request must reach the same exit code, stderr=%q",
			fileCode, nameCode, fileErr.String())
	}
	if byName.writes != 0 || byFile.writes != 0 {
		t.Errorf("removing something never installed must not write, got %d and %d", byName.writes, byFile.writes)
	}
	for _, got := range []string{nameOut.String(), fileOut.String()} {
		if !strings.Contains(got, "installed namespaces: other-bundle") {
			t.Errorf("a removal that matched nothing should name what is installed instead, got %q", got)
		}
	}
}

// The refusal that names --match-structural exists to offer a recovery when
// the markers were stripped from a bundle that is still there. With nothing
// of the bundle in the policy there is nothing to recover, so pointing at the
// flag sends the operator after a run that would remove nothing.
func TestRemoveOfANeverInstalledBundleDoesNotSuggestStructuralMatching(t *testing.T) {
	f := &fakeTailnet{policy: []byte(uninstalledPolicy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	path := writeBundleFile(t, "never-applied.hujson", uninstalledBundle)
	if err := runRemove(context.Background(), env, path, "", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if !strings.Contains(out.String(), "is not installed") {
		t.Errorf("the operator handed over a bundle file and should be told it is not installed, got %q", out.String())
	}
	if strings.Contains(out.String(), "--match-structural") {
		t.Errorf("nothing of this bundle is in the policy, so --match-structural would remove nothing; got %q", out.String())
	}
	if strings.Contains(out.String(), "may have been removed") {
		t.Errorf("the markers were never there to be removed; got %q", out.String())
	}
}

// Exit codes are a contract, so the situations remove can land in are pinned
// together rather than one at a time: what matters is not only each code but
// that the codes agree where the situations do. Naming the bundle file and
// naming the bundle are the same request and must answer the same way, and no
// row may return 1, which belongs to diff and means the policy would change.
func TestRemoveExitCodesAcrossEverySituation(t *testing.T) {
	const marked = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		// scurgery:aws-router
		"tag:aws-app": ["group:eng"],
	},
}`
	const stripped = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
		"tag:aws-app": ["group:eng"],
	},
}`
	const clean = `{
	"tagOwners": {
		"tag:mine": ["autogroup:admin"],
	},
}`
	const bundle = `{
	"tagOwners": {
		"tag:aws-app": ["group:eng"],
	},
}`
	const shared = `{
	// scurgery:ns-a owns-key
	"tagOwners": {
		"tag:a": ["autogroup:admin"],
		// scurgery:ns-b
		"tag:b": ["autogroup:admin"],
	},
}`

	cases := []struct {
		name       string
		policy     string
		args       []string
		wantCode   int
		wantWrites int
	}{
		{"installed, by name", marked, []string{"remove", "aws-router"}, 0, 1},
		{"installed, by file", marked, []string{"remove", "BUNDLE"}, 0, 1},
		{"not installed, by name", clean, []string{"remove", "aws-router"}, 0, 0},
		{"not installed, by file", clean, []string{"remove", "BUNDLE"}, 0, 0},
		{"markers stripped, by file, no flag", stripped, []string{"remove", "BUNDLE"}, 3, 0},
		{"markers stripped, by file, structural", stripped, []string{"remove", "BUNDLE", "--match-structural"}, 0, 1},
		{"structural asked for by name", stripped, []string{"remove", "aws-router", "--match-structural"}, 3, 0},
		{"name flag with a bare name", stripped, []string{"remove", "aws-router", "--name", "x"}, 3, 0},
		{"shared container", shared, []string{"remove", "ns-a"}, 3, 0},
		{"missing bundle file", clean, []string{"remove", "./nope.hujson"}, 3, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeBundleFile(t, "aws-router.hujson", bundle)
			args := append([]string{}, c.args...)
			for i := range args {
				if args[i] == "BUNDLE" {
					args[i] = path
				}
			}
			args = append(args, "--yes", "--backup-dir", t.TempDir())
			f := &fakeTailnet{policy: []byte(c.policy), etag: `"e1"`, validateOK: true}
			code, out, errb := runEnd2End(t, f, args, "")
			if code != c.wantCode {
				t.Errorf("exit = %d, want %d\nstdout: %s\nstderr: %s", code, c.wantCode, out.String(), errb.String())
			}
			if f.writes != c.wantWrites {
				t.Errorf("writes = %d, want %d", f.writes, c.wantWrites)
			}
			if code == 1 {
				t.Error("exit 1 is diff's drift code and must never come from remove")
			}
			if strings.Contains(errb.String(), "panic") {
				t.Error("panicked")
			}
		})
	}
}

// The warning ends by inviting the operator to delete the container by hand,
// so it has to be raised for exactly the containers the removal emptied. This
// policy holds one of each case: tagOwners is emptied by the removal,
// autoApprovers was already empty and so is none of scurgery's doing, and acls
// keeps a rule the operator wrote.
func TestRemoveStructuralWarnsOnlyAboutContainersItEmptied(t *testing.T) {
	const policy = `{
	"tagOwners": {
		"tag:solo": ["group:eng"],
	},
	"acls": [
		{"action": "accept", "src": ["tag:solo"], "dst": ["*:*"]},
		{"action": "accept", "src": ["*"], "dst": ["*:*"]},
	],
	"autoApprovers": {},
}`
	const bundle = `{
	"tagOwners": {
		"tag:solo": ["group:eng"],
	},
	"acls": [
		{"action": "accept", "src": ["tag:solo"], "dst": ["*:*"]},
	],
	"autoApprovers": {
		"routes": {"10.0.0.0/8": ["tag:solo"]},
	},
}`
	f := &fakeTailnet{policy: []byte(policy), etag: `"e1"`, validateOK: true}
	env, out, done := testEnv(t, f, "")
	defer done()

	p := writeBundleFile(t, "solo.hujson", bundle)
	if err := runRemove(context.Background(), env, p, "", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if !strings.Contains(out.String(), `"tagOwners" is now empty`) {
		t.Errorf("the removal emptied tagOwners and must say so, got %q", out.String())
	}
	if strings.Contains(out.String(), `"autoApprovers" is now empty`) {
		t.Errorf("autoApprovers was already empty before the removal, so scurgery did not leave that shell behind, got %q", out.String())
	}
	if strings.Contains(out.String(), `"acls" is now empty`) {
		t.Errorf("acls still holds a rule the operator wrote and is not empty at all, got %q", out.String())
	}
}
