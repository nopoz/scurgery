package policy

import (
	"bytes"
	"fmt"

	"github.com/tailscale/hujson"
)

// VerifyApply checks scurgery's own claim about an apply: that the only
// difference between before and after is the namespace's own contributions.
// Removing the namespace from each side must leave the same non-namespace
// content behind. Comparing against before directly would be wrong when ns
// was already installed in before: updating an already-installed namespace
// is legitimate, and Remove strips that namespace's markers from before too.
//
// This runs against the live policy before any write, so a parsing or merging
// defect is caught locally rather than uploaded.
func VerifyApply(before, after []byte, ns string) error {
	strippedAfter, err := Remove(after, ns)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the merged policy: %w", err)
	}
	strippedBefore, err := Remove(before, ns)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the original policy: %w", err)
	}
	if !bytes.Equal(strippedAfter.Policy, strippedBefore.Policy) {
		return fmt.Errorf("self-check failed: removing %q from the merged policy left something different from "+
			"removing it from the original, so the merge changed something it does not own. Nothing was written", ns)
	}
	return nil
}

// VerifyRemove checks the claim about a removal: the namespace, and only the
// namespace, is gone. Every other namespace's contributions must survive,
// and every top-level key and value the operator owned must survive
// unchanged, including its own leading comment.
//
// This is checked independently of Remove: comparing after against a fresh
// Remove(before, ns) would only prove Remove agrees with itself, which
// catches nothing if Remove itself is the thing that is wrong.
func VerifyRemove(before, after []byte, ns string) error {
	beforeNames, err := Namespaces(before)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the original: %w", err)
	}
	afterNames, err := Namespaces(after)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the result: %w", err)
	}
	wantNames := make([]string, 0, len(beforeNames))
	for _, n := range beforeNames {
		if n != ns {
			wantNames = append(wantNames, n)
		}
	}
	if !equalStrings(afterNames, wantNames) {
		for _, n := range afterNames {
			if n == ns {
				return fmt.Errorf("self-check failed: %q is still present after removal. Nothing was written", ns)
			}
		}
		return fmt.Errorf("self-check failed: removal changed which namespaces are present (want %v, got %v). "+
			"Nothing was written", wantNames, afterNames)
	}

	beforeRoot, err := hujson.Parse(before)
	if err != nil {
		return fmt.Errorf("self-check could not parse the original: %w", err)
	}
	afterRoot, err := hujson.Parse(after)
	if err != nil {
		return fmt.Errorf("self-check could not parse the result: %w", err)
	}
	bo, ok1 := beforeRoot.Value.(*hujson.Object)
	ao, ok2 := afterRoot.Value.(*hujson.Object)
	if !ok1 || !ok2 {
		return fmt.Errorf("self-check failed: policy is not a JSON object")
	}

	for _, m := range bo.Members {
		name := memberName(m)
		target := findMember(ao, name)
		if target == nil {
			if hasKeyMarker(m.Name.BeforeExtra, ns) {
				continue // scurgery created this key, so it is meant to disappear
			}
			return fmt.Errorf("self-check failed: removal dropped top-level key %q, which scurgery did not create. Nothing was written", name)
		}
		// A retained top-level member's own leading comment is never touched
		// by Remove, shared container or not, so it must be byte-identical.
		// This is the only place a deleted operator comment can be caught,
		// since the content comparison below is by value and does not see
		// comments at all.
		if !bytes.Equal(m.Name.BeforeExtra, target.Name.BeforeExtra) {
			return fmt.Errorf("self-check failed: removal altered the comment or formatting attached to top-level key %q. Nothing was written", name)
		}
		if err := verifyContentSurvives(m.Value, target.Value, ns); err != nil {
			return fmt.Errorf("self-check failed: top-level key %q: %v. Nothing was written", name, err)
		}
	}
	return nil
}

// verifyContentSurvives checks that everything in before not marked for ns
// is still present, by value, in after. It does not call Remove.
func verifyContentSurvives(before, after hujson.Value, ns string) error {
	switch bt := before.Value.(type) {
	case *hujson.Object:
		at, ok := after.Value.(*hujson.Object)
		if !ok {
			return fmt.Errorf("was an object, is no longer one")
		}
		for _, bm := range bt.Members {
			if hasMarker(bm.Name.BeforeExtra, ns) {
				continue // scurgery's own contribution, meant to be gone
			}
			name := memberName(bm)
			am := findMember(at, name)
			if am == nil {
				return fmt.Errorf("member %q was removed", name)
			}
			if !semanticEqual(bm.Value, am.Value) {
				return fmt.Errorf("member %q's value changed", name)
			}
		}
	case *hujson.Array:
		at, ok := after.Value.(*hujson.Array)
		if !ok {
			return fmt.Errorf("was an array, is no longer one")
		}
		for _, be := range bt.Elements {
			if hasMarker(be.BeforeExtra, ns) {
				continue // scurgery's own contribution, meant to be gone
			}
			found := false
			for _, ae := range at.Elements {
				if semanticEqual(be, ae) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("an element was removed")
			}
		}
	default:
		if !semanticEqual(before, after) {
			return fmt.Errorf("value changed")
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
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
