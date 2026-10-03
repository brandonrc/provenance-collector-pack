package report

import "time"

// ProvenanceReport is the top-level output artifact produced by the collector.
type ProvenanceReport struct {
	Metadata     ReportMetadata `json:"metadata"`
	Images       []ImageRecord  `json:"images"`
	HelmReleases []HelmRecord   `json:"helmReleases,omitempty"`
	Summary      ReportSummary  `json:"summary"`
}

// ReportMetadata describes when and where the report was generated.
type ReportMetadata struct {
	GeneratedAt       time.Time `json:"generatedAt"`
	CollectorVersion  string    `json:"collectorVersion"`
	ClusterName       string    `json:"clusterName,omitempty"`
	NamespacesScanned []string  `json:"namespacesScanned"`
}

// ImageRecord captures provenance data for a single container image.
type ImageRecord struct {
	Image      string          `json:"image"`
	Digest     string          `json:"digest,omitempty"`
	Namespace  string          `json:"namespace"`
	Workload   WorkloadRef     `json:"workload"`
	Signature  *SignatureInfo  `json:"signature,omitempty"`
	SBOM       *SBOMInfo       `json:"sbom,omitempty"`
	Provenance *ProvenanceInfo `json:"provenance,omitempty"`
	Update     *UpdateInfo     `json:"update,omitempty"`
}

// WorkloadRef identifies the Kubernetes workload that owns a container.
type WorkloadRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SignatureInfo records cosign signature verification results.
type SignatureInfo struct {
	Signed   bool   `json:"signed"`
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
}

// SBOMInfo records whether an SBOM attestation is attached to an image.
type SBOMInfo struct {
	HasSBOM bool   `json:"hasSBOM"`
	Format  string `json:"format,omitempty"`
}

// ProvenanceInfo records SLSA provenance attestation results.
type ProvenanceInfo struct {
	HasProvenance bool   `json:"hasProvenance"`
	PredicateType string `json:"predicateType,omitempty"`
}

// UpdateInfo records available version updates for an image or chart.
type UpdateInfo struct {
	CurrentTag      string `json:"currentTag"`
	LatestInMajor   string `json:"latestInMajor,omitempty"`
	NewestAvailable string `json:"newestAvailable,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// HelmRecord captures provenance data for a deployed Helm release.
type HelmRecord struct {
	ReleaseName string      `json:"releaseName"`
	Namespace   string      `json:"namespace"`
	Chart       string      `json:"chart"`
	Version     string      `json:"version"`
	AppVersion  string      `json:"appVersion"`
	Status      string      `json:"status"`
	Update      *UpdateInfo `json:"update,omitempty"`
}

// ReportSummary provides aggregate counts for quick compliance review.
type ReportSummary struct {
	TotalImages             int `json:"totalImages"`
	UniqueImages            int `json:"uniqueImages"`
	SignedImages            int `json:"signedImages"`
	VerifiedImages          int `json:"verifiedImages"`
	ImagesWithSBOM          int `json:"imagesWithSBOM"`
	ImagesWithProvenance    int `json:"imagesWithProvenance"`
	ImagesWithUpdates       int `json:"imagesWithUpdates"`
	TotalHelmReleases       int `json:"totalHelmReleases"`
	HelmReleasesWithUpdates int `json:"helmReleasesWithUpdates"`
}
