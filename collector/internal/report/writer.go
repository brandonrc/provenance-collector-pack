package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Writer persists a ProvenanceReport.
type Writer interface {
	Write(ctx context.Context, report *ProvenanceReport) error
}

// WriteReportFiles persists pre-marshaled report JSON to basePath as a
// timestamped file plus provenance-latest.json, and prunes files older than
// retention. A negative retention disables pruning. The dashboard's internal
// upload handler and PVCWriter share this primitive so the on-disk layout is
// identical regardless of how the report arrived.
func WriteReportFiles(basePath string, retention time.Duration, data []byte) error {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return fmt.Errorf("creating report directory: %w", err)
	}

	filename := fmt.Sprintf("provenance-%s.json", time.Now().UTC().Format("20060102-150405"))
	path := filepath.Join(basePath, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing report to %s: %w", path, err)
	}

	latestPath := filepath.Join(basePath, "provenance-latest.json")
	_ = os.Remove(latestPath)
	if err := os.WriteFile(latestPath, data, 0o644); err != nil {
		return fmt.Errorf("writing latest report: %w", err)
	}

	pruneOldReports(basePath, retention)
	return nil
}

func pruneOldReports(basePath string, retention time.Duration) {
	if retention < 0 {
		return
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-retention)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "provenance-latest.json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(basePath, e.Name()))
		}
	}
}

// PVCWriter writes reports as JSON files to a filesystem path (typically a PVC mount).
type PVCWriter struct {
	basePath  string
	retention time.Duration
}

// NewPVCWriter creates a Writer that outputs to the filesystem.
// Reports older than retention are removed after each write.
// A negative retention disables cleanup.
func NewPVCWriter(basePath string, retention time.Duration) Writer {
	return &PVCWriter{basePath: basePath, retention: retention}
}

func (w *PVCWriter) Write(_ context.Context, report *ProvenanceReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}
	return WriteReportFiles(w.basePath, w.retention, data)
}

// HTTPWriter uploads reports to the dashboard's internal upload endpoint over
// in-cluster HTTP. Used in `http` output mode so the collector Job and the
// dashboard don't need to share a PersistentVolume — only the dashboard pod
// owns the reports volume, sidestepping multi-attach errors on RWO storage
// classes like Hetzner's csi.hetzner.cloud.
type HTTPWriter struct {
	uploadURL string
	client    *http.Client
}

// NewHTTPWriter creates a Writer that POSTs reports to uploadURL. The URL
// must be the fully qualified path of the upload endpoint (e.g.
// http://provenance-web-internal.namespace.svc:8081/internal/reports).
func NewHTTPWriter(uploadURL string, timeout time.Duration) Writer {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPWriter{
		uploadURL: uploadURL,
		client:    &http.Client{Timeout: timeout},
	}
}

func (w *HTTPWriter) Write(ctx context.Context, report *ProvenanceReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("uploading report to %s: %w", w.uploadURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("upload to %s returned %d: %s", w.uploadURL, resp.StatusCode, string(body))
	}
	return nil
}

// ConfigMapWriter writes reports as Kubernetes ConfigMaps.
type ConfigMapWriter struct {
	client    kubernetes.Interface
	name      string
	namespace string
}

// NewConfigMapWriter creates a Writer that stores reports in a ConfigMap.
func NewConfigMapWriter(client kubernetes.Interface, name, namespace string) Writer {
	return &ConfigMapWriter{
		client:    client,
		name:      name,
		namespace: namespace,
	}
}

func (w *ConfigMapWriter) Write(ctx context.Context, report *ProvenanceReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.name,
			Namespace: w.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "provenance-collector",
				"app.kubernetes.io/managed-by": "provenance-collector",
			},
		},
		Data: map[string]string{
			"report.json": string(data),
			"generated":   report.Metadata.GeneratedAt.Format(time.RFC3339),
		},
	}

	existing, err := w.client.CoreV1().ConfigMaps(w.namespace).Get(ctx, w.name, metav1.GetOptions{})
	if err != nil {
		_, err = w.client.CoreV1().ConfigMaps(w.namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating configmap %s/%s: %w", w.namespace, w.name, err)
		}
		return nil
	}

	existing.Data = cm.Data
	existing.Labels = cm.Labels
	_, err = w.client.CoreV1().ConfigMaps(w.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating configmap %s/%s: %w", w.namespace, w.name, err)
	}
	return nil
}
