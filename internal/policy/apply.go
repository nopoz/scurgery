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

// ApplyResult carries the merged policy, or a nil Policy when conflicts
// blocked the operation. When Policy is nil, Added and Skipped are
// meaningless: they reflect a merge that was not applied.
type ApplyResult struct {
	Policy    []byte
	Added     int
	Skipped   int
	Conflicts []Conflict
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
				for _, existing := range tv.Elements {
					if semanticEqual(existing, el) {
						dup = true
						break
					}
				}
				if dup {
					res.Skipped++
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
