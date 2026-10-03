package verify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCosignVerifier_InvalidReference(t *testing.T) {
	v := NewSignatureVerifier("")
	info, err := v.Verify(context.Background(), ":::invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Error == "" {
		t.Error("expected error for invalid reference")
	}
	if !strings.Contains(info.Error, "invalid image reference") {
		t.Errorf("expected 'invalid image reference' error, got: %s", info.Error)
	}
}

func TestCosignVerifier_WithKey_InvalidKeyPath(t *testing.T) {
	v := NewSignatureVerifier("/nonexistent/key.pub")
	info, err := v.Verify(context.Background(), "docker.io/library/nginx:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(info.Error, "reading public key") {
		t.Errorf("expected 'reading public key' error, got: %s", info.Error)
	}
	if info.Signed {
		t.Error("expected signed=false for missing key file")
	}
}

func TestCosignVerifier_WithKey_InvalidKeyContent(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.pub")
	if err := os.WriteFile(keyPath, []byte("not a pem key"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewSignatureVerifier(keyPath)
	info, err := v.Verify(context.Background(), "docker.io/library/nginx:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(info.Error, "parsing public key") {
		t.Errorf("expected 'parsing public key' error, got: %s", info.Error)
	}
}

func TestCosignVerifier_WithKey_ValidKeyLoading(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "cosign.pub")
	if err := os.WriteFile(keyPath, pemBlock, 0o644); err != nil {
		t.Fatal(err)
	}

	v := NewSignatureVerifier(keyPath)
	// This will fail at the registry verification step (no network / no matching
	// signature), but the key loading path is exercised completely.
	info, err := v.Verify(context.Background(), "docker.io/library/nginx:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The error should come from the verification step, not key loading.
	for _, prefix := range []string{"reading public key", "parsing public key", "loading verifier"} {
		if strings.HasPrefix(info.Error, prefix) {
			t.Errorf("key loading should succeed, but got error: %s", info.Error)
		}
	}

	// Verification fails (no registry access in tests), so we expect an error
	// from the verification step itself.
	if info.Error == "" {
		t.Error("expected verification error (no registry in test env)")
	}
	if strings.HasPrefix(info.Error, "verification failed") {
		// Correct: got past key loading, failed at registry verification.
		if info.Signed {
			t.Error("should not claim signed when verification failed with error")
		}
	}
}

func TestNewSignatureVerifier_NoKey(t *testing.T) {
	v := NewSignatureVerifier("")
	if v == nil {
		t.Fatal("expected non-nil verifier")
	}
	cv, ok := v.(*CosignVerifier)
	if !ok {
		t.Fatal("expected *CosignVerifier")
	}
	if cv.publicKey != "" {
		t.Error("expected empty publicKey")
	}
}

func TestNewSignatureVerifier_WithKey(t *testing.T) {
	v := NewSignatureVerifier("/some/key.pub")
	cv, ok := v.(*CosignVerifier)
	if !ok {
		t.Fatal("expected *CosignVerifier")
	}
	if cv.publicKey != "/some/key.pub" {
		t.Errorf("expected /some/key.pub, got %s", cv.publicKey)
	}
}
