package verify

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const inTotoLayerMediaType = types.MediaType("application/vnd.in-toto+json")

// pushBuildKitIndex publishes a multi-arch index that mirrors the layout
// docker buildx / docker/build-push-action produces: a normal image manifest
// plus an unknown/unknown attestation manifest (vnd.docker.reference.type=
// attestation-manifest) whose layers carry the given in-toto predicate types.
// It returns the image reference string.
func pushBuildKitIndex(t *testing.T, predicateTypes ...string) string {
	t.Helper()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	refStr := host + "/test/buildkit:latest"

	ref, err := name.ParseReference(refStr)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	// A normal platform image.
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	// The attestation manifest: an image whose layers advertise predicate types.
	att := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	for _, pt := range predicateTypes {
		layer := static.NewLayer([]byte(`{"predicate":"test"}`), inTotoLayerMediaType)
		att, err = mutate.Append(att, mutate.Addendum{
			Layer:       layer,
			Annotations: map[string]string{annoInTotoPredicateType: pt},
		})
		if err != nil {
			t.Fatalf("append attestation layer: %v", err)
		}
	}

	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: att,
			Descriptor: v1.Descriptor{
				Platform:    &v1.Platform{OS: "unknown", Architecture: "unknown"},
				Annotations: map[string]string{annoDockerReferenceType: attestationManifestType},
			},
		},
	)

	if err := remote.WriteIndex(ref, idx); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return refStr
}

func TestIndexAttestationPredicateTypes_BuildKit(t *testing.T) {
	ref := pushBuildKitIndex(t, predicateSPDX, "https://slsa.dev/provenance/v0.2")

	got := indexAttestationPredicateTypes(context.Background(), ref)
	for _, want := range []string{predicateSPDX, "https://slsa.dev/provenance/v0.2"} {
		if !slices.Contains(got, want) {
			t.Errorf("indexAttestationPredicateTypes() = %v, want to contain %q", got, want)
		}
	}
}

func TestOCISBOMDiscoverer_BuildKitIndexAttestation(t *testing.T) {
	// SBOM stored only as a BuildKit in-index attestation manifest — the case
	// the referrers fallback tag cannot see. This is the regression target.
	ref := pushBuildKitIndex(t, predicateSPDX)

	info, err := NewSBOMDiscoverer().Discover(context.Background(), ref)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info == nil || !info.HasSBOM {
		t.Fatalf("expected HasSBOM=true for BuildKit SPDX attestation, got %+v", info)
	}
	if info.Format != "spdx" {
		t.Errorf("expected format spdx, got %q", info.Format)
	}
}

func TestSLSAProvenanceChecker_BuildKitIndexAttestation(t *testing.T) {
	ref := pushBuildKitIndex(t, "https://slsa.dev/provenance/v0.2")

	info, err := NewProvenanceChecker().Check(context.Background(), ref)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info == nil || !info.HasProvenance {
		t.Fatalf("expected HasProvenance=true for BuildKit SLSA attestation, got %+v", info)
	}
	if info.PredicateType != "https://slsa.dev/provenance/v0.2" {
		t.Errorf("unexpected predicate type %q", info.PredicateType)
	}
}

func TestOCISBOMDiscoverer_BuildKitIndexNoSBOM(t *testing.T) {
	// Provenance-only attestation (the dex / build-push-action default): no
	// SBOM layer, so detection must report HasSBOM=false rather than a false
	// positive.
	ref := pushBuildKitIndex(t, "https://slsa.dev/provenance/v1")

	info, err := NewSBOMDiscoverer().Discover(context.Background(), ref)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info == nil || info.HasSBOM {
		t.Fatalf("expected HasSBOM=false for provenance-only image, got %+v", info)
	}
}
