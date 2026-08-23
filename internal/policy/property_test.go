package policy

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// propertySeed is fixed so a failing trial is reproducible: re-running the
// suite must generate the exact same policies and bundles.
const propertySeed = 20260822

// propertyTrials balances coverage against suite speed. Each trial builds a
// distinct policy and bundle shape, so this is not one test repeated;
// it's propertyTrials different (policy, bundle) pairs.
const propertyTrials = 120

// containerKeyPool names the top-level keys a generated policy or bundle may
// use. Kept small and fixed so "does this key already exist" logic in the
// generator stays simple.
var containerKeyPool = []string{"cA", "cB", "cC", "cD"}

type genMember struct {
	key   string
	value string
}

// objectStyle renders an object container's members in one of several
// formatting shapes: single line, multiline with and without a trailing
// comma, a bundle-only inline comment before the last comma, and a leading
// comment before a member (like a fixture's operator commentary). style
// selects which; an empty member list always renders as "{}".
func objectStyle(indent string, members []genMember, style int) string {
	if len(members) == 0 {
		return "{}"
	}
	switch style % 5 {
	case 0: // single line, no trailing comma
		parts := make([]string, len(members))
		for i, m := range members {
			parts[i] = fmt.Sprintf("%q: %s", m.key, m.value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case 1: // multiline, trailing comma
		var sb strings.Builder
		sb.WriteString("{\n")
		for _, m := range members {
			fmt.Fprintf(&sb, "%s\t%q: %s,\n", indent, m.key, m.value)
		}
		sb.WriteString(indent + "}")
		return sb.String()
	case 2: // multiline, no trailing comma
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, m := range members {
			sep := ","
			if i == len(members)-1 {
				sep = ""
			}
			fmt.Fprintf(&sb, "%s\t%q: %s%s\n", indent, m.key, m.value, sep)
		}
		sb.WriteString(indent + "}")
		return sb.String()
	case 3: // multiline, comment before the trailing comma on the last member
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, m := range members {
			if i == len(members)-1 {
				fmt.Fprintf(&sb, "%s\t%q: %s /* note */,\n", indent, m.key, m.value)
			} else {
				fmt.Fprintf(&sb, "%s\t%q: %s,\n", indent, m.key, m.value)
			}
		}
		sb.WriteString(indent + "}")
		return sb.String()
	default: // multiline, trailing comma, a leading comment on the first member
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, m := range members {
			if i == 0 {
				fmt.Fprintf(&sb, "%s\t// %s\n", indent, m.key)
			}
			fmt.Fprintf(&sb, "%s\t%q: %s,\n", indent, m.key, m.value)
		}
		sb.WriteString(indent + "}")
		return sb.String()
	}
}

// arrayStyle mirrors objectStyle for an array container, whose elements are
// already-rendered value text.
func arrayStyle(indent string, elements []string, style int) string {
	if len(elements) == 0 {
		return "[]"
	}
	switch style % 4 {
	case 0: // single line, no trailing comma
		return "[" + strings.Join(elements, ", ") + "]"
	case 1: // multiline, trailing comma
		var sb strings.Builder
		sb.WriteString("[\n")
		for _, e := range elements {
			fmt.Fprintf(&sb, "%s\t%s,\n", indent, e)
		}
		sb.WriteString(indent + "]")
		return sb.String()
	case 2: // multiline, no trailing comma
		var sb strings.Builder
		sb.WriteString("[\n")
		for i, e := range elements {
			sep := ","
			if i == len(elements)-1 {
				sep = ""
			}
			fmt.Fprintf(&sb, "%s\t%s%s\n", indent, e, sep)
		}
		sb.WriteString(indent + "]")
		return sb.String()
	default: // multiline, comment before the trailing comma on the last element
		var sb strings.Builder
		sb.WriteString("[\n")
		for i, e := range elements {
			if i == len(elements)-1 {
				fmt.Fprintf(&sb, "%s\t%s /* note */,\n", indent, e)
			} else {
				fmt.Fprintf(&sb, "%s\t%s,\n", indent, e)
			}
		}
		sb.WriteString(indent + "]")
		return sb.String()
	}
}

// containerShape records what a generator built for one top-level key, so
// the bundle generator can target it consistently: merging must offer the
// same container kind the policy already used there, or Apply errors out on
// a type mismatch rather than exercising the merge path at all.
type containerShape struct {
	key      string
	isObject bool
}

func randValue(r *rand.Rand, tag string) string {
	return fmt.Sprintf(`["v-%s"]`, tag)
}

// genPolicy builds one generated policy text and reports which top-level
// keys it used and their kind, so genBundle can merge into them without
// guessing.
func genPolicy(r *rand.Rand, trial int) (text string, shapes []containerShape) {
	var top []genMember
	for _, key := range containerKeyPool {
		roll := r.Intn(10)
		if roll < 3 {
			continue // this key is absent from the policy entirely
		}
		isObject := r.Intn(2) == 0
		n := r.Intn(4) // 0..3 members/elements: exercises empty and single-member too
		style := r.Intn(5)

		if isObject {
			members := make([]genMember, n)
			for i := range members {
				members[i] = genMember{
					key:   fmt.Sprintf("p-%d-%s-%d", trial, key, i),
					value: randValue(r, fmt.Sprintf("%d-%s-%d", trial, key, i)),
				}
			}
			top = append(top, genMember{key: key, value: objectStyle("\t\t", members, style)})
		} else {
			elements := make([]string, n)
			for i := range elements {
				elements[i] = fmt.Sprintf("%q", fmt.Sprintf("p-elem-%d-%s-%d", trial, key, i))
			}
			top = append(top, genMember{key: key, value: arrayStyle("\t\t", elements, style)})
		}
		shapes = append(shapes, containerShape{key: key, isObject: isObject})
	}

	rootStyle := r.Intn(5)
	body := objectStyle("\t", top, rootStyle)
	if r.Intn(2) == 0 {
		body += "\n" // most fixtures end with a trailing newline
	}
	return body, shapes
}

// genBundle builds a bundle that merges fresh, disjoint-named content into
// some of the policy's existing containers and creates at least one brand
// new one, without ever colliding on a member name, an element value, or a
// container kind. That is what keeps every trial's Apply free of conflicts:
// a conflict would leave Apply's Policy nil, which is an uninteresting
// reason for this property to fail, so the generator avoids it by
// construction rather than relying on a runtime skip.
func genBundle(r *rand.Rand, trial int, shapes []containerShape) string {
	var top []genMember

	// Merge into up to two of the policy's existing containers.
	merged := 0
	for _, s := range shapes {
		if merged >= 2 || r.Intn(2) == 0 {
			continue
		}
		merged++
		n := 1 + r.Intn(2)
		style := r.Intn(5)
		if s.isObject {
			members := make([]genMember, n)
			for i := range members {
				members[i] = genMember{
					key:   fmt.Sprintf("b-%d-%s-%d", trial, s.key, i),
					value: randValue(r, fmt.Sprintf("bundle-%d-%s-%d", trial, s.key, i)),
				}
			}
			top = append(top, genMember{key: s.key, value: objectStyle("\t\t", members, style)})
		} else {
			elements := make([]string, n)
			for i := range elements {
				elements[i] = fmt.Sprintf("%q", fmt.Sprintf("b-elem-%d-%s-%d", trial, s.key, i))
			}
			top = append(top, genMember{key: s.key, value: arrayStyle("\t\t", elements, style)})
		}
	}

	// Always create at least one brand new top-level container, using a key
	// name outside containerKeyPool entirely so it can never collide with
	// anything the policy generator chose.
	newKey := fmt.Sprintf("new%d", trial)
	if r.Intn(2) == 0 {
		top = append(top, genMember{key: newKey, value: objectStyle("\t\t", []genMember{
			{key: fmt.Sprintf("b-%d-new-0", trial), value: randValue(r, fmt.Sprintf("new-%d-0", trial))},
		}, r.Intn(5))})
	} else {
		top = append(top, genMember{key: newKey, value: arrayStyle("\t\t", []string{
			fmt.Sprintf("%q", fmt.Sprintf("b-elem-%d-new-0", trial)),
		}, r.Intn(4))})
	}

	return objectStyle("\t", top, r.Intn(5)) + "\n"
}

// TestApplyThenRemoveRestoresGeneratedPolicies is the property test the
// design calls for: for many generated (policy, bundle) pairs, spanning
// randomised whitespace, trailing commas present and absent, inline
// comments, a comment before a comma, empty containers, single-member and
// single-line containers, and a missing trailing newline, applying a bundle
// and then removing it must restore the original policy bytes exactly. The
// five-fixture-by-four-bundle matrix in roundtrip_test.go is a fixed corpus;
// this covers shapes that corpus does not enumerate, with a fixed seed so a
// failure reproduces deterministically.
func TestApplyThenRemoveRestoresGeneratedPolicies(t *testing.T) {
	r := rand.New(rand.NewSource(propertySeed))

	for trial := 0; trial < propertyTrials; trial++ {
		policyText, shapes := genPolicy(r, trial)
		bundleText := genBundle(r, trial, shapes)
		ns := fmt.Sprintf("prop-%d", trial)

		if _, err := hujson.Parse([]byte(policyText)); err != nil {
			t.Fatalf("trial %d: generated policy does not parse: %v\n%s", trial, err, policyText)
		}
		if _, err := hujson.Parse([]byte(bundleText)); err != nil {
			t.Fatalf("trial %d: generated bundle does not parse: %v\n%s", trial, err, bundleText)
		}

		applied, err := Apply([]byte(policyText), []byte(bundleText), ns, ApplyOptions{})
		if err != nil {
			t.Fatalf("trial %d: Apply: %v\npolicy:\n%s\nbundle:\n%s", trial, err, policyText, bundleText)
		}
		if len(applied.Conflicts) > 0 {
			// The generator is built to avoid this by construction (disjoint
			// member names and matching container kinds); a conflict here
			// means that guarantee broke, which is itself worth failing on
			// rather than silently skipping.
			t.Fatalf("trial %d: unexpected conflicts, generator should never produce these: %+v", trial, applied.Conflicts)
		}

		removed, err := Remove(applied.Policy, ns)
		if err != nil {
			t.Fatalf("trial %d: Remove: %v", trial, err)
		}
		if string(removed.Policy) != policyText {
			t.Fatalf("trial %d: apply then remove did not restore the original byte-for-byte\n--- want ---\n%s\n--- got ---\n%s",
				trial, policyText, removed.Policy)
		}
	}
}
