package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testReport = `{
  "metadata": {
    "generatedAt": "2025-06-15T06:00:00Z",
    "collectorVersion": "test",
    "clusterName": "test-cluster",
    "namespacesScanned": ["default"]
  },
  "images": [
    {
      "image": "nginx:1.27",
      "digest": "sha256:abc123def456",
      "namespace": "default",
      "workload": {"kind": "Deployment", "name": "nginx"},
      "signature": {"signed": true, "verified": true},
      "sbom": {"hasSBOM": true, "format": "spdx"}
    }
  ],
  "helmReleases": [
    {
      "releaseName": "ingress",
      "namespace": "default",
      "chart": "ingress-nginx",
      "version": "4.8.0",
      "appVersion": "1.9.4",
      "status": "deployed"
    }
  ],
  "summary": {
    "totalImages": 1,
    "uniqueImages": 1,
    "signedImages": 1,
    "verifiedImages": 1,
    "imagesWithSBOM": 1,
    "imagesWithUpdates": 0,
    "totalHelmReleases": 1,
    "helmReleasesWithUpdates": 0
  }
}`

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "provenance-20250615-060000.json"), []byte(testReport), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write the latest symlink file too — should be excluded from listing
	if err := os.WriteFile(filepath.Join(dir, "provenance-latest.json"), []byte(testReport), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHealthz(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestListReports(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reports []reportEntry
	if err := json.Unmarshal(w.Body.Bytes(), &reports); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Should have 1 report (provenance-latest.json is excluded)
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if reports[0].Filename != "provenance-20250615-060000.json" {
		t.Errorf("expected provenance-20250615-060000.json, got %s", reports[0].Filename)
	}
	if reports[0].Summary.TotalImages != 1 {
		t.Errorf("expected totalImages=1, got %d", reports[0].Summary.TotalImages)
	}
	if reports[0].ClusterName != "test-cluster" {
		t.Errorf("expected clusterName=test-cluster, got %s", reports[0].ClusterName)
	}
}

func TestListReports_EmptyDir(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/api/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reports []reportEntry
	if err := json.Unmarshal(w.Body.Bytes(), &reports); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	// An empty reports dir must encode as `[]`, not `null`, so the frontend's
	// list handling doesn't choke on a null body.
	if reports == nil {
		t.Error("expected an empty array, got null")
	}
	if len(reports) != 0 {
		t.Errorf("expected 0 reports, got %d", len(reports))
	}
}

func TestGetReport(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/reports/provenance-20250615-060000.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestGetReport_NotFound(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/api/reports/nonexistent.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetReport_PathTraversal(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/api/reports/..%2f..%2fetc%2fpasswd", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// The handler checks for ".." and "/" in the filename and rejects them.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", w.Code)
	}
}

// The dashboard is API-only; the UI is served by the separate frontend
// container. The root path is no longer handled and should 404.
func TestRoot_NotFound(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /, got %d", w.Code)
	}
}

func TestGetReport_ValidJSON(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/reports/provenance-20250615-060000.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if _, ok := report["metadata"]; !ok {
		t.Error("expected metadata field in report")
	}
	if _, ok := report["images"]; !ok {
		t.Error("expected images field in report")
	}
	if _, ok := report["summary"]; !ok {
		t.Error("expected summary field in report")
	}
}

func TestListReports_SortedNewestFirst(t *testing.T) {
	dir := t.TempDir()
	// Create two reports with different timestamps
	report1 := `{"metadata":{"generatedAt":"2025-06-14T06:00:00Z","collectorVersion":"test","namespacesScanned":["default"]},"images":[],"summary":{"totalImages":0}}`
	report2 := `{"metadata":{"generatedAt":"2025-06-15T06:00:00Z","collectorVersion":"test","namespacesScanned":["default"]},"images":[],"summary":{"totalImages":0}}`

	if err := os.WriteFile(filepath.Join(dir, "provenance-20250614-060000.json"), []byte(report1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provenance-20250615-060000.json"), []byte(report2), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/api/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var reports []reportEntry
	if err := json.Unmarshal(w.Body.Bytes(), &reports); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	// Newest first
	if reports[0].GeneratedAt < reports[1].GeneratedAt {
		t.Errorf("expected newest first, got %s before %s", reports[0].GeneratedAt, reports[1].GeneratedAt)
	}
}

func TestGetReport_Latest(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/reports/latest", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := report["metadata"]; !ok {
		t.Error("expected metadata in latest report")
	}
}

func TestGetReport_EmptyFilename(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest("GET", "/api/reports/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty filename, got %d", w.Code)
	}
}

func TestListReports_SkipsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	// Valid report
	if err := os.WriteFile(filepath.Join(dir, "provenance-20250615-060000.json"), []byte(testReport), 0o644); err != nil {
		t.Fatal(err)
	}
	// Malformed JSON
	if err := os.WriteFile(filepath.Join(dir, "provenance-20250614-060000.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-JSON file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a report"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(dir)
	req := httptest.NewRequest("GET", "/api/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var reports []reportEntry
	if err := json.Unmarshal(w.Body.Bytes(), &reports); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(reports) != 1 {
		t.Errorf("expected 1 valid report (skipping malformed + non-json), got %d", len(reports))
	}
}

func TestListReports_NonexistentDir(t *testing.T) {
	srv := NewServer("/nonexistent/path")
	req := httptest.NewRequest("GET", "/api/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for nonexistent dir, got %d", w.Code)
	}
}

// readEndpoints are the endpoints requireAuth wraps: they expose report data
// and must reject unauthenticated callers when auth is configured.
var readEndpoints = []string{
	"/api/reports",
	"/api/reports/provenance-20250615-060000.json",
	"/api/export?filename=provenance-20250615-060000.json",
}

// When auth is enabled, the read endpoints reject a caller with no bearer (or
// an unrecognized token) with 401 instead of leaking report data.
func TestReadEndpoints_RequireAuth_Unauthenticated(t *testing.T) {
	dir := setupTestDir(t)
	stub := userInfoStub(t, nil, nil) // every token is unknown -> 401 from userinfo
	defer stub.Close()

	srv := NewServer(dir).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})

	for _, path := range readEndpoints {
		t.Run(path, func(t *testing.T) {
			for _, bearer := range []string{"", "bogus-token"} {
				req := httptest.NewRequest("GET", path, nil)
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				w := httptest.NewRecorder()
				srv.ServeHTTP(w, req)

				if w.Code != http.StatusUnauthorized {
					t.Errorf("bearer=%q: expected 401, got %d", bearer, w.Code)
				}
			}
		})
	}
}

// Any authenticated user — not just admins — may read reports. A valid token
// whose groups don't include an admin group must still get 200.
func TestReadEndpoints_AuthenticatedNonAdmin_OK(t *testing.T) {
	dir := setupTestDir(t)
	stub := userInfoStub(t, map[string]string{
		"user-token": `{"sub":"u2","email":"u@x","groups":["/users"]}`,
	}, nil)
	defer stub.Close()

	srv := NewServer(dir).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})

	for _, path := range readEndpoints {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer user-token")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for authenticated non-admin, got %d", w.Code)
			}
		})
	}
}

// With auth disabled (no issuer), the read endpoints stay open — preserving
// the local/dev unauthenticated mode.
func TestReadEndpoints_AuthDisabled_Open(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	for _, path := range readEndpoints {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 when auth disabled, got %d", w.Code)
			}
		})
	}
}
