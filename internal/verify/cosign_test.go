package verify

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/cosign/v2/pkg/oci/mutate"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	"github.com/sigstore/cosign/v2/pkg/oci/static"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/sigstore/sigstore/pkg/signature/payload"

	"github.com/nebari-dev/provenance-collector/internal/registry"
)

// testRegistry is an in-memory OCI registry, optionally behind basic auth.
type testRegistry struct {
	host string
	auth authn.Authenticator // nil when anonymous
}

const (
	regUser = "collector"
	regPass = "s3cret"
)

func newTestRegistry(t *testing.T, requireAuth bool) *testRegistry {
	t.Helper()
	h := ggcrregistry.New()
	tr := &testRegistry{}
	if requireAuth {
		inner := h
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u, p, ok := r.BasicAuth(); !ok || u != regUser || p != regPass {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			inner.ServeHTTP(w, r)
		})
		tr.auth = &authn.Basic{Username: regUser, Password: regPass}
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tr.host = strings.TrimPrefix(srv.URL, "http://")
	return tr
}

func (r *testRegistry) remoteOpts() []remote.Option {
	if r.auth == nil {
		return nil
	}
	return []remote.Option{remote.WithAuth(r.auth)}
}

// client returns a registry.Client that authenticates to r through a Docker
// config file, i.e. via PROVENANCE_REGISTRY_AUTH.
func (r *testRegistry) client(t *testing.T) *registry.Client {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir()) // no ambient credentials
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := `{"auths":{"` + r.host + `":{"auth":"` + base64.StdEncoding.EncodeToString([]byte(regUser+":"+regPass)) + `"}}}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := registry.NewClient(registry.Options{AuthFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pushImage pushes a random image and returns its tag reference.
func (r *testRegistry) pushImage(t *testing.T, repoTag string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(r.host + "/" + repoTag)
	if err != nil {
		t.Fatal(err)
	}
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img, r.remoteOpts()...); err != nil {
		t.Fatalf("push: %v", err)
	}
	return ref
}

// sign attaches a cosign signature for ref made with key, exactly as
// `cosign sign --key --tlog-upload=false` lays it out (sha256-<hex>.sig).
func (r *testRegistry) sign(t *testing.T, ref name.Reference, key *ecdsa.PrivateKey) {
	t.Helper()
	opts := []ociremote.Option{ociremote.WithRemoteOptions(r.remoteOpts()...)}
	se, err := ociremote.SignedEntity(ref, opts...)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := se.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pl, err := payload.Cosign{Image: ref.Context().Digest(digest.String())}.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signature.LoadECDSASignerVerifier(key, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signer.SignMessage(bytes.NewReader(pl))
	if err != nil {
		t.Fatal(err)
	}
	ociSig, err := static.NewSignature(pl, base64.StdEncoding.EncodeToString(sig))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := mutate.AttachSignatureToEntity(se, ociSig)
	if err != nil {
		t.Fatal(err)
	}
	if err := ociremote.WriteSignatures(ref.Context(), signed, opts...); err != nil {
		t.Fatalf("writing signature: %v", err)
	}
}

func newKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := cryptoutils.MarshalPublicKeyToPEM(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cosign.pub")
	if err := os.WriteFile(path, pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return key, path
}

func TestCosignVerifier_ValidSignatureVerifies(t *testing.T) {
	reg := newTestRegistry(t, false)
	ref := reg.pushImage(t, "signed/app:1.0.0")
	key, pubPath := newKey(t)
	reg.sign(t, ref, key)

	info, err := NewSignatureVerifier(pubPath, nil).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Signed || !info.Verified || info.Error != "" {
		t.Errorf("want signed and verified, got %+v", info)
	}
}

func TestCosignVerifier_WrongKeyFailsVerification(t *testing.T) {
	reg := newTestRegistry(t, false)
	ref := reg.pushImage(t, "signed/app:1.0.0")
	key, _ := newKey(t)
	reg.sign(t, ref, key)
	_, otherPub := newKey(t)

	info, err := NewSignatureVerifier(otherPub, nil).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Verified || info.Signed {
		t.Errorf("a signature from another key must not verify: %+v", info)
	}
	if !strings.HasPrefix(info.Error, "verification failed") {
		t.Errorf("expected a verification error, got %q", info.Error)
	}
}

func TestCosignVerifier_UnsignedImageWithKey(t *testing.T) {
	reg := newTestRegistry(t, false)
	ref := reg.pushImage(t, "plain/app:1.0.0")
	_, pubPath := newKey(t)

	info, err := NewSignatureVerifier(pubPath, nil).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Signed || info.Verified || !strings.HasPrefix(info.Error, "verification failed") {
		t.Errorf("unsigned image: got %+v", info)
	}
}

func TestCosignVerifier_ExistenceCheck(t *testing.T) {
	reg := newTestRegistry(t, false)
	signedRef := reg.pushImage(t, "signed/app:1.0.0")
	key, _ := newKey(t)
	reg.sign(t, signedRef, key)
	plainRef := reg.pushImage(t, "plain/app:1.0.0")

	v := NewSignatureVerifier("", nil)
	info, err := v.Verify(context.Background(), signedRef.String())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Signed || info.Verified {
		t.Errorf("signed image without a key: want signed, not verified; got %+v", info)
	}

	info, err = v.Verify(context.Background(), plainRef.String())
	if err != nil {
		t.Fatal(err)
	}
	if info.Signed || info.Verified {
		t.Errorf("unsigned image: got %+v", info)
	}
}

// The verifier authenticates with PROVENANCE_REGISTRY_AUTH credentials, for
// both the existence check and key verification.
func TestCosignVerifier_PrivateRegistry(t *testing.T) {
	reg := newTestRegistry(t, true)
	ref := reg.pushImage(t, "private/app:1.0.0")
	key, pubPath := newKey(t)
	reg.sign(t, ref, key)
	client := reg.client(t)

	info, err := NewSignatureVerifier(pubPath, client).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Signed || !info.Verified {
		t.Errorf("key verification with registry auth: got %+v", info)
	}

	info, err = NewSignatureVerifier("", client).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Signed {
		t.Errorf("existence check with registry auth: got %+v", info)
	}

	info, err = NewSignatureVerifier("", nil).Verify(context.Background(), ref.String())
	if err != nil {
		t.Fatal(err)
	}
	if info.Signed || info.Error == "" {
		t.Errorf("anonymous check against a private registry should report an error, got %+v", info)
	}
}

func TestCosignVerifier_InvalidReference(t *testing.T) {
	v := NewSignatureVerifier("", nil)
	info, err := v.Verify(context.Background(), ":::invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(info.Error, "invalid image reference") {
		t.Errorf("expected 'invalid image reference' error, got: %s", info.Error)
	}
}

// Key problems are reported before any registry is contacted (localhost:1 is
// never dialled).
func TestCosignVerifier_KeyErrors(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "bad.pub")
	if err := os.WriteFile(notPEM, []byte("not a pem key"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		filepath.Join(dir, "missing.pub"): "reading public key",
		notPEM:                            "parsing public key",
	}
	for keyPath, wantPrefix := range cases {
		info, err := NewSignatureVerifier(keyPath, nil).Verify(context.Background(), "localhost:1/app:1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(info.Error, wantPrefix) || info.Signed {
			t.Errorf("%s: want error prefix %q, got %+v", keyPath, wantPrefix, info)
		}
	}
}

func TestNewSignatureVerifier(t *testing.T) {
	cv, ok := NewSignatureVerifier("/some/key.pub", nil).(*CosignVerifier)
	if !ok {
		t.Fatal("expected *CosignVerifier")
	}
	if cv.publicKey != "/some/key.pub" {
		t.Errorf("expected /some/key.pub, got %s", cv.publicKey)
	}
}

// SBOM and provenance lookups (referrers + index attestations) also go
// through the registry client's credentials.
func TestAttestationLookupsUseRegistryAuth(t *testing.T) {
	reg := newTestRegistry(t, true)
	ref := pushBuildKitIndexTo(t, reg.host+"/private/buildkit:latest", reg.remoteOpts(),
		"https://slsa.dev/provenance/v1", "https://spdx.dev/Document")
	client := reg.client(t)

	prov, err := NewProvenanceChecker(client).Check(context.Background(), ref)
	if err != nil || !prov.HasProvenance {
		t.Errorf("provenance with registry auth: %+v, %v", prov, err)
	}
	sbom, err := NewSBOMDiscoverer(client).Discover(context.Background(), ref)
	if err != nil || !sbom.HasSBOM || sbom.Format != "spdx" {
		t.Errorf("SBOM with registry auth: %+v, %v", sbom, err)
	}

	prov, _ = NewProvenanceChecker(nil).Check(context.Background(), ref)
	if prov.HasProvenance {
		t.Error("anonymous lookup must not see a private registry's attestations")
	}
}
