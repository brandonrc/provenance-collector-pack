package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInternalServer_Upload(t *testing.T) {
	dir := t.TempDir()
	srv := NewInternalServer(dir, -1, 1<<20)

	req := httptest.NewRequest(http.MethodPost, "/internal/reports", bytes.NewReader([]byte(testReport)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}

	latest, err := os.ReadFile(filepath.Join(dir, "provenance-latest.json"))
	if err != nil {
		t.Fatalf("expected provenance-latest.json to be written: %v", err)
	}
	if !bytes.Contains(latest, []byte(`"test-cluster"`)) {
		t.Errorf("persisted report did not include cluster name; got %s", string(latest))
	}

	entries, _ := os.ReadDir(dir)
	var timestamped int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "provenance-") && e.Name() != "provenance-latest.json" {
			timestamped++
		}
	}
	if timestamped != 1 {
		t.Errorf("expected 1 timestamped report, got %d", timestamped)
	}
}

func TestInternalServer_RejectsGet(t *testing.T) {
	srv := NewInternalServer(t.TempDir(), -1, 1<<20)
	req := httptest.NewRequest(http.MethodGet, "/internal/reports", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != http.MethodPost {
		t.Errorf("expected Allow: POST, got %s", allow)
	}
}

func TestInternalServer_RejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	srv := NewInternalServer(dir, -1, 1<<20)

	req := httptest.NewRequest(http.MethodPost, "/internal/reports", bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "provenance-latest.json")); !os.IsNotExist(err) {
		t.Error("malformed upload should not have produced a latest report")
	}
}

func TestInternalServer_EnforcesMaxBytes(t *testing.T) {
	srv := NewInternalServer(t.TempDir(), -1, 16)

	req := httptest.NewRequest(http.MethodPost, "/internal/reports", bytes.NewReader([]byte(testReport)))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d", w.Code)
	}
}

func TestInternalServer_Healthz(t *testing.T) {
	srv := NewInternalServer(t.TempDir(), -1, 1<<20)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInternalServer_Retention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "provenance-20240101-060000.json")
	if err := os.WriteFile(old, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	srv := NewInternalServer(dir, 24*time.Hour, 1<<20)
	req := httptest.NewRequest(http.MethodPost, "/internal/reports", bytes.NewReader([]byte(testReport)))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("expected old report to be pruned by retention")
	}
}
