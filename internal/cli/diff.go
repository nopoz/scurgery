// Package cli composes the policy, bundle and api packages into commands.
package cli

import (
	"fmt"
	"strings"
)

// Unified renders a line diff with the given number of context lines. It uses
// a standard longest-common-subsequence table, which is ample for policy files
// and avoids a dependency.
func Unified(before, after []byte, context int) string {
	a := splitLines(string(before))
	b := splitLines(string(after))

	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}

	type line struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var lines []line
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			lines = append(lines, line{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			lines = append(lines, line{'-', a[i]})
			i++
		default:
			lines = append(lines, line{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		lines = append(lines, line{'-', a[i]})
	}
	for ; j < len(b); j++ {
		lines = append(lines, line{'+', b[j]})
	}

	keep := make([]bool, len(lines))
	changed := false
	for n, l := range lines {
		if l.kind == ' ' {
			continue
		}
		changed = true
		lo := max(0, n-context)
		hi := min(len(lines)-1, n+context)
		for k := lo; k <= hi; k++ {
			keep[k] = true
		}
	}
	if !changed {
		return ""
	}

	var sb strings.Builder
	gap := false
	for n, l := range lines {
		if !keep[n] {
			gap = true
			continue
		}
		if gap {
			fmt.Fprintln(&sb, "  ...")
			gap = false
		}
		fmt.Fprintf(&sb, "%c%s\n", l.kind, l.text)
	}
	return sb.String()
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
