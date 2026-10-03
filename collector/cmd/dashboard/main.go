package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/nebari-dev/provenance-collector/internal/dashboard"
	"github.com/nebari-dev/provenance-collector/internal/report"
)

const (
	defaultPublicAddr   = ":8080"
	defaultInternalAddr = ":8081"
	defaultMaxUpload    = int64(16 << 20) // 16 MiB
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting provenance dashboard", "version", report.Version)

	reportsDir := os.Getenv("PROVENANCE_REPORT_PATH")
	if reportsDir == "" {
		reportsDir = "/reports"
	}

	publicAddr := os.Getenv("PROVENANCE_DASHBOARD_ADDR")
	if publicAddr == "" {
		publicAddr = defaultPublicAddr
	}

	internalAddr := os.Getenv("PROVENANCE_DASHBOARD_INTERNAL_ADDR")
	if internalAddr == "" {
		internalAddr = defaultInternalAddr
	}

	retention := parseDuration("PROVENANCE_REPORT_RETENTION", 7*24*time.Hour)
	maxUpload := parseBytes("PROVENANCE_UPLOAD_MAX_BYTES", defaultMaxUpload)

	authCfg := dashboard.AuthConfig{
		IssuerURL:   os.Getenv("PROVENANCE_OIDC_ISSUER"),
		AdminGroups: splitAndTrim(os.Getenv("PROVENANCE_ADMIN_GROUPS")),
	}

	manualJobTTL := parseManualJobTTL(os.Getenv("PROVENANCE_MANUAL_JOB_TTL"))

	features := dashboard.Features{
		TimelineDeltas: parseBool("PROVENANCE_FEATURE_TIMELINE_DELTAS"),
	}

	slog.Info("configuration loaded",
		"publicAddr", publicAddr,
		"internalAddr", internalAddr,
		"reportsDir", reportsDir,
		"retention", retention.String(),
		"maxUploadBytes", maxUpload,
		"authIssuer", authCfg.IssuerURL,
		"adminGroups", len(authCfg.AdminGroups),
		"manualJobTTL", manualJobTTL.String(),
		"features.timelineDeltas", features.TimelineDeltas,
	)

	publicSrv := dashboard.NewServer(reportsDir).WithAuth(authCfg).WithFeatures(features)
	internalSrv := dashboard.NewInternalServer(reportsDir, retention, maxUpload)

	// /api/scan needs an in-cluster client + the CronJob's namespace/name.
	// Missing config or being out-of-cluster simply leaves the endpoint
	// disabled — handler will respond 503.
	namespace := os.Getenv("PROVENANCE_NAMESPACE")
	cronJobName := os.Getenv("PROVENANCE_CRONJOB_NAME")
	if namespace != "" && cronJobName != "" {
		if runner, err := buildScanRunner(namespace, cronJobName, manualJobTTL); err != nil {
			slog.Warn("scan endpoint disabled: kubernetes client unavailable",
				"namespace", namespace, "cronJob", cronJobName, "error", err)
		} else {
			publicSrv = publicSrv.WithScanRunner(runner)
			slog.Info("scan endpoint enabled",
				"namespace", namespace, "cronJob", cronJobName)
		}
	}

	publicHTTP := &http.Server{
		Addr:         publicAddr,
		Handler:      publicSrv,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	internalHTTP := &http.Server{
		Addr:         internalAddr,
		Handler:      internalSrv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	serveErr := make(chan error, 2)
	go func() {
		slog.Info("public listener starting", "addr", publicAddr)
		if err := publicHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()
	go func() {
		slog.Info("internal listener starting", "addr", internalAddr)
		if err := internalHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-serveErr:
		slog.Error("listener failed", "error", err)
		cancel()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := publicHTTP.Shutdown(shutdownCtx); err != nil {
		slog.Error("public shutdown error", "error", err)
	}
	if err := internalHTTP.Shutdown(shutdownCtx); err != nil {
		slog.Error("internal shutdown error", "error", err)
	}
}

func buildScanRunner(namespace, cronJobName string, manualJobTTL time.Duration) (dashboard.ScanRunner, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return dashboard.NewK8sScanRunner(client, namespace, cronJobName, manualJobTTL), nil
}

// parseManualJobTTL reads the PROVENANCE_MANUAL_JOB_TTL env var.
// Empty / unparseable values fall back to DefaultManualJobTTL.
// Set the value to "0" (or any zero-duration string) to disable the TTL
// entirely so manual Jobs persist until manually deleted.
func parseManualJobTTL(v string) time.Duration {
	if v == "" {
		return dashboard.DefaultManualJobTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid PROVENANCE_MANUAL_JOB_TTL, using default", "value", v, "error", err)
		return dashboard.DefaultManualJobTTL
	}
	if d < 0 {
		return 0
	}
	return d
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseDuration(key string, fallback time.Duration) time.Duration {
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

// parseBool reads an env var and returns true iff the value is one of
// "1", "true", "yes", or "on" (case-insensitive). Anything else, including
// unset, is false — matching the opt-in posture for feature flags.
func parseBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func parseBytes(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
