package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that might interfere
	for _, key := range []string{
		"PROVENANCE_NAMESPACES", "PROVENANCE_EXCLUDE_NAMESPACES",
		"PROVENANCE_VERIFY_SIGNATURES", "PROVENANCE_HELM_ENABLED",
		"PROVENANCE_CHECK_UPDATES", "PROVENANCE_REPORT_OUTPUT",
		"PROVENANCE_REPORT_PATH", "PROVENANCE_REGISTRY_TIMEOUT",
		"PROVENANCE_REPORT_UPLOAD_URL", "PROVENANCE_REPORT_UPLOAD_TIMEOUT",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.VerifySignatures != true {
		t.Error("expected VerifySignatures=true by default")
	}
	if cfg.HelmEnabled != true {
		t.Error("expected HelmEnabled=true by default")
	}
	if cfg.CheckUpdates != true {
		t.Error("expected CheckUpdates=true by default")
	}
	if cfg.ReportOutput != "http" {
		t.Errorf("expected ReportOutput=http, got %s", cfg.ReportOutput)
	}
	if cfg.ReportUploadTimeout != 30*time.Second {
		t.Errorf("expected ReportUploadTimeout=30s, got %s", cfg.ReportUploadTimeout)
	}
	if cfg.ReportPath != "/reports" {
		t.Errorf("expected ReportPath=/reports, got %s", cfg.ReportPath)
	}
	if cfg.RegistryTimeout != 30*time.Second {
		t.Errorf("expected RegistryTimeout=30s, got %s", cfg.RegistryTimeout)
	}
	if len(cfg.Namespaces) != 0 {
		t.Errorf("expected empty Namespaces, got %v", cfg.Namespaces)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PROVENANCE_NAMESPACES", "default,kube-system")
	t.Setenv("PROVENANCE_EXCLUDE_NAMESPACES", "kube-public")
	t.Setenv("PROVENANCE_VERIFY_SIGNATURES", "false")
	t.Setenv("PROVENANCE_HELM_ENABLED", "false")
	t.Setenv("PROVENANCE_REPORT_OUTPUT", "configmap")
	t.Setenv("PROVENANCE_REGISTRY_TIMEOUT", "1m")
	t.Setenv("PROVENANCE_CLUSTER_NAME", "my-cluster")

	cfg := Load()

	if len(cfg.Namespaces) != 2 || cfg.Namespaces[0] != "default" || cfg.Namespaces[1] != "kube-system" {
		t.Errorf("expected [default kube-system], got %v", cfg.Namespaces)
	}
	if len(cfg.ExcludeNamespaces) != 1 || cfg.ExcludeNamespaces[0] != "kube-public" {
		t.Errorf("expected [kube-public], got %v", cfg.ExcludeNamespaces)
	}
	if cfg.VerifySignatures {
		t.Error("expected VerifySignatures=false")
	}
	if cfg.HelmEnabled {
		t.Error("expected HelmEnabled=false")
	}
	if cfg.ReportOutput != "configmap" {
		t.Errorf("expected ReportOutput=configmap, got %s", cfg.ReportOutput)
	}
	if cfg.RegistryTimeout != time.Minute {
		t.Errorf("expected RegistryTimeout=1m, got %s", cfg.RegistryTimeout)
	}
	if cfg.ClusterName != "my-cluster" {
		t.Errorf("expected ClusterName=my-cluster, got %s", cfg.ClusterName)
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
	}

	for _, tc := range tests {
		result := splitCSV(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("splitCSV(%q): got %v, want %v", tc.input, result, tc.expected)
			continue
		}
		for i := range result {
			if result[i] != tc.expected[i] {
				t.Errorf("splitCSV(%q)[%d]: got %q, want %q", tc.input, i, result[i], tc.expected[i])
			}
		}
	}
}
