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

// markerIndexIn returns the offset of the first marker for ns in extra, or -1
// when there is none. The namespace-end check is what keeps namespace "aws"
// from matching a marker for "aws-router".
func markerIndexIn(extra hujson.Extra, ns, suffix string) int {
	needle := markerPrefix + ns + suffix
	s := string(extra)
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return -1
		}
		end := i + j + len(needle)
		if end == len(s) || namespaceEnds(s[end]) {
			return i + j
		}
		i = end
	}
}

func containsMarker(extra hujson.Extra, ns, suffix string) bool {
	return markerIndexIn(extra, ns, suffix) >= 0
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

// salvageBeforeExtra returns the part of a marked member's leading extra that
// scurgery did not write, and so must survive the member's removal.
//
// A member's leading extra starts at the previous member's comma, which means
// an operator commenting on their own rule after an apply puts that comment
// into the extra of the member that follows it: scurgery's. markerExtra always
// begins with a newline and puts the marker on a line of its own, so
// everything before that newline came from somewhere else. It returns nothing
// when the marker shares its line with other text, since there is then no
// split that leaves both halves intact.
func salvageBeforeExtra(extra hujson.Extra, ns string) hujson.Extra {
	i := markerIndexIn(extra, ns, "")
	if i < 0 {
		return nil
	}
	nl := strings.LastIndexByte(string(extra[:i]), '\n')
	if nl < 0 {
		return nil
	}
	// Stop before the whole line terminator, both bytes of it on a CRLF
	// policy, or the salvaged run carries a stray carriage return.
	if nl > 0 && extra[nl-1] == '\r' {
		nl--
	}
	return extra[:nl]
}

// appendSalvage collects the runs rescued from a run of consecutive removed
// members, keeping each on its own line so that a line comment in one does not
// swallow the next.
func appendSalvage(acc, next hujson.Extra) hujson.Extra {
	if len(next) == 0 {
		return acc
	}
	if len(acc) > 0 {
		acc = append(acc, '\n')
	}
	return append(acc, next...)
}

// prependExtra puts salvaged text back in front of the extra that follows the
// member it was rescued from.
func prependExtra(salvaged, dst hujson.Extra) hujson.Extra {
	if len(salvaged) == 0 {
		return dst
	}
	out := make(hujson.Extra, 0, len(salvaged)+len(dst)+1)
	out = append(out, salvaged...)
	// The salvaged run ends where scurgery's own newline used to be, so a line
	// comment at the end of it needs one back or it swallows what comes next.
	if len(dst) == 0 || (dst[0] != '\n' && dst[0] != '\r') {
		out = append(out, '\n')
	}
	return append(out, dst...)
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
