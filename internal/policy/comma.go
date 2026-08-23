package policy

import "github.com/tailscale/hujson"

// hujson emits a trailing comma only when the last element's AfterExtra is
// non-nil, using a zero-length slice as the sentinel. Appending or deleting at
// the tail changes which element is last, so the sentinel must be carried
// across the mutation explicitly. Treat it as a property of the container,
// read before mutating and restored after.

func trailingComma(v hujson.Value) bool {
	switch t := v.Value.(type) {
	case *hujson.Array:
		if len(t.Elements) == 0 {
			return false
		}
		return t.Elements[len(t.Elements)-1].AfterExtra != nil
	case *hujson.Object:
		if len(t.Members) == 0 {
			return false
		}
		return t.Members[len(t.Members)-1].Value.AfterExtra != nil
	}
	return false
}

func setTrailingComma(v hujson.Value, want bool) {
	var last, container *hujson.Extra
	switch t := v.Value.(type) {
	case *hujson.Array:
		if len(t.Elements) == 0 {
			return
		}
		last = &t.Elements[len(t.Elements)-1].AfterExtra
		container = &t.AfterExtra
	case *hujson.Object:
		if len(t.Members) == 0 {
			return
		}
		last = &t.Members[len(t.Members)-1].Value.AfterExtra
		container = &t.AfterExtra
	default:
		return
	}
	if want {
		if *last == nil {
			*last = hujson.Extra{}
		}
		return
	}
	// The element's AfterExtra can hold real comment text, not just the
	// sentinel. Clearing it must not discard that text: move it onto the
	// container's AfterExtra, the way hujson's own parser and formatter do.
	if len(*last) > 0 {
		moved := make(hujson.Extra, 0, len(*last)+len(*container))
		moved = append(moved, *last...)
		moved = append(moved, *container...)
		*container = moved
	}
	*last = nil
}
