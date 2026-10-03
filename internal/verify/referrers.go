package verify

import (
	"context"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/nebari-dev/provenance-collector/internal/registry"
)

// Annotation keys that carry the in-toto predicate type of a referring
// attestation manifest. Sigstore bundles use the first; buildx / BuildKit
// attestation manifests use the second.
const (
	annoSigstorePredicateType = "dev.sigstore.bundle.predicateType"
	annoInTotoPredicateType   = "in-toto.io/predicate-type"
)

// Annotation key and value that BuildKit uses to mark the attestation
// manifests it embeds in a multi-arch image index. Such entries carry platform
// unknown/unknown and reference the image manifest they attest to.
const (
	annoDockerReferenceType = "vnd.docker.reference.type"
	attestationManifestType = "attestation-manifest"
)

// referrerManifests resolves imageRef, then reads the OCI referrers index via
// the <algo>-<hex> fallback tag derived from the image digest, and returns the
// referring manifest descriptors. Cosign/sigstore bundle attestations land
// here. BuildKit's in-index attestation manifests do NOT — those are walked
// separately by indexAttestationPredicateTypes.
//
// Every failure (bad ref, unreachable registry, no referrers tag) returns an
// empty slice and a nil error: a missing referrers index is the common case,
// not an error worth surfacing to the caller.
func referrerManifests(ctx context.Context, client *registry.Client, imageRef string) ([]v1.Descriptor, error) {
	ref, err := client.ParseReference(imageRef)
	if err != nil {
		return nil, nil
	}

	opts := client.RemoteOptions(ctx)
	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return nil, nil
	}

	referrersTag := strings.Replace(desc.Digest.String(), ":", "-", 1)
	referrersRef := ref.Context().Tag(referrersTag)

	idx, err := remote.Index(referrersRef, opts...)
	if err != nil {
		return nil, nil
	}

	manifest, err := idx.IndexManifest()
	if err != nil || manifest == nil {
		return nil, nil
	}

	return manifest.Manifests, nil
}

// predicateTypes returns the candidate in-toto predicate types advertised by a
// referring manifest descriptor: the dedicated sigstore/in-toto annotations
// first, then every other annotation value as a fallback (some producers stash
// the predicate type under non-standard keys).
func predicateTypes(d v1.Descriptor) []string {
	var out []string
	if pt := d.Annotations[annoSigstorePredicateType]; pt != "" {
		out = append(out, pt)
	}
	if pt := d.Annotations[annoInTotoPredicateType]; pt != "" {
		out = append(out, pt)
	}
	for k, v := range d.Annotations {
		if k == annoSigstorePredicateType || k == annoInTotoPredicateType {
			continue
		}
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// indexAttestationPredicateTypes returns the in-toto predicate types attached
// to imageRef via BuildKit attestation manifests embedded in the image index.
//
// docker/build-push-action (sbom: true / provenance: true) and Docker Official
// Images store SBOM and SLSA attestations as extra manifests inside the
// multi-arch image index, marked with platform unknown/unknown and the
// annotation vnd.docker.reference.type=attestation-manifest. The predicate type
// lives on the attestation manifest's *layer* descriptors, so we fetch each
// attestation manifest and read its layer annotations. These attestations never
// appear in the <algo>-<hex> referrers fallback tag, so referrerManifests alone
// misses them.
//
// Every failure returns an empty slice: a single-arch image (no index) or an
// image without attestations is the common case, not an error worth surfacing.
func indexAttestationPredicateTypes(ctx context.Context, client *registry.Client, imageRef string) []string {
	ref, err := client.ParseReference(imageRef)
	if err != nil {
		return nil
	}

	opts := client.RemoteOptions(ctx)
	desc, err := remote.Get(ref, opts...)
	if err != nil || !desc.MediaType.IsIndex() {
		return nil
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil
	}
	manifest, err := idx.IndexManifest()
	if err != nil || manifest == nil {
		return nil
	}

	var out []string
	for _, m := range manifest.Manifests {
		if m.Annotations[annoDockerReferenceType] != attestationManifestType {
			continue
		}
		attRef := ref.Context().Digest(m.Digest.String())
		img, err := remote.Image(attRef, opts...)
		if err != nil {
			continue
		}
		attManifest, err := img.Manifest()
		if err != nil || attManifest == nil {
			continue
		}
		for _, layer := range attManifest.Layers {
			if pt := layer.Annotations[annoInTotoPredicateType]; pt != "" {
				out = append(out, pt)
			}
		}
	}
	return out
}
