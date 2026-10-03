package verify

import (
	"context"
	"testing"
)

func TestIsSLSAPredicate(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://slsa.dev/provenance/v1", true},
		{"https://slsa.dev/provenance/v0.2", true},
		{"https://in-toto.io/provenance/v1", true},
		{"https://in-toto.io/provenance/v0.1", true},
		{"https://spdx.dev/Document", false},
		{"https://cyclonedx.org/bom", false},
		{"https://slsa.dev/verification_summary/v1", false},
		{"", false},
		{"random string", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := isSLSAPredicate(tc.input)
			if got != tc.want {
				t.Errorf("isSLSAPredicate(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewProvenanceChecker(t *testing.T) {
	c := NewProvenanceChecker()
	if c == nil {
		t.Fatal("expected non-nil checker")
	}
	if _, ok := c.(*SLSAProvenanceChecker); !ok {
		t.Error("expected *SLSAProvenanceChecker")
	}
}

func TestSLSAProvenanceChecker_InvalidRef(t *testing.T) {
	c := NewProvenanceChecker()
	info, err := c.Check(context.Background(), ":::invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.HasProvenance {
		t.Error("expected no provenance for invalid ref")
	}
}

func TestSLSAProvenanceChecker_UnreachableImage(t *testing.T) {
	c := NewProvenanceChecker()
	// Non-existent image — registry call will fail, should return empty (not error)
	info, err := c.Check(context.Background(), "localhost:1/nonexistent:v0.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.HasProvenance {
		t.Error("expected no provenance for unreachable image")
	}
}
