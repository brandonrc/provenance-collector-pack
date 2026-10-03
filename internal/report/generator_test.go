package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Mock implementations for testing

type mockDigestResolver struct {
	digests map[string]string
}

func (m *mockDigestResolver) Resolve(_ context.Context, ref string) (string, error) {
	if d, ok := m.digests[ref]; ok {
		return d, nil
	}
	return "", fmt.Errorf("unknown image: %s", ref)
}

type mockUpdateChecker struct {
	updates map[string]*UpdateInfo
}

func (m *mockUpdateChecker) Check(_ context.Context, ref string) (*UpdateInfo, error) {
	if u, ok := m.updates[ref]; ok {
		return u, nil
	}
	return &UpdateInfo{CurrentTag: "unknown"}, nil
}

type mockSigVerifier struct {
	results map[string]*SignatureInfo
}

func (m *mockSigVerifier) Verify(_ context.Context, ref string) (*SignatureInfo, error) {
	if r, ok := m.results[ref]; ok {
		return r, nil
	}
	return &SignatureInfo{Signed: false}, nil
}

type mockSBOMDiscoverer struct {
	results map[string]*SBOMInfo
}

func (m *mockSBOMDiscoverer) Discover(_ context.Context, ref string) (*SBOMInfo, error) {
	if r, ok := m.results[ref]; ok {
		return r, nil
	}
	return &SBOMInfo{HasSBOM: false}, nil
}

func TestGeneratorReport(t *testing.T) {
	gen := NewGenerator(
		GeneratorConfig{
			VerifySignatures: true,
			CheckUpdates:     true,
			ClusterName:      "test-cluster",
			Concurrency:      2,
		},
		&mockDigestResolver{
			digests: map[string]string{
				"nginx:1.27":              "sha256:abc123",
				"prom/prometheus:v2.50.0": "sha256:def456",
			},
		},
		&mockUpdateChecker{
			updates: map[string]*UpdateInfo{
				"nginx:1.27": {
					CurrentTag:      "1.27",
					LatestInMajor:   "1.27.3",
					UpdateAvailable: true,
				},
			},
		},
		&mockSigVerifier{
			results: map[string]*SignatureInfo{
				"nginx:1.27": {Signed: true, Verified: true},
			},
		},
		&mockSBOMDiscoverer{
			results: map[string]*SBOMInfo{
				"nginx:1.27": {HasSBOM: true, Format: "spdx"},
			},
		},
		nil, // provenanceChecker
	)

	images := []ImageInput{
		{Image: "nginx:1.27", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "nginx"},
		{Image: "prom/prometheus:v2.50.0", Namespace: "monitoring", WorkloadKind: "StatefulSet", WorkloadName: "prometheus"},
	}

	helmReleases := []HelmSource{
		{ReleaseName: "ingress", Namespace: "default", ChartName: "ingress-nginx", ChartVersion: "4.8.0", AppVersion: "1.9.4", Status: "deployed"},
	}

	report := gen.Generate(context.Background(), images, helmReleases, []string{"default", "monitoring"})

	// Check metadata
	if report.Metadata.ClusterName != "test-cluster" {
		t.Errorf("expected cluster name test-cluster, got %s", report.Metadata.ClusterName)
	}
	if report.Metadata.CollectorVersion != Version {
		t.Errorf("expected version %s, got %s", Version, report.Metadata.CollectorVersion)
	}

	// Check images
	if len(report.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(report.Images))
	}

	nginxRecord := report.Images[0]
	if nginxRecord.Digest != "sha256:abc123" {
		t.Errorf("expected digest sha256:abc123, got %s", nginxRecord.Digest)
	}
	if nginxRecord.Signature == nil || !nginxRecord.Signature.Signed {
		t.Error("expected nginx to be signed")
	}
	if nginxRecord.SBOM == nil || !nginxRecord.SBOM.HasSBOM {
		t.Error("expected nginx to have SBOM")
	}
	if nginxRecord.Update == nil || !nginxRecord.Update.UpdateAvailable {
		t.Error("expected nginx to have update available")
	}

	// Check helm
	if len(report.HelmReleases) != 1 {
		t.Fatalf("expected 1 helm release, got %d", len(report.HelmReleases))
	}
	if report.HelmReleases[0].ReleaseName != "ingress" {
		t.Errorf("expected release name ingress, got %s", report.HelmReleases[0].ReleaseName)
	}

	// Check summary
	if report.Summary.TotalImages != 2 {
		t.Errorf("expected totalImages=2, got %d", report.Summary.TotalImages)
	}
	if report.Summary.UniqueImages != 2 {
		t.Errorf("expected uniqueImages=2, got %d", report.Summary.UniqueImages)
	}
	if report.Summary.SignedImages != 1 {
		t.Errorf("expected signedImages=1, got %d", report.Summary.SignedImages)
	}
	if report.Summary.ImagesWithSBOM != 1 {
		t.Errorf("expected imagesWithSBOM=1, got %d", report.Summary.ImagesWithSBOM)
	}
	if report.Summary.ImagesWithUpdates != 1 {
		t.Errorf("expected imagesWithUpdates=1, got %d", report.Summary.ImagesWithUpdates)
	}
	if report.Summary.TotalHelmReleases != 1 {
		t.Errorf("expected totalHelmReleases=1, got %d", report.Summary.TotalHelmReleases)
	}
}

type failingUpdateChecker struct{}

func (failingUpdateChecker) Check(context.Context, string) (*UpdateInfo, error) {
	return nil, fmt.Errorf("listing tags: 429 Too Many Requests")
}

type mockProvenanceChecker struct{}

func (mockProvenanceChecker) Check(_ context.Context, ref string) (*ProvenanceInfo, error) {
	if ref == "signed:1" {
		return &ProvenanceInfo{HasProvenance: true, PredicateType: "https://slsa.dev/provenance/v1"}, nil
	}
	return &ProvenanceInfo{}, nil
}

func TestGeneratorSchemaVersionClockAndWarnings(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	gen := NewGenerator(
		GeneratorConfig{CheckUpdates: true, Now: func() time.Time { return fixed }},
		&mockDigestResolver{digests: map[string]string{"signed:1": "sha256:aa"}},
		failingUpdateChecker{},
		nil, nil,
		mockProvenanceChecker{},
	)
	images := []ImageInput{
		{Image: "signed:1", Namespace: "a"},
		{Image: "private.example.com/app:1", Namespace: "a", WorkloadKind: "ReplicaSet", WorkloadName: "x"},
		{Image: "private.example.com/app:1", Namespace: "b", WorkloadKind: "ReplicaSet", WorkloadName: "y"},
	}
	r := gen.Generate(context.Background(), images, nil, []string{"a", "b"}, "helm: zeta", "helm: alpha")

	if r.Metadata.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %q", r.Metadata.SchemaVersion)
	}
	if !r.Metadata.GeneratedAt.Equal(fixed) || r.Metadata.GeneratedAt.Location() != time.UTC {
		t.Errorf("generatedAt = %v, want %v in UTC", r.Metadata.GeneratedAt, fixed)
	}
	if r.Images[0].Provenance == nil || !r.Images[0].Provenance.HasProvenance || r.Summary.ImagesWithProvenance != 1 {
		t.Errorf("provenance not recorded: %+v / %+v", r.Images[0], r.Summary)
	}

	want := []string{
		"helm: alpha",
		"helm: zeta",
		"image private.example.com/app:1: digest not resolved: unknown image: private.example.com/app:1",
		"image private.example.com/app:1: update check failed: listing tags: 429 Too Many Requests",
		"image signed:1: update check failed: listing tags: 429 Too Many Requests",
	}
	if len(r.Warnings) != len(want) {
		t.Fatalf("warnings = %q", r.Warnings)
	}
	for i := range want {
		if r.Warnings[i] != want[i] {
			t.Errorf("warnings[%d] = %q, want %q", i, r.Warnings[i], want[i])
		}
	}
}

func TestGeneratorNoWarningsOmitted(t *testing.T) {
	gen := NewGenerator(GeneratorConfig{}, nil, nil, nil, nil, nil)
	r := gen.Generate(context.Background(), nil, nil, []string{})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"warnings"`) {
		t.Errorf("a clean report should omit warnings: %s", s)
	}
	if !strings.Contains(s, `"images":[]`) || !strings.Contains(s, `"schemaVersion":"`+SchemaVersion+`"`) {
		t.Errorf("images must be an empty array and schemaVersion set: %s", s)
	}
}
