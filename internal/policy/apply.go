package policy

import (
	"fmt"

	"github.com/tailscale/hujson"
)

// Conflict is a member scurgery wanted to add that already exists with a
// different value.
type Conflict struct {
	Path     string
	Existing string
	Incoming string
}

// ApplyOptions selects what to do about conflicts. With neither set, a
// conflict blocks the whole operation and nothing is changed.
type ApplyOptions struct {
	Force         bool
	SkipConflicts bool
}

// SharedMember names a member or element the bundle declares that was
// skipped because it already exists, semantically equal, under another
// namespace's marker: the applying namespace never marks it, so it survives
// only for as long as the owning namespace's contribution does.
type SharedMember struct {
	Path  string
	Owner string
}

// ApplyResult carries the merged policy, or a nil Policy when conflicts
// blocked the operation. When Policy is nil, Added, Updated and Skipped are
// meaningless: they reflect a merge that was not applied.
type ApplyResult struct {
	Policy []byte
	Added  int
	// Updated counts a member that already carried ns's own marker and whose
	// value changed: re-applying an updated bundle for a namespace scurgery
	// already installed, not a conflict with something else's content.
	Updated   int
	Skipped   int
	Conflicts []Conflict
	// Shared lists members or elements skipped as already-present that are
	// actually owned by a different namespace, not by ns and not by the
	// operator. See SharedMember.
	Shared []SharedMember
}

func memberName(m hujson.ObjectMember) string {
	lit, ok := m.Name.Value.(hujson.Literal)
	if !ok {
		return ""
	}
	return lit.String()
}

func findMember(o *hujson.Object, name string) *hujson.ObjectMember {
	for i := range o.Members {
		if memberName(o.Members[i]) == name {
			return &o.Members[i]
		}
	}
	return nil
}

// Apply merges bundle into policy, marking every contributed member with ns.
// It is schema-agnostic: objects merge by member key, arrays merge by append.
func Apply(policy, bundle []byte, ns string, opts ApplyOptions) (*ApplyResult, error) {
	if err := ValidateNamespace(ns); err != nil {
		return nil, err
	}
	root, err := hujson.Parse(policy)
	if err != nil {
		return nil, fmt.Errorf("parsing policy: %w", err)
	}
	bv, err := hujson.Parse(bundle)
	if err != nil {
		return nil, fmt.Errorf("parsing bundle: %w", err)
	}
	rootObj, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("policy is not a JSON object")
	}
	bundleObj, ok := bv.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("bundle is not a JSON object")
	}

	res := &ApplyResult{}
	rootHad := trailingComma(root)

	for _, bm := range bundleObj.Members {
		key := memberName(bm)
		target := findMember(rootObj, key)

		if target == nil {
			ind := "\t"
			if len(rootObj.Members) > 0 {
				ind = indentOf(rootObj.Members[len(rootObj.Members)-1].Name.BeforeExtra, ind)
			}
			nm := bm
			nm.Name.BeforeExtra = markerExtra(markerKeyFor(ns), ind)
			nm.Value.AfterExtra = nil
			rootObj.Members = append(rootObj.Members, nm)
			res.Added++
			continue
		}

		had := trailingComma(target.Value)
		switch tv := target.Value.Value.(type) {
		case *hujson.Object:
			bo, ok := bm.Value.Value.(*hujson.Object)
			if !ok {
				return nil, fmt.Errorf("bundle key %q is not an object but the policy's is", key)
			}
			ind := "\t\t"
			if len(tv.Members) > 0 {
				ind = indentOf(tv.Members[len(tv.Members)-1].Name.BeforeExtra, ind)
			}
			for _, m := range bo.Members {
				name := memberName(m)
				if existing := findMember(tv, name); existing != nil {
					if semanticEqual(existing.Value, m.Value) {
						res.Skipped++
						if owner, ok := markerNamespace(existing.Name.BeforeExtra); ok && owner != ns {
							res.Shared = append(res.Shared, SharedMember{
								Path:  fmt.Sprintf("%s.%q", key, name),
								Owner: owner,
							})
						}
						continue
					}
					owned := hasMarker(existing.Name.BeforeExtra, ns)
					if !owned && hasKeyMarker(target.Name.BeforeExtra, ns) {
						// The enclosing top-level key was created wholesale
						// by a previous apply of ns, so its members carry no
						// marker of their own (only the key does). Such a
						// member is still ns's own unless it carries some
						// OTHER namespace's marker, which can only happen if
						// a later apply of a different namespace added it
						// into the same container.
						if owner, ok := markerNamespace(existing.Name.BeforeExtra); !ok || owner == ns {
							owned = true
						}
					}
					if owned {
						// This member is scurgery's own from a previous
						// apply of the same namespace: an updated bundle
						// value overwrites it in place. It is not a
						// conflict with content scurgery does not own, and
						// it stays reversible.
						nv := m.Value
						nv.BeforeExtra = existing.Value.BeforeExtra
						nv.AfterExtra = existing.Value.AfterExtra
						existing.Value = nv
						res.Updated++
						continue
					}
					res.Conflicts = append(res.Conflicts, Conflict{
						Path:     fmt.Sprintf("%s.%q", key, name),
						Existing: compactString(existing.Value),
						Incoming: compactString(m.Value),
					})
					if opts.Force {
						nv := m.Value
						nv.BeforeExtra = existing.Value.BeforeExtra
						nv.AfterExtra = existing.Value.AfterExtra
						existing.Value = nv
						res.Added++
					}
					continue
				}
				nm := m
				nm.Name.BeforeExtra = markerExtra(markerFor(ns), ind)
				nm.Value.AfterExtra = nil
				tv.Members = append(tv.Members, nm)
				res.Added++
			}

		case *hujson.Array:
			ba, ok := bm.Value.Value.(*hujson.Array)
			if !ok {
				return nil, fmt.Errorf("bundle key %q is not an array but the policy's is", key)
			}
			ind := "\t\t"
			if len(tv.Elements) > 0 {
				ind = indentOf(tv.Elements[len(tv.Elements)-1].BeforeExtra, ind)
			}
			for _, el := range ba.Elements {
				dup := false
				var dupOf hujson.Value
				for _, existing := range tv.Elements {
					if semanticEqual(existing, el) {
						dup = true
						dupOf = existing
						break
					}
				}
				if dup {
					res.Skipped++
					if owner, ok := markerNamespace(dupOf.BeforeExtra); ok && owner != ns {
						res.Shared = append(res.Shared, SharedMember{
							Path:  key,
							Owner: owner,
						})
					}
					continue
				}
				ne := el
				ne.BeforeExtra = markerExtra(markerFor(ns), ind)
				ne.AfterExtra = nil
				tv.Elements = append(tv.Elements, ne)
				res.Added++
			}

		default:
			if semanticEqual(target.Value, bm.Value) {
				res.Skipped++
				continue
			}
			if hasMarker(target.Name.BeforeExtra, ns) {
				// scurgery's own top-level scalar from a previous apply of
				// the same namespace: overwrite in place, see the object
				// branch above for why this is not a conflict.
				nv := bm.Value
				nv.BeforeExtra = target.Value.BeforeExtra
				nv.AfterExtra = target.Value.AfterExtra
				target.Value = nv
				res.Updated++
				break
			}
			res.Conflicts = append(res.Conflicts, Conflict{
				Path:     key,
				Existing: compactString(target.Value),
				Incoming: compactString(bm.Value),
			})
			if opts.Force {
				nv := bm.Value
				nv.BeforeExtra = target.Value.BeforeExtra
				nv.AfterExtra = target.Value.AfterExtra
				target.Value = nv
				res.Added++
			}
		}
		setTrailingComma(target.Value, had)
	}

	setTrailingComma(root, rootHad)

	if len(res.Conflicts) > 0 && !opts.Force && !opts.SkipConflicts {
		return res, nil // Policy stays nil so a caller cannot write it by accident
	}
	res.Policy = root.Pack()
	return res, nil
}
