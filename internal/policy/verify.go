package policy

import (
	"bytes"
	"fmt"
	"strings"

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
	return verifyCommentsOnRemovedMembersSurvive(bo, after, ns)
}

// verifyCommentsOnRemovedMembersSurvive checks the half the comparisons above
// cannot see. A member's leading extra runs from the previous member's comma,
// so it can hold an operator's comment as well as scurgery's marker, and a
// removal that takes the whole extra deletes both. The members it belonged to
// are gone by definition, so no member-by-member comparison reaches it.
//
// It reads the comments out of before and asks only whether the text is still
// somewhere in after. That is deliberately not a claim about placement: it
// never consults Remove, so Remove agreeing with itself cannot satisfy it.
func verifyCommentsOnRemovedMembersSurvive(bo *hujson.Object, after []byte, ns string) error {
	check := func(where string, extra hujson.Extra) error {
		if !hasMarker(extra, ns) {
			return nil // this member survives, and so does its extra
		}
		for _, c := range commentsIn(extra) {
			if containsMarker(hujson.Extra(c), ns, "") {
				continue // scurgery's own marker, which removal is meant to take
			}
			if !bytes.Contains(after, []byte(c)) {
				return fmt.Errorf("self-check failed: removal deleted the comment %q, which sat next to %s but was not scurgery's to remove. Nothing was written", c, where)
			}
		}
		return nil
	}

	for _, m := range bo.Members {
		name := memberName(m)
		if err := check(fmt.Sprintf("top-level key %q", name), m.Name.BeforeExtra); err != nil {
			return err
		}
		switch t := m.Value.Value.(type) {
		case *hujson.Object:
			for _, im := range t.Members {
				if err := check(fmt.Sprintf("%s.%q", name, memberName(im)), im.Name.BeforeExtra); err != nil {
					return err
				}
			}
		case *hujson.Array:
			for _, el := range t.Elements {
				if err := check(fmt.Sprintf("an element of %q", name), el.BeforeExtra); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// commentsIn returns the comment tokens in an extra, in order. An Extra holds
// nothing but whitespace and comments, so this needs no JSON lexer.
func commentsIn(extra hujson.Extra) []string {
	var out []string
	s := string(extra)
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "//"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return append(out, s[i:])
			}
			out = append(out, strings.TrimRight(s[i:i+j], "\r"))
			i += j + 1
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return append(out, s[i:])
			}
			out = append(out, s[i:i+2+j+2])
			i += 2 + j + 2
		default:
			i++
		}
	}
	return out
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

// VerifyRemoveStructural checks the claim about a structural removal,
// independently of RemoveStructural: it never calls RemoveStructural, and it
// never consults markers, so it cannot simply agree with whatever that
// function decided. It reads only before, after, and the bundle: everything
// in before that is not semantically equal to a bundle entry at the same
// location must still be present, unchanged, in after.
//
// This is strictly weaker than VerifyRemove, because it has no markers to
// tell an operator's own value apart from scurgery's; it can only compare
// against what the bundle says scurgery would remove. It therefore cannot
// catch over-removal of content the bundle itself also describes, such as
// two identical array elements where the bundle names one of them and both
// happen to match: nothing here distinguishes which one was meant to
// survive. What it does catch is the dangerous half: a member the operator
// has since edited no longer equals the bundle, and so must not disappear.
func VerifyRemoveStructural(before, after, bundle []byte) error {
	beforeRoot, err := hujson.Parse(before)
	if err != nil {
		return fmt.Errorf("self-check could not parse the original: %w", err)
	}
	afterRoot, err := hujson.Parse(after)
	if err != nil {
		return fmt.Errorf("self-check could not parse the result: %w", err)
	}
	bundleRoot, err := hujson.Parse(bundle)
	if err != nil {
		return fmt.Errorf("self-check could not parse the bundle: %w", err)
	}
	bo, ok1 := beforeRoot.Value.(*hujson.Object)
	ao, ok2 := afterRoot.Value.(*hujson.Object)
	bundleObj, ok3 := bundleRoot.Value.(*hujson.Object)
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("self-check failed: policy or bundle is not a JSON object")
	}

	for _, m := range bo.Members {
		name := memberName(m)
		target := findMember(ao, name)
		bundleMember := findMember(bundleObj, name)
		if bundleMember == nil {
			// The bundle never mentions this key, so RemoveStructural never
			// touches it either: it must survive untouched, comments
			// included.
			if target == nil {
				return fmt.Errorf("self-check failed: removal dropped top-level key %q, which the bundle does not describe. Nothing was written", name)
			}
			if !bytes.Equal(m.Name.BeforeExtra, target.Name.BeforeExtra) || !semanticEqual(m.Value, target.Value) {
				return fmt.Errorf("self-check failed: removal changed top-level key %q, which the bundle does not describe. Nothing was written", name)
			}
			continue
		}
		if target == nil {
			return fmt.Errorf("self-check failed: removal dropped top-level key %q. Nothing was written", name)
		}
		if err := verifyStructuralContentSurvives(m.Value, target.Value, bundleMember.Value); err != nil {
			return fmt.Errorf("self-check failed: top-level key %q: %v. Nothing was written", name, err)
		}
	}
	return nil
}

// verifyStructuralContentSurvives checks that everything in before that does
// not semantically match a bundle entry at this location is still present,
// by value, in after. It does not call RemoveStructural.
func verifyStructuralContentSurvives(before, after, bundleValue hujson.Value) error {
	switch bt := before.Value.(type) {
	case *hujson.Object:
		at, ok := after.Value.(*hujson.Object)
		if !ok {
			return fmt.Errorf("was an object, is no longer one")
		}
		bundleObj, _ := bundleValue.Value.(*hujson.Object)
		for _, bm := range bt.Members {
			name := memberName(bm)
			if bundleObj != nil {
				if want := findMember(bundleObj, name); want != nil && semanticEqual(bm.Value, want.Value) {
					continue // matches a bundle entry: allowed to be gone
				}
			}
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
		bundleArr, _ := bundleValue.Value.(*hujson.Array)
		for _, be := range bt.Elements {
			exempt := false
			if bundleArr != nil {
				for _, want := range bundleArr.Elements {
					if semanticEqual(be, want) {
						exempt = true
						break
					}
				}
			}
			if exempt {
				continue // matches a bundle entry: allowed to be gone
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
