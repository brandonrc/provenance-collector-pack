package verify

import (
	"context"
	"slices"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestPredicateTypes(t *testing.T) {
	tests := []struct {
		name string
		desc v1.Descriptor
		want []string // membership check, order-independent
	}{
		{
			name: "sigstore bundle annotation",
			desc: v1.Descriptor{Annotations: map[string]string{
				annoSigstorePredicateType: "https://slsa.dev/provenance/v1",
			}},
			want: []string{"https://slsa.dev/provenance/v1"},
		},
		{
			name: "buildx in-toto annotation",
			desc: v1.Descriptor{Annotations: map[string]string{
				annoInTotoPredicateType: "https://spdx.dev/Document",
			}},
			want: []string{"https://spdx.dev/Document"},
		},
		{
			name: "non-standard annotation falls through",
			desc: v1.Descriptor{Annotations: map[string]string{
				"org.example.predicate": "https://cyclonedx.org/bom",
			}},
			want: []string{"https://cyclonedx.org/bom"},
		},
		{
			name: "no annotations",
			desc: v1.Descriptor{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := predicateTypes(tc.desc)
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Errorf("predicateTypes() = %v, want to contain %q", got, w)
				}
			}
			if len(tc.want) == 0 && len(got) != 0 {
				t.Errorf("predicateTypes() = %v, want empty", got)
			}
		})
	}
}

func TestReferrerManifests_InvalidRef(t *testing.T) {
	got, err := referrerManifests(context.Background(), ":::invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no manifests for invalid ref, got %d", len(got))
	}
}
