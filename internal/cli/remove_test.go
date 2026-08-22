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
