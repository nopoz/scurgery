package policy

import (
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

const markerPrefix = "// scurgery:"

// keyMarkerSuffix flags a top-level key that scurgery created, and which
// removal should therefore take away entirely rather than emptying.
const keyMarkerSuffix = " owns-key"

func markerFor(ns string) string    { return markerPrefix + ns }
func markerKeyFor(ns string) string { return markerPrefix + ns + keyMarkerSuffix }

// namespaceEnds reports whether the byte after a marker terminates the
// namespace, so that namespace "aws" does not match marker "aws-router".
func namespaceEnds(b byte) bool {
	return b == '\n' || b == '\r' || b == ' ' || b == '\t'
}

func containsMarker(extra hujson.Extra, ns, suffix string) bool {
	needle := markerPrefix + ns + suffix
	s := string(extra)
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return false
		}
		end := i + j + len(needle)
		if end == len(s) || namespaceEnds(s[end]) {
			return true
		}
		i = end
	}
}

func hasMarker(extra hujson.Extra, ns string) bool {
	return containsMarker(extra, ns, "") || containsMarker(extra, ns, keyMarkerSuffix)
}

func hasKeyMarker(extra hujson.Extra, ns string) bool {
	return containsMarker(extra, ns, keyMarkerSuffix)
}

// markerNamespace returns the namespace named by the first scurgery marker in
// extra, if any. A member or element Apply produces carries at most one.
func markerNamespace(extra hujson.Extra) (string, bool) {
	s := string(extra)
	j := strings.Index(s, markerPrefix)
	if j < 0 {
		return "", false
	}
	start := j + len(markerPrefix)
	end := start
	for end < len(s) && !namespaceEnds(s[end]) {
		end++
	}
	if end == start {
		return "", false
	}
	return s[start:end], true
}

// indentOf returns the indentation a container uses for its children, taken
// from the whitespace preceding an existing child.
func indentOf(extra hujson.Extra, def string) string {
	s := string(extra)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return def
}

// markerExtra builds the whitespace-and-comment run that precedes a marked
// member: a newline, the marker on its own line, then the child indentation.
func markerExtra(marker, indent string) hujson.Extra {
	return hujson.Extra("\n" + indent + marker + "\n" + indent)
}

// ValidateNamespace rejects namespaces that would produce an unparseable or
// ambiguous marker comment.
func ValidateNamespace(ns string) error {
	if ns == "" {
		return fmt.Errorf("namespace must not be empty")
	}
	if ns == "owns-key" {
		return fmt.Errorf("namespace %q is reserved", ns)
	}
	for _, r := range ns {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == '/' {
			return fmt.Errorf("namespace %q must not contain whitespace or %q", ns, "/")
		}
	}
	return nil
}
