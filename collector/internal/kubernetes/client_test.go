package kubernetes

import (
	"os"
	"path/filepath"
	"testing"
)

// validKubeconfig is a minimal kubeconfig for testing. Kept at package level
// to avoid Go source indentation (tabs) leaking into the YAML content.
const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: fake-token
`

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewClient_InvalidKubeconfig(t *testing.T) {
	_, err := NewClient("/nonexistent/kubeconfig")
	if err == nil {
		t.Error("expected error for nonexistent kubeconfig")
	}
}

func TestNewClient_MalformedKubeconfig(t *testing.T) {
	path := writeKubeconfig(t, "not valid yaml {{{")
	_, err := NewClient(path)
	if err == nil {
		t.Error("expected error for malformed kubeconfig")
	}
}

func TestNewClient_ValidKubeconfig(t *testing.T) {
	path := writeKubeconfig(t, validKubeconfig)
	client, err := NewClient(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestRestConfig_InvalidPath(t *testing.T) {
	_, err := RestConfig("/nonexistent/kubeconfig")
	if err == nil {
		t.Error("expected error for nonexistent kubeconfig")
	}
}

func TestRestConfig_ValidKubeconfig(t *testing.T) {
	path := writeKubeconfig(t, validKubeconfig)
	cfg, err := RestConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "https://127.0.0.1:6443" {
		t.Errorf("expected host https://127.0.0.1:6443, got %s", cfg.Host)
	}
}

func TestNewClient_EmptyKubeconfig_NoInCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	_, err := NewClient("")
	if err == nil {
		t.Error("expected error when not in-cluster and no kubeconfig")
	}
}
