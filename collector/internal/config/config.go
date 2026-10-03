package config

import (
	"os"
	"strings"
	"time"
)

// Config holds all configuration for the provenance collector.
type Config struct {
	// Namespaces to scan. Empty means all namespaces.
	Namespaces []string
	// Namespaces to exclude from scanning.
	ExcludeNamespaces []string
	// Path to Docker config.json for registry authentication.
	RegistryAuth string
	// Whether to verify cosign signatures on images.
	VerifySignatures bool
	// Path or KMS URI for cosign public key (empty = keyless/existence check only).
	CosignPublicKey string
	// Whether to discover Helm releases.
	HelmEnabled bool
	// Whether to check for available updates.
	CheckUpdates bool
	// Whether to skip pre-release versions (alpha, beta, rc) when checking updates.
	SkipPrerelease bool
	// Minimum version bump level to flag as an update: "patch", "minor", or "major".
	UpdateLevel string
	// Whether to check for SBOM attestations on images.
	CheckSBOM bool
	// Whether to check for SLSA provenance attestations on images.
	CheckProvenance bool
	// Report output type: "http" (POST to dashboard), "pvc" (write to local
	// filesystem on a shared PVC), or "configmap".
	ReportOutput string
	// File path for PVC-based report output.
	ReportPath string
	// How long to keep old reports. Negative means keep forever.
	// In http mode this is consumed by the dashboard, not the collector.
	ReportRetention time.Duration
	// ConfigMap name for configmap-based report output.
	ReportConfigMap string
	// Namespace for ConfigMap report output.
	ReportConfigMapNamespace string
	// Upload URL used in http mode (full URL including path, e.g.
	// http://provenance-web-internal.namespace.svc:8081/internal/reports).
	ReportUploadURL string
	// Timeout for the upload request in http mode.
	ReportUploadTimeout time.Duration
	// Timeout for registry operations.
	RegistryTimeout time.Duration
	// Path to kubeconfig (empty = in-cluster).
	Kubeconfig string
	// Cluster name for report metadata.
	ClusterName string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	c := &Config{
		Namespaces:               splitCSV(os.Getenv("PROVENANCE_NAMESPACES")),
		ExcludeNamespaces:        splitCSV(os.Getenv("PROVENANCE_EXCLUDE_NAMESPACES")),
		RegistryAuth:             os.Getenv("PROVENANCE_REGISTRY_AUTH"),
		VerifySignatures:         envBool("PROVENANCE_VERIFY_SIGNATURES", true),
		CosignPublicKey:          os.Getenv("PROVENANCE_COSIGN_PUBLIC_KEY"),
		HelmEnabled:              envBool("PROVENANCE_HELM_ENABLED", true),
		CheckUpdates:             envBool("PROVENANCE_CHECK_UPDATES", true),
		SkipPrerelease:           envBool("PROVENANCE_SKIP_PRERELEASE", true),
		UpdateLevel:              envDefault("PROVENANCE_UPDATE_LEVEL", "patch"),
		CheckSBOM:                envBool("PROVENANCE_CHECK_SBOM", true),
		CheckProvenance:          envBool("PROVENANCE_CHECK_PROVENANCE", true),
		ReportOutput:             envDefault("PROVENANCE_REPORT_OUTPUT", "http"),
		ReportPath:               envDefault("PROVENANCE_REPORT_PATH", "/reports"),
		ReportRetention:          envDuration("PROVENANCE_REPORT_RETENTION", 7*24*time.Hour),
		ReportConfigMap:          envDefault("PROVENANCE_REPORT_CONFIGMAP", "provenance-report"),
		ReportConfigMapNamespace: envDefault("PROVENANCE_REPORT_CONFIGMAP_NAMESPACE", "default"),
		ReportUploadURL:          os.Getenv("PROVENANCE_REPORT_UPLOAD_URL"),
		ReportUploadTimeout:      envDuration("PROVENANCE_REPORT_UPLOAD_TIMEOUT", 30*time.Second),
		RegistryTimeout:          envDuration("PROVENANCE_REGISTRY_TIMEOUT", 30*time.Second),
		Kubeconfig:               os.Getenv("KUBECONFIG"),
		ClusterName:              os.Getenv("PROVENANCE_CLUSTER_NAME"),
	}
	return c
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "1" || strings.EqualFold(v, "true")
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if v == "-1" {
		return -1
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
