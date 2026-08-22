package policy

import (
	"bytes"
	"fmt"

	"github.com/tailscale/hujson"
)

// VerifyApply checks scurgery's own claim about an apply: that the only
// difference between before and after is the namespace's own contributions.
// Removing them again must reproduce the original bytes exactly.
//
// This runs against the live policy before any write, so a parsing or merging
// defect is caught locally rather than uploaded.
func VerifyApply(before, after []byte, ns string) error {
	res, err := Remove(after, ns)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the merged policy: %w", err)
	}
	if !bytes.Equal(res.Policy, before) {
		return fmt.Errorf("self-check failed: removing %q from the merged policy did not reproduce the original, "+
			"so the merge changed something it does not own. Nothing was written", ns)
	}
	return nil
}

// VerifyRemove checks the claim about a removal: the namespace is gone, and
// every top-level key that scurgery did not create is still present.
//
// There is no exact inverse here the way there is for apply, so this asserts
// the two properties that matter rather than byte equality.
func VerifyRemove(before, after []byte, ns string) error {
	names, err := Namespaces(after)
	if err != nil {
		return fmt.Errorf("self-check could not re-parse the result: %w", err)
	}
	for _, n := range names {
		if n == ns {
			return fmt.Errorf("self-check failed: %q is still present after removal. Nothing was written", ns)
		}
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
		if hasKeyMarker(m.Name.BeforeExtra, ns) {
			continue // scurgery created this key, so it is meant to disappear
		}
		if findMember(ao, memberName(m)) == nil {
			return fmt.Errorf("self-check failed: removal dropped top-level key %q, which scurgery did not create. Nothing was written", memberName(m))
		}
	}
	return nil
}
