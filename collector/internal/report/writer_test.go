package report

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testReport() *ProvenanceReport {
	return &ProvenanceReport{
		Metadata: ReportMetadata{
			GeneratedAt:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			CollectorVersion:  "test",
			ClusterName:       "test-cluster",
			NamespacesScanned: []string{"default"},
		},
		Images: []ImageRecord{
			{
				Image:     "nginx:1.27",
				Digest:    "sha256:abc123",
				Namespace: "default",
				Workload:  WorkloadRef{Kind: "Deployment", Name: "nginx"},
			},
		},
		Summary: ReportSummary{
			TotalImages:  1,
			UniqueImages: 1,
		},
	}
}

func TestPVCWriter(t *testing.T) {
	dir := t.TempDir()
	writer := NewPVCWriter(dir, -1)

	err := writer.Write(context.Background(), testReport())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that latest file was written
	latestPath := filepath.Join(dir, "provenance-latest.json")
	data, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("failed to read latest report: %v", err)
	}

	var report ProvenanceReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("failed to unmarshal report: %v", err)
	}

	if report.Metadata.ClusterName != "test-cluster" {
		t.Errorf("expected cluster name test-cluster, got %s", report.Metadata.ClusterName)
	}
	if len(report.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(report.Images))
	}
}

func TestPVCWriter_Retention(t *testing.T) {
	dir := t.TempDir()

	// Write an "old" report file with a past modification time
	oldFile := filepath.Join(dir, "provenance-20240101-060000.json")
	if err := os.WriteFile(oldFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago
	if err := os.Chtimes(oldFile, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	// Write a "recent" report file
	recentFile := filepath.Join(dir, "provenance-20250601-060000.json")
	if err := os.WriteFile(recentFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use 7-day retention — old file should be pruned
	writer := NewPVCWriter(dir, 7*24*time.Hour)
	if err := writer.Write(context.Background(), testReport()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old report to be pruned")
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Error("expected recent report to be kept")
	}
}

func TestPVCWriter_RetentionDisabled(t *testing.T) {
	dir := t.TempDir()

	oldFile := filepath.Join(dir, "provenance-20240101-060000.json")
	if err := os.WriteFile(oldFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	// Retention -1 means keep forever
	writer := NewPVCWriter(dir, -1)
	if err := writer.Write(context.Background(), testReport()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(oldFile); err != nil {
		t.Error("expected old report to be kept when retention is disabled")
	}
}

func TestPVCWriter_RetentionKeepsLatest(t *testing.T) {
	dir := t.TempDir()

	// provenance-latest.json should never be pruned even if old
	latestFile := filepath.Join(dir, "provenance-latest.json")
	if err := os.WriteFile(latestFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(latestFile, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	writer := NewPVCWriter(dir, 1*time.Hour)
	if err := writer.Write(context.Background(), testReport()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// provenance-latest.json is rewritten by Write, so it'll exist with new content
	if _, err := os.Stat(latestFile); err != nil {
		t.Error("expected provenance-latest.json to exist")
	}
}

func TestHTTPWriter_Success(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/reports" {
			t.Errorf("expected /internal/reports, got %s", r.URL.Path)
		}
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	writer := NewHTTPWriter(srv.URL+"/internal/reports", time.Second)
	if err := writer.Write(context.Background(), testReport()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("expected application/json, got %s", gotContentType)
	}
	var report ProvenanceReport
	if err := json.Unmarshal(gotBody, &report); err != nil {
		t.Fatalf("upload body is not valid JSON: %v", err)
	}
	if report.Metadata.ClusterName != "test-cluster" {
		t.Errorf("expected cluster name test-cluster, got %s", report.Metadata.ClusterName)
	}
}

func TestHTTPWriter_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	writer := NewHTTPWriter(srv.URL+"/internal/reports", time.Second)
	err := writer.Write(context.Background(), testReport())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHTTPWriter_Unreachable(t *testing.T) {
	// Closed server — connection should fail.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/internal/reports"
	srv.Close()

	writer := NewHTTPWriter(url, 200*time.Millisecond)
	if err := writer.Write(context.Background(), testReport()); err == nil {
		t.Fatal("expected error when upload endpoint is unreachable")
	}
}

func TestConfigMapWriter(t *testing.T) {
	client := fake.NewSimpleClientset()
	writer := NewConfigMapWriter(client, "test-report", "default")

	// First write — creates the ConfigMap
	err := writer.Write(context.Background(), testReport())
	if err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps("default").Get(context.Background(), "test-report", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get configmap: %v", err)
	}

	if _, ok := cm.Data["report.json"]; !ok {
		t.Error("expected report.json key in configmap data")
	}
	if cm.Labels["app.kubernetes.io/name"] != "provenance-collector" {
		t.Error("expected provenance-collector label")
	}

	// Second write — updates the ConfigMap
	err = writer.Write(context.Background(), testReport())
	if err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
}
