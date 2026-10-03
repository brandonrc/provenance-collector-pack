package verify

import (
	"context"
	"crypto"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// SignatureVerifier checks whether container images have cosign signatures.
type SignatureVerifier interface {
	Verify(ctx context.Context, imageRef string) (*report.SignatureInfo, error)
}

// CosignVerifier uses the cosign library to verify image signatures.
type CosignVerifier struct {
	publicKey string
}

// NewSignatureVerifier creates a SignatureVerifier.
// If publicKey is empty, it only checks for signature existence without
// verifying the trust chain.
func NewSignatureVerifier(publicKey string) SignatureVerifier {
	return &CosignVerifier{publicKey: publicKey}
}

func (v *CosignVerifier) Verify(ctx context.Context, imageRef string) (*report.SignatureInfo, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("invalid image reference: %v", err),
		}, nil
	}

	// If a public key is provided, use full cosign verification.
	if v.publicKey != "" {
		return v.verifyWithKey(ctx, ref)
	}

	// Otherwise, just check for signature existence.
	return v.checkExistence(ref)
}

// verifyWithKey performs full key-based cosign signature verification.
func (v *CosignVerifier) verifyWithKey(ctx context.Context, ref name.Reference) (*report.SignatureInfo, error) {
	pemBytes, err := os.ReadFile(v.publicKey)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("reading public key: %v", err),
		}, nil
	}

	pubKey, err := cryptoutils.UnmarshalPEMToPublicKey(pemBytes)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("parsing public key: %v", err),
		}, nil
	}

	verifier, err := signature.LoadVerifier(pubKey, crypto.SHA256)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("loading verifier: %v", err),
		}, nil
	}

	opts := &cosign.CheckOpts{
		SigVerifier: verifier,
		IgnoreTlog:  true,
		IgnoreSCT:   true,
	}

	sigs, _, err := cosign.VerifyImageSignatures(ctx, ref, opts)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("verification failed: %v", err),
		}, nil
	}

	if len(sigs) == 0 {
		return &report.SignatureInfo{Signed: false}, nil
	}

	return &report.SignatureInfo{Signed: true, Verified: true}, nil
}

// checkExistence checks whether any cosign signature exists for the image
// without verifying against a specific key.
func (v *CosignVerifier) checkExistence(ref name.Reference) (*report.SignatureInfo, error) {
	se, err := ociremote.SignedEntity(ref)
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("fetching signed entity: %v", err),
		}, nil
	}

	sigs, err := se.Signatures()
	if err != nil {
		return &report.SignatureInfo{
			Error: fmt.Sprintf("checking signatures: %v", err),
		}, nil
	}

	sigList, err := sigs.Get()
	if err != nil || len(sigList) == 0 {
		return &report.SignatureInfo{Signed: false}, nil
	}

	return &report.SignatureInfo{Signed: true}, nil
}
