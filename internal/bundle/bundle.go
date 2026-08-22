// Package bundle loads the partial policy file that scurgery contributes to a
// tailnet. A bundle is a policy file fragment: the same shape as the policy
// itself, containing only the keys to add.
package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/nopoz/scurgery/internal/policy"
)

type Bundle struct {
	Name string
	Data []byte
	Path string
}

// Load reads a bundle. The namespace comes from nameOverride if given, and
// otherwise from the filename stem, which keeps it stable across apply and
// remove without the operator having to remember a flag.
func Load(path, nameOverride string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bundle: %w", err)
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing bundle %s: %w", path, err)
	}
	if _, ok := v.Value.(*hujson.Object); !ok {
		return nil, fmt.Errorf("bundle %s must be a JSON object", path)
	}

	name := nameOverride
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if err := policy.ValidateNamespace(name); err != nil {
		return nil, fmt.Errorf("bundle name %q is not usable: %w (pass --name to choose one)", name, err)
	}
	return &Bundle{Name: name, Data: data, Path: path}, nil
}
