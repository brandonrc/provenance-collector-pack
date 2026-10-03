package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExportCSV(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=csv", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("expected text/csv, got %s", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "provenance-report.csv") {
		t.Errorf("expected csv filename in Content-Disposition, got %s", cd)
	}

	body := w.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least header + 1 row, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "Image,Namespace,") {
		t.Errorf("unexpected CSV header: %s", lines[0])
	}
	if !strings.Contains(lines[1], "nginx:1.27") {
		t.Errorf("expected nginx:1.27 in CSV data, got: %s", lines[1])
	}
}

func TestExportMarkdown(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=markdown", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("expected text/markdown, got %s", ct)
	}

	body := w.Body.String()
	for _, want := range []string{
		"# Provenance Report",
		"## Summary",
		"## Container Images",
		"nginx:1.27",
		"## Helm Releases",
		"ingress-nginx",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected markdown to contain %q", want)
		}
	}
}

func TestExportMdShorthand(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("expected text/markdown, got %s", ct)
	}
}

func TestExportDefaultFormat(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("expected default format csv, got %s", ct)
	}
}

func TestExportUnsupportedFormat(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=pdf", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported format, got %d", w.Code)
	}
}

func TestExportNoReport(t *testing.T) {
	srv := NewServer(t.TempDir())

	req := httptest.NewRequest("GET", "/api/export?format=csv", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no report exists, got %d", w.Code)
	}
}

// TestExportFilenameParam exercises the ?filename= override so the dashboard's
// timeline selection can drive the export. setupTestDir writes
// provenance-20250615-060000.json with the same content as the latest, so a
// successful response confirms the parameter was honored (without it the
// endpoint would still return the latest, which would look identical here).
func TestExportFilenameParam(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=csv&filename=provenance-20250615-060000.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "nginx:1.27") {
		t.Error("expected timestamped-file export to contain the same fixture image")
	}
}

func TestExportFilenameNotFound(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	req := httptest.NewRequest("GET", "/api/export?format=csv&filename=does-not-exist.json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing filename, got %d", w.Code)
	}
}

func TestExportFilenamePathTraversal(t *testing.T) {
	dir := setupTestDir(t)
	srv := NewServer(dir)

	for _, bad := range []string{"../etc/passwd", "subdir/file.json", ".."} {
		req := httptest.NewRequest("GET", "/api/export?format=csv&filename="+bad, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("filename=%q: expected 400, got %d", bad, w.Code)
		}
	}
}

func TestCSVEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"has,comma", "\"has,comma\""},
		{"has\"quote", "\"has\"\"quote\""},
		{"has\nnewline", "\"has\nnewline\""},
		{"", ""},
	}
	for _, tc := range tests {
		got := csvEscape(tc.input)
		if got != tc.want {
			t.Errorf("csvEscape(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
