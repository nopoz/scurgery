package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

// RemoveResult carries the policy with a namespace's contributions removed.
type RemoveResult struct {
	Policy    []byte
	Removed   int
	Unmatched []string
}

// otherNamespaceMarker reports whether extra carries a scurgery marker for a
// namespace other than ns.
func otherNamespaceMarker(extra hujson.Extra, ns string) bool {
	s := string(extra)
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], markerPrefix)
		if j < 0 {
			return false
		}
		start := i + j + len(markerPrefix)
		end := start
		for end < len(s) && !namespaceEnds(s[end]) {
			end++
		}
		if end > start && s[start:end] != ns {
			return true
		}
		i = end
		if i == start {
			i++ // no progress: step past the prefix so this cannot loop
		}
	}
	return false
}

// otherNamespaceMarkers returns the distinct namespaces, other than ns,
// marked on any member or element of v.
func otherNamespaceMarkers(v hujson.Value, ns string) []string {
	seen := map[string]bool{}
	collect := func(extra hujson.Extra) {
		s := string(extra)
		for i := 0; i < len(s); {
			j := strings.Index(s[i:], markerPrefix)
			if j < 0 {
				return
			}
			start := i + j + len(markerPrefix)
			end := start
			for end < len(s) && !namespaceEnds(s[end]) {
				end++
			}
			if end > start && s[start:end] != ns {
				seen[s[start:end]] = true
			}
			i = end
			if i == start {
				i++ // no progress: step past the prefix so this cannot loop
			}
		}
	}
	switch t := v.Value.(type) {
	case *hujson.Object:
		for _, m := range t.Members {
			collect(m.Name.BeforeExtra)
		}
	case *hujson.Array:
		for _, el := range t.Elements {
			collect(el.BeforeExtra)
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// RemoveBlockers reports which other namespaces are marked inside a
// container ns created, and so are blocking ns's removal from taking full
// effect: Remove will not drop a container ns owns while another namespace
// still has something inside it. It returns nil when nothing blocks ns.
func RemoveBlockers(policy []byte, ns string) ([]string, error) {
	if err := ValidateNamespace(ns); err != nil {
		return nil, err
	}
	root, err := hujson.Parse(policy)
	if err != nil {
		return nil, fmt.Errorf("parsing policy: %w", err)
	}
	rootObj, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("policy is not a JSON object")
	}

	seen := map[string]bool{}
	for _, m := range rootObj.Members {
		if !hasKeyMarker(m.Name.BeforeExtra, ns) {
			continue
		}
		for _, other := range otherNamespaceMarkers(m.Value, ns) {
			seen[other] = true
		}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// containerSharedWithOtherNamespace reports whether any member or element of
// v carries a marker for a namespace other than ns. An owns-key container
// must not be dropped whole while another namespace still has something
// inside it: the container's own marker is left untouched in that case, so a
// later Remove of the original namespace finds it unshared once the other
// namespace is gone too.
func containerSharedWithOtherNamespace(v hujson.Value, ns string) bool {
	switch t := v.Value.(type) {
	case *hujson.Object:
		for _, m := range t.Members {
			if otherNamespaceMarker(m.Name.BeforeExtra, ns) {
				return true
			}
		}
	case *hujson.Array:
		for _, el := range t.Elements {
			if otherNamespaceMarker(el.BeforeExtra, ns) {
				return true
			}
		}
	}
	return false
}

// Remove deletes every member and element marked with ns, and every top-level
// key marked as created by ns. Containers the operator owned are left in place
// even when every marked member inside them is removed.
func Remove(policy []byte, ns string) (*RemoveResult, error) {
	if err := ValidateNamespace(ns); err != nil {
		return nil, err
	}
	root, err := hujson.Parse(policy)
	if err != nil {
		return nil, fmt.Errorf("parsing policy: %w", err)
	}
	rootObj, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("policy is not a JSON object")
	}

	res := &RemoveResult{}
	rootHad := trailingComma(root)
	kept := make([]hujson.ObjectMember, 0, len(rootObj.Members))

	for _, m := range rootObj.Members {
		if hasKeyMarker(m.Name.BeforeExtra, ns) && !containerSharedWithOtherNamespace(m.Value, ns) {
			res.Removed++
			continue
		}
		had := trailingComma(m.Value)
		switch tv := m.Value.Value.(type) {
		case *hujson.Object:
			km := make([]hujson.ObjectMember, 0, len(tv.Members))
			for _, im := range tv.Members {
				if hasMarker(im.Name.BeforeExtra, ns) {
					res.Removed++
					continue
				}
				km = append(km, im)
			}
			tv.Members = km
		case *hujson.Array:
			ke := make([]hujson.Value, 0, len(tv.Elements))
			for _, el := range tv.Elements {
				if hasMarker(el.BeforeExtra, ns) {
					res.Removed++
					continue
				}
				ke = append(ke, el)
			}
			tv.Elements = ke
		}
		setTrailingComma(m.Value, had)
		kept = append(kept, m)
	}

	rootObj.Members = kept
	setTrailingComma(root, rootHad)
	res.Policy = root.Pack()
	return res, nil
}

// RemoveStructural deletes members and elements that match the bundle by
// value. It is a recovery path for a policy whose marker comments were lost,
// and it cannot recognise a member the operator has since edited: those are
// reported in Unmatched rather than removed.
func RemoveStructural(policy, bundle []byte) (*RemoveResult, error) {
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

	res := &RemoveResult{}
	rootHad := trailingComma(root)

	for _, bm := range bundleObj.Members {
		key := memberName(bm)
		target := findMember(rootObj, key)
		if target == nil {
			res.Unmatched = append(res.Unmatched, key)
			continue
		}
		had := trailingComma(target.Value)
		switch tv := target.Value.Value.(type) {
		case *hujson.Object:
			bo, ok := bm.Value.Value.(*hujson.Object)
			if !ok {
				res.Unmatched = append(res.Unmatched, key)
				continue
			}
			for _, want := range bo.Members {
				wantName := memberName(want)
				idx := -1
				for i, im := range tv.Members {
					if memberName(im) == wantName && semanticEqual(im.Value, want.Value) {
						idx = i
						break
					}
				}
				if idx < 0 {
					res.Unmatched = append(res.Unmatched, fmt.Sprintf("%s.%q", key, wantName))
					continue
				}
				tv.Members = append(tv.Members[:idx], tv.Members[idx+1:]...)
				res.Removed++
			}
		case *hujson.Array:
			ba, ok := bm.Value.Value.(*hujson.Array)
			if !ok {
				res.Unmatched = append(res.Unmatched, key)
				continue
			}
			for n, want := range ba.Elements {
				idx := -1
				for i, el := range tv.Elements {
					if semanticEqual(el, want) {
						idx = i
						break
					}
				}
				if idx < 0 {
					res.Unmatched = append(res.Unmatched, fmt.Sprintf("%s[%d]", key, n))
					continue
				}
				tv.Elements = append(tv.Elements[:idx], tv.Elements[idx+1:]...)
				res.Removed++
			}
		default:
			res.Unmatched = append(res.Unmatched, key)
		}
		setTrailingComma(target.Value, had)
	}

	setTrailingComma(root, rootHad)
	res.Policy = root.Pack()
	return res, nil
}

// Namespaces lists the scurgery namespaces present in a policy, sorted.
func Namespaces(policy []byte) ([]string, error) {
	root, err := hujson.Parse(policy)
	if err != nil {
		return nil, fmt.Errorf("parsing policy: %w", err)
	}
	seen := map[string]bool{}
	collect := func(extra hujson.Extra) {
		s := string(extra)
		for i := 0; i < len(s); {
			j := strings.Index(s[i:], markerPrefix)
			if j < 0 {
				return
			}
			start := i + j + len(markerPrefix)
			end := start
			for end < len(s) && !namespaceEnds(s[end]) {
				end++
			}
			if end > start {
				seen[s[start:end]] = true
			}
			i = end
			if i == start {
				i++ // no progress: step past the prefix so this cannot loop
			}
		}
	}

	// Markers can sit before a top-level key, before a member of any nested
	// object, or before an array element, so walk everything.
	var walk func(v hujson.Value)
	walk = func(v hujson.Value) {
		collect(v.BeforeExtra)
		switch t := v.Value.(type) {
		case *hujson.Object:
			for _, m := range t.Members {
				collect(m.Name.BeforeExtra)
				walk(m.Value)
			}
		case *hujson.Array:
			for _, el := range t.Elements {
				walk(el)
			}
		}
	}
	walk(root)

	out := make([]string, 0, len(seen))
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out, nil
}
