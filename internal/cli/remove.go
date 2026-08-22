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

// removeVerify is policy.VerifyRemove behind a package variable so tests can
// wrap it to observe whether the marker-based self-check actually ran, and
// that it does not run on the structural path.
var removeVerify = policy.VerifyRemove

// removeStructuralVerify is policy.VerifyRemoveStructural behind a package
// variable, mirroring removeVerify, so tests can observe it actually running
// on the structural path and confirm that a failing check blocks the write
// rather than being merely present but unreachable.
var removeStructuralVerify = policy.VerifyRemoveStructural

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
	// survives untouched, using the markers themselves as the oracle. A
	// structural removal has no markers to consult by definition, so that
	// check cannot apply to it; policy.VerifyRemoveStructural takes its
	// place there, using the bundle as an independent oracle instead.
	var usedStructural bool
	verify := func(before, after []byte) error {
		if usedStructural {
			return removeStructuralVerify(before, after, b.Data)
		}
		return removeVerify(before, after, name)
	}

	return writePolicy(ctx, env, "remove "+name, func(current []byte) ([]byte, error) {
		res, err := policy.Remove(current, name)
		if err != nil {
			return nil, err
		}

		// A namespace can still be present after Remove even when something
		// was removed: Remove will not drop a top-level container ns
		// created while another namespace still has something inside it,
		// and that container's own key marker is what keeps ns "present".
		// This must be checked before deciding anything from res.Removed,
		// since a marker elsewhere (an unrelated key, say) can make
		// Removed > 0 while ns is still very much live in the container.
		afterNames, err := policy.Namespaces(res.Policy)
		if err != nil {
			return nil, err
		}
		if stillPresent(afterNames, name) {
			blockers, err := policy.RemoveBlockers(current, name)
			if err != nil {
				return nil, err
			}
			if len(blockers) == 0 {
				// Remove() only scans the marker locations Apply produces:
				// a top-level key, or an immediate member/element of a
				// top-level container. A marker sitting deeper than that
				// (only reachable by hand-editing the policy) is invisible
				// to both Remove and RemoveBlockers, but Namespaces still
				// finds it, so ns looks present with nothing identifiable
				// holding it there. Say that plainly rather than asserting
				// a shared container that was never found.
				return nil, fmt.Errorf("%q still appears in the policy after removing everything scurgery could "+
					"find, but nothing scurgery recognizes is holding it there: no top-level container it "+
					"created is shared with another namespace. Nothing was changed. The policy may carry a %q "+
					"marker somewhere scurgery does not know how to interpret", name, name)
			}
			return nil, fmt.Errorf("%q was not removed: it shares a top-level container that scurgery created "+
				"with another namespace, so nothing marked for %q could be taken out without also destroying "+
				"that namespace's rules. Nothing was changed. Remove %s first, then re-run this command to "+
				"finish removing %q", name, name, strings.Join(blockers, ", "), name)
		}

		if res.Removed > 0 {
			fmt.Fprintf(env.Out, "removing %d marked entr%s\n", res.Removed, plural(res.Removed))
			return res.Policy, nil
		}

		// Nothing was removed and ns is not present at all: it was never
		// installed by marker. Fall through to the structural path if a
		// bundle is available, or say so plainly if all we have is a name.
		installed, err := policy.Namespaces(current)
		if err != nil {
			return nil, err
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
		fmt.Fprintf(env.Out, "warning: the marker-based self-check cannot run for a structural removal, since it "+
			"relies on markers this policy no longer has; a reduced content check ran in its place\n")
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
