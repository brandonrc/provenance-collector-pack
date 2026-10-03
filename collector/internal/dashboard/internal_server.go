package dashboard

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// InternalServer exposes the dashboard's collector-facing endpoints. It must
// be bound to a port that is only reachable from inside the cluster — never
// fronted by an Ingress — because it accepts unauthenticated report uploads
// from the collector Job. The boundary is the Service exposure, not a token.
type InternalServer struct {
	reportsDir string
	retention  time.Duration
	maxBytes   int64
	mux        *http.ServeMux
}

// NewInternalServer creates the collector-facing HTTP handler. retention is
// applied to existing files in reportsDir after each successful upload (a
// negative duration disables pruning). maxBytes caps the request body so a
// runaway collector can't fill the volume.
func NewInternalServer(reportsDir string, retention time.Duration, maxBytes int64) *InternalServer {
	s := &InternalServer{
		reportsDir: reportsDir,
		retention:  retention,
		maxBytes:   maxBytes,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/reports", s.handleUpload)
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux = mux
	return s
}

func (s *InternalServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *InternalServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *InternalServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body := http.MaxBytesReader(w, r.Body, s.maxBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var probe report.ProvenanceReport
	if err := json.Unmarshal(data, &probe); err != nil {
		slog.Warn("rejecting malformed upload", "error", err, "remote", r.RemoteAddr)
		http.Error(w, "invalid report JSON", http.StatusBadRequest)
		return
	}

	if err := report.WriteReportFiles(s.reportsDir, s.retention, data); err != nil {
		slog.Error("failed to persist uploaded report", "error", err)
		http.Error(w, "failed to persist report", http.StatusInternalServerError)
		return
	}

	slog.Info("report uploaded",
		"clusterName", probe.Metadata.ClusterName,
		"totalImages", probe.Summary.TotalImages,
		"remote", r.RemoteAddr,
	)

	w.WriteHeader(http.StatusAccepted)
}
