package cli

import (
	"context"
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
	_ = runRemove(context.Background(), env, p, "", true)
	if !strings.Contains(out.String(), "not found") {
		t.Errorf("an edited member should be reported, not silently skipped, got %q", out.String())
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
	if !strings.Contains(out.String(), "tagOwners") || !strings.Contains(out.String(), "empty") {
		t.Errorf("leaving an empty container behind must be flagged, not reported as a plain success, got %q", out.String())
	}
}
