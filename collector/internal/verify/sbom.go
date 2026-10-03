package verify

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// SBOMDiscoverer checks whether container images have attached SBOM attestations.
type SBOMDiscoverer interface {
	Discover(ctx context.Context, imageRef string) (*report.SBOMInfo, error)
}

// OCISBOMDiscoverer looks for SBOM attestations in OCI registries.
type OCISBOMDiscoverer struct{}

// NewSBOMDiscoverer creates an SBOMDiscoverer that checks OCI registries.
func NewSBOMDiscoverer() SBOMDiscoverer {
	return &OCISBOMDiscoverer{}
}

func (d *OCISBOMDiscoverer) Discover(ctx context.Context, imageRef string) (*report.SBOMInfo, error) {
	// Path 1: the OCI referrers fallback tag. Cosign/sigstore bundle-format
	// SBOM attestations land here as referring manifests, advertising their
	// format through the in-toto predicate type on the descriptor.
	if info := discoverFromReferrers(ctx, imageRef); info != nil {
		return info, nil
	}

	// Path 2: BuildKit attestation manifests embedded in the image index.
	// docker/build-push-action (sbom: true) and Docker Official Images store
	// the SBOM here, not in the referrers fallback tag, with the predicate type
	// on the attestation manifest's layers. This is the common case and the
	// referrers path above cannot see it. provenance.go checks the same place
	// so the two stay in lock-step.
	if info := discoverFromIndexAttestations(ctx, imageRef); info != nil {
		return info, nil
	}

	// Path 3: the legacy cosign attestation tag (sha256-<hex>.att) produced
	// by older `cosign attest` runs that predate the referrers/bundle format.
	if info := discoverFromCosignAtt(imageRef); info != nil {
		return info, nil
	}

	return &report.SBOMInfo{HasSBOM: false}, nil
}

// discoverFromReferrers looks for an SBOM attestation in the image's OCI
// referrers index. Returns nil when no SBOM-typed referrer is found, so the
// caller can fall through to the later paths.
func discoverFromReferrers(ctx context.Context, imageRef string) *report.SBOMInfo {
	manifests, err := referrerManifests(ctx, imageRef)
	if err != nil {
		return nil
	}
	for _, m := range manifests {
		for _, pt := range predicateTypes(m) {
			if format := sbomFormatFromPredicate(pt); format != "" {
				return &report.SBOMInfo{HasSBOM: true, Format: format}
			}
		}
	}
	return nil
}

// discoverFromIndexAttestations looks for an SBOM among the BuildKit
// attestation manifests embedded in the image index. Returns nil when no
// SBOM-typed predicate is found, so the caller can fall through to the legacy
// path.
func discoverFromIndexAttestations(ctx context.Context, imageRef string) *report.SBOMInfo {
	for _, pt := range indexAttestationPredicateTypes(ctx, imageRef) {
		if format := sbomFormatFromPredicate(pt); format != "" {
			return &report.SBOMInfo{HasSBOM: true, Format: format}
		}
	}
	return nil
}

// discoverFromCosignAtt looks for an SBOM in the legacy cosign attestation tag.
// Returns nil when nothing usable is found.
func discoverFromCosignAtt(imageRef string) *report.SBOMInfo {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil
	}

	se, err := ociremote.SignedEntity(ref)
	if err != nil {
		return nil
	}

	atts, err := se.Attestations()
	if err != nil {
		return nil
	}

	attList, err := atts.Get()
	if err != nil || len(attList) == 0 {
		return nil
	}

	for _, att := range attList {
		payload, err := att.Payload()
		if err != nil {
			continue
		}
		if format := detectSBOMFormat(payload); format != "" {
			return &report.SBOMInfo{HasSBOM: true, Format: format}
		}
	}

	return nil
}

// Known in-toto predicate types for SBOM formats.
const (
	predicateSPDX      = "https://spdx.dev/Document"
	predicateCycloneDX = "https://cyclonedx.org/bom"
)

// sbomFormatFromPredicate maps an in-toto predicate type to an SBOM format,
// returning "" when the predicate type is not an SBOM.
func sbomFormatFromPredicate(predicateType string) string {
	switch {
	case strings.HasPrefix(predicateType, predicateSPDX):
		return "spdx"
	case strings.HasPrefix(predicateType, predicateCycloneDX):
		return "cyclonedx"
	default:
		return ""
	}
}

// inTotoStatement represents the minimal structure of an in-toto attestation
// needed to extract the predicate type.
type inTotoStatement struct {
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

// detectSBOMFormat identifies the SBOM format from an attestation payload.
// It first tries to parse the JSON structure and check the in-toto predicateType,
// then falls back to content-based detection.
func detectSBOMFormat(payload []byte) string {
	var stmt inTotoStatement
	if err := json.Unmarshal(payload, &stmt); err == nil && stmt.PredicateType != "" {
		switch {
		case strings.HasPrefix(stmt.PredicateType, predicateSPDX):
			return "spdx"
		case strings.HasPrefix(stmt.PredicateType, predicateCycloneDX):
			return "cyclonedx"
		}

		// Check the predicate body for known SBOM markers.
		if len(stmt.Predicate) > 0 {
			if f := detectFromContent(string(stmt.Predicate)); f != "" {
				return f
			}
		}
	}

	// Fallback: scan the raw payload for known markers.
	return detectFromContent(string(payload))
}

func detectFromContent(s string) string {
	switch {
	case strings.Contains(s, "https://spdx.dev/Document") || strings.Contains(s, "SPDXRef-"):
		return "spdx"
	case strings.Contains(s, "CycloneDX") || strings.Contains(s, "cyclonedx"):
		return "cyclonedx"
	default:
		return ""
	}
}
