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
	var p *hujson.Extra
	switch t := v.Value.(type) {
	case *hujson.Array:
		if len(t.Elements) == 0 {
			return
		}
		p = &t.Elements[len(t.Elements)-1].AfterExtra
	case *hujson.Object:
		if len(t.Members) == 0 {
			return
		}
		p = &t.Members[len(t.Members)-1].Value.AfterExtra
	default:
		return
	}
	if want {
		if *p == nil {
			*p = hujson.Extra{}
		}
		return
	}
	*p = nil
}
