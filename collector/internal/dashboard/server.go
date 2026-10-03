package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// Features holds the optional, opt-in UI features the server advertises via
// /api/me. The zero value disables every feature, which is what consumers
// upgrading from a release without the feature flags will get.
type Features struct {
	// TimelineDeltas controls whether the dashboard renders the +N/-N
	// unique-image delta badge on each timeline card. Off by default to
	// match the chart default; enable via webUI.features.timelineDeltas.
	TimelineDeltas bool `json:"timelineDeltas"`
}

// Server serves the provenance dashboard web UI.
type Server struct {
	reportsDir string
	auth       *authenticator
	scan       ScanRunner
	features   Features
	mux        *http.ServeMux
}

// NewServer creates a dashboard HTTP handler that reads reports from reportsDir.
// Authorization is disabled until WithAuth is called with a non-empty issuer.
// The scan endpoint is disabled until WithScanRunner is called with a runner.
func NewServer(reportsDir string) *Server {
	s := &Server{reportsDir: reportsDir}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/reports", s.requireAuth(s.handleListReports))
	mux.HandleFunc("/api/reports/", s.requireAuth(s.handleGetReport))
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/scan", s.handleScan)
	mux.HandleFunc("/api/export", s.requireAuth(s.handleExport))
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux = mux
	return s
}

// requireAuth wraps a read handler so it rejects unauthenticated callers with
// 401 whenever authorization is configured. It only checks for a valid
// identity (any authenticated user) — not admin-group membership, which
// remains specific to /api/scan. When auth is disabled (no OIDC issuer), the
// wrapper is a no-op, preserving the local/dev unauthenticated mode. The
// gateway deliberately does not enforce auth in the SPA login model, so these
// read endpoints (reports, export) must gate access themselves.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth.enabled() && s.auth.identify(r.Context(), r) == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// WithAuth attaches an OIDC authorization config. Passing a config with an
// empty IssuerURL or AdminGroups leaves authorization disabled — /api/me
// will still respond, but always with canRunScan=false.
func (s *Server) WithAuth(cfg AuthConfig) *Server {
	s.auth = newAuthenticator(cfg)
	return s
}

// WithScanRunner attaches the runner used by /api/scan to create one-shot
// Jobs. When unset, /api/scan responds with 503.
func (s *Server) WithScanRunner(r ScanRunner) *Server {
	s.scan = r
	return s
}

// WithFeatures sets the opt-in UI feature flags returned by /api/me. The
// dashboard JS reads these during init() and gates rendering of the
// corresponding components on them.
func (s *Server) WithFeatures(f Features) *Server {
	s.features = f
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// reportEntry is a summary of a report file for the listing API.
type reportEntry struct {
	Filename    string               `json:"filename"`
	GeneratedAt string               `json:"generatedAt"`
	Summary     report.ReportSummary `json:"summary"`
	ClusterName string               `json:"clusterName,omitempty"`
}

func (s *Server) handleListReports(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.reportsDir)
	if err != nil {
		http.Error(w, "failed to read reports directory", http.StatusInternalServerError)
		return
	}

	// Initialize non-nil so an empty reports dir encodes as `[]`, not `null` — a
	// JSON null would slip past the frontend's list handling and crash the render.
	reports := []reportEntry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "provenance-latest.json" {
			continue
		}

		r, err := s.loadReport(e.Name())
		if err != nil {
			slog.Warn("skipping malformed report", "file", e.Name(), "error", err)
			continue
		}

		reports = append(reports, reportEntry{
			Filename:    e.Name(),
			GeneratedAt: r.Metadata.GeneratedAt.Format("2006-01-02T15:04:05Z"),
			Summary:     r.Summary,
			ClusterName: r.Metadata.ClusterName,
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].GeneratedAt > reports[j].GeneratedAt
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reports)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	if filename == "" || strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	// "latest" resolves to provenance-latest.json
	if filename == "latest" {
		filename = "provenance-latest.json"
	}

	data, err := os.ReadFile(filepath.Join(s.reportsDir, filename))
	if err != nil {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) loadReport(filename string) (*report.ProvenanceReport, error) {
	data, err := os.ReadFile(filepath.Join(s.reportsDir, filename))
	if err != nil {
		return nil, err
	}
	var r report.ProvenanceReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// meResponse is the JSON shape returned by /api/me.
type meResponse struct {
	AuthEnabled bool     `json:"authEnabled"`
	Email       string   `json:"email,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	CanRunScan  bool     `json:"canRunScan"`
	Features    Features `json:"features"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id := s.auth.identify(r.Context(), r)
	resp := meResponse{
		AuthEnabled: s.auth.enabled(),
		CanRunScan:  s.auth.canRunScan(id),
		Features:    s.features,
	}
	if id != nil {
		resp.Email = id.Email
		resp.Groups = id.Groups
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
