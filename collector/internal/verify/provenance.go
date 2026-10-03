package verify

import (
	"context"
	"strings"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// ProvenanceChecker checks whether container images have SLSA provenance
// attestations attached via OCI referrers or cosign attestation tags.
type ProvenanceChecker interface {
	Check(ctx context.Context, imageRef string) (*report.ProvenanceInfo, error)
}

// SLSAProvenanceChecker checks for SLSA provenance using OCI referrers.
type SLSAProvenanceChecker struct{}

// NewProvenanceChecker creates a ProvenanceChecker.
func NewProvenanceChecker() ProvenanceChecker {
	return &SLSAProvenanceChecker{}
}

// Known SLSA predicate type prefixes.
var slsaPredicates = []string{
	"https://slsa.dev/provenance/",
	"https://in-toto.io/provenance/",
}

func (c *SLSAProvenanceChecker) Check(ctx context.Context, imageRef string) (*report.ProvenanceInfo, error) {
	manifests, err := referrerManifests(ctx, imageRef)
	if err != nil {
		return &report.ProvenanceInfo{}, nil
	}

	for _, m := range manifests {
		for _, pt := range predicateTypes(m) {
			if isSLSAPredicate(pt) {
				return &report.ProvenanceInfo{
					HasProvenance: true,
					PredicateType: pt,
				}, nil
			}
		}
	}

	// BuildKit stores SLSA provenance as an attestation manifest embedded in
	// the image index (unknown/unknown), not as a referrer, so check there too.
	for _, pt := range indexAttestationPredicateTypes(ctx, imageRef) {
		if isSLSAPredicate(pt) {
			return &report.ProvenanceInfo{
				HasProvenance: true,
				PredicateType: pt,
			}, nil
		}
	}

	return &report.ProvenanceInfo{HasProvenance: false}, nil
}

func isSLSAPredicate(s string) bool {
	for _, prefix := range slsaPredicates {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
