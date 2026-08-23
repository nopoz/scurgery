package policy

import (
	"testing"

	"github.com/tailscale/hujson"
)

func TestMarkerStrings(t *testing.T) {
	if got, want := markerFor("aws-router"), "// scurgery:aws-router"; got != want {
		t.Errorf("markerFor = %q, want %q", got, want)
	}
	if got, want := markerKeyFor("aws-router"), "// scurgery:aws-router owns-key"; got != want {
		t.Errorf("markerKeyFor = %q, want %q", got, want)
	}
}

func TestHasMarkerMatchesOwnNamespaceOnly(t *testing.T) {
	extra := hujson.Extra("\n\t// scurgery:aws-router\n\t")
	if !hasMarker(extra, "aws-router") {
		t.Error("hasMarker should match its own namespace")
	}
	if hasMarker(extra, "other") {
		t.Error("hasMarker must not match a different namespace")
	}
	if hasMarker(hujson.Extra("\n\t// just a comment\n\t"), "aws-router") {
		t.Error("hasMarker must not match an unrelated comment")
	}
}

// "aws" must not match "aws-router": a prefix is not a namespace.
func TestHasMarkerRejectsPrefixCollision(t *testing.T) {
	extra := hujson.Extra("\n\t// scurgery:aws-router\n\t")
	if hasMarker(extra, "aws") {
		t.Error("hasMarker matched a namespace that is only a prefix of the marker")
	}
}

func TestKeyMarkerImpliesMarker(t *testing.T) {
	extra := hujson.Extra("\n\t// scurgery:aws-router owns-key\n\t")
	if !hasKeyMarker(extra, "aws-router") {
		t.Error("hasKeyMarker should match")
	}
	if !hasMarker(extra, "aws-router") {
		t.Error("a key marker is also a marker")
	}
}

func TestIndentOf(t *testing.T) {
	if got, want := indentOf(hujson.Extra("\n\t\t"), "\n\t"), "\t\t"; got != want {
		t.Errorf("indentOf = %q, want %q", got, want)
	}
	if got, want := indentOf(hujson.Extra(""), "  "), "  "; got != want {
		t.Errorf("indentOf fallback = %q, want %q", got, want)
	}
}

func TestMarkerExtra(t *testing.T) {
	got := string(markerExtra("// scurgery:x", "\t\t"))
	want := "\n\t\t// scurgery:x\n\t\t"
	if got != want {
		t.Errorf("markerExtra = %q, want %q", got, want)
	}
}

func TestValidateNamespace(t *testing.T) {
	for _, ok := range []string{"aws-router", "a", "aws_router2", "AWS-Router"} {
		if err := ValidateNamespace(ok); err != nil {
			t.Errorf("ValidateNamespace(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "has space", "has\nnewline", "has/slash", "owns-key"} {
		if err := ValidateNamespace(bad); err == nil {
			t.Errorf("ValidateNamespace(%q) = nil, want error", bad)
		}
	}
}
