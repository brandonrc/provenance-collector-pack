package report

import "time"

// SchemaVersion is the semantic version of the report document format, carried
// in metadata.schemaVersion. Consumers should accept any report with the same
// major version and ignore fields they do not know.
//
//   - MAJOR: a field is removed, renamed or changes type or meaning.
//   - MINOR: a field is added.
//   - PATCH: documentation or schema-only fixes; the JSON is unchanged.
//
// Reports written before this field existed are 1.0.0. 1.1.0 added
// metadata.schemaVersion and warnings. The JSON Schema for this version lives
// in schema/report.schema.json (regenerate with `go run ./hack/genschema`).
const SchemaVersion = "1.1.0"

// ProvenanceReport is the top-level output artifact produced by the collector.
type ProvenanceReport struct {
	Metadata     ReportMetadata `json:"metadata"`
	Images       []ImageRecord  `json:"images"`
	HelmReleases []HelmRecord   `json:"helmReleases,omitempty"`
	Summary      ReportSummary  `json:"summary"`
	// Warnings lists problems that made the report incomplete without failing
	// the run, e.g. Helm releases that could not be listed or images whose
	// digest could not be resolved. Empty or absent means nothing was skipped.
	Warnings []string `json:"warnings,omitempty"`
}

// ReportMetadata describes when and where the report was generated.
type ReportMetadata struct {
	// SchemaVersion is the report format version (see report.SchemaVersion).
	SchemaVersion string `json:"schemaVersion"`
	// GeneratedAt is when the collection ran (UTC, RFC 3339).
	GeneratedAt time.Time `json:"generatedAt"`
	// CollectorVersion is the collector build that wrote the report
	// (informational; use SchemaVersion for compatibility decisions).
	CollectorVersion string `json:"collectorVersion"`
	// ClusterName is PROVENANCE_CLUSTER_NAME, when set.
	ClusterName string `json:"clusterName,omitempty"`
	// NamespacesScanned lists the namespaces in which images were found, sorted.
	NamespacesScanned []string `json:"namespacesScanned"`
}

// ImageRecord captures provenance data for a single container image.
type ImageRecord struct {
	// Image is the reference from the pod spec, as written there.
	Image string `json:"image"`
	// Digest is the registry digest of Image. Absent when it could not be
	// resolved; a matching entry in the report's warnings says why.
	Digest    string      `json:"digest,omitempty"`
	Namespace string      `json:"namespace"`
	Workload  WorkloadRef `json:"workload"`
	// Signature is absent when signature checks are disabled.
	Signature *SignatureInfo `json:"signature,omitempty"`
	// SBOM is present only when an SBOM was found.
	SBOM *SBOMInfo `json:"sbom,omitempty"`
	// Provenance is present only when SLSA provenance was found.
	Provenance *ProvenanceInfo `json:"provenance,omitempty"`
	// Update is present only when a newer version meets the update level.
	Update *UpdateInfo `json:"update,omitempty"`
}

// WorkloadRef identifies the Kubernetes workload that owns a container.
type WorkloadRef struct {
	// Kind is the pod's controlling owner kind (ReplicaSet, StatefulSet,
	// DaemonSet, Job, ...) or "Pod" for a bare pod.
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SignatureInfo records cosign signature verification results.
type SignatureInfo struct {
	// Signed is true when a cosign signature exists for the image.
	Signed bool `json:"signed"`
	// Verified is true when the signature verified against the configured
	// public key. Always false when no key is configured.
	Verified bool `json:"verified"`
	// Error explains why the check could not complete (unreachable registry,
	// bad key, failed verification).
	Error string `json:"error,omitempty"`
}

// SBOMInfo records whether an SBOM attestation is attached to an image.
type SBOMInfo struct {
	HasSBOM bool `json:"hasSBOM"`
	// Format is "spdx" or "cyclonedx" when known.
	Format string `json:"format,omitempty"`
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
