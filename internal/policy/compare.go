package policy

import (
	"encoding/json"
	"reflect"

	"github.com/tailscale/hujson"
)

// normalize strips everything that carries no meaning to Tailscale, so that
// two values are compared on content alone. Array order is preserved because
// it is meaningful in a policy file.
func normalize(v hujson.Value) (any, error) {
	c := v.Clone()
	c.Standardize()
	var out any
	if err := json.Unmarshal(c.Pack(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func semanticEqual(a, b hujson.Value) bool {
	na, err := normalize(a)
	if err != nil {
		return false
	}
	nb, err := normalize(b)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(na, nb)
}

// compactString renders a value without comments or whitespace, for use in
// conflict messages where the operator needs to see both sides.
func compactString(v hujson.Value) string {
	c := v.Clone()
	c.Standardize()
	c.Minimize()
	return string(c.Pack())
}
