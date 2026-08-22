package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/nopoz/scurgery/internal/bundle"
	"github.com/nopoz/scurgery/internal/policy"
)

// runRemove takes either a namespace name or a bundle path. Markers are always
// tried first. Structural matching needs the bundle to compare against, and is
// never used without the operator saying so.
func runRemove(ctx context.Context, env *Env, target, nameOverride string, matchStructural bool) error {
	var b *bundle.Bundle
	name := target

	if looksLikePath(target) {
		var err error
		b, err = bundle.Load(target, nameOverride)
		if err != nil {
			return err
		}
		name = b.Name
	} else if nameOverride != "" {
		name = nameOverride
	}

	// policy.VerifyRemove checks that everything Remove did not mark for ns
	// survives untouched. A structural removal, by definition, only ever
	// runs against a policy whose markers are already gone, so every member
	// it legitimately removes looks unmarked to VerifyRemove too: it would
	// reject the exact removal the operator asked for. The self-check is
	// therefore only meaningful for marker-based removal.
	var usedStructural bool
	verify := func(before, after []byte) error {
		if usedStructural {
			return nil
		}
		return policy.VerifyRemove(before, after, name)
	}

	return writePolicy(ctx, env, "remove "+name, func(current []byte) ([]byte, error) {
		res, err := policy.Remove(current, name)
		if err != nil {
			return nil, err
		}
		if res.Removed > 0 {
			fmt.Fprintf(env.Out, "removing %d marked entr%s\n", res.Removed, plural(res.Removed))
			return res.Policy, nil
		}

		// Nothing was removed by marker. This means either the namespace is
		// not installed at all, or every marker for it sits on a top-level
		// container that another namespace still shares, which Remove
		// correctly refuses to drop and so leaves nothing marked to find.
		// Those two cases must not be confused: the second one is a
		// namespace that is still very much live in the policy.
		installed, err := policy.Namespaces(current)
		if err != nil {
			return nil, err
		}
		if stillPresent(installed, name) {
			var others []string
			for _, n := range installed {
				if n != name {
					others = append(others, n)
				}
			}
			who := "the other namespace"
			if len(others) > 0 {
				who = strings.Join(others, ", ")
			}
			return nil, fmt.Errorf("%q was not removed: it shares a top-level container that scurgery created "+
				"with another namespace, so nothing marked for %q could be taken out without also destroying "+
				"that namespace's rules. Nothing was changed. Remove %s first, then re-run this command to "+
				"finish removing %q", name, name, who, name)
		}

		if b == nil {
			if len(installed) > 0 {
				fmt.Fprintf(env.Out, "installed namespaces: %s\n", strings.Join(installed, ", "))
			}
			return nil, nil
		}
		if !matchStructural {
			preview, err := policy.RemoveStructural(current, b.Data)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("no %q markers found in the policy. Comment markers may have been removed. "+
				"Matching on content instead would remove %d entr%s. "+
				"Nothing was changed: re-run with --match-structural to proceed",
				name, preview.Removed, plural(preview.Removed))
		}

		res, err = policy.RemoveStructural(current, b.Data)
		if err != nil {
			return nil, err
		}
		for _, u := range res.Unmatched {
			fmt.Fprintf(env.Out, "not found, leaving alone: %s\n", u)
		}
		if res.Removed == 0 {
			return nil, nil
		}
		usedStructural = true
		fmt.Fprintf(env.Out, "warning: scurgery's local self-check does not run for a structural removal, "+
			"since it relies on markers this policy no longer has; review the diff below before confirming\n")
		residue, err := emptiedContainers(res.Policy, b.Data)
		if err != nil {
			return nil, err
		}
		for _, key := range residue {
			fmt.Fprintf(env.Out, "warning: %q is now empty. Structural matching has no marker to tell whether "+
				"scurgery created this container, so the empty shell was left behind rather than removed; "+
				"delete it by hand if nothing else uses it\n", key)
		}
		fmt.Fprintf(env.Out, "removing %d entr%s by content match\n", res.Removed, plural(res.Removed))
		return res.Policy, nil
	}, verify)
}

func stillPresent(installed []string, name string) bool {
	for _, n := range installed {
		if n == name {
			return true
		}
	}
	return false
}

// emptiedContainers reports the top-level bundle keys whose container in the
// resulting policy was left with no members or elements at all.
func emptiedContainers(resultPolicy, bundleData []byte) ([]string, error) {
	root, err := hujson.Parse(resultPolicy)
	if err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}
	bv, err := hujson.Parse(bundleData)
	if err != nil {
		return nil, fmt.Errorf("parsing bundle: %w", err)
	}
	rootObj, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, nil
	}
	bundleObj, ok := bv.Value.(*hujson.Object)
	if !ok {
		return nil, nil
	}

	var empty []string
	for _, bm := range bundleObj.Members {
		key := literalName(bm.Name)
		if key == "" {
			continue
		}
		for _, m := range rootObj.Members {
			if literalName(m.Name) != key {
				continue
			}
			switch tv := m.Value.Value.(type) {
			case *hujson.Object:
				if len(tv.Members) == 0 {
					empty = append(empty, key)
				}
			case *hujson.Array:
				if len(tv.Elements) == 0 {
					empty = append(empty, key)
				}
			}
			break
		}
	}
	return empty, nil
}

func literalName(v hujson.Value) string {
	lit, ok := v.Value.(hujson.Literal)
	if !ok {
		return ""
	}
	return lit.String()
}

func looksLikePath(s string) bool {
	if strings.ContainsAny(s, "/\\") || strings.HasSuffix(s, ".hujson") || strings.HasSuffix(s, ".json") {
		return true
	}
	_, err := os.Stat(s)
	return err == nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
