package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nebari-dev/provenance-collector/internal/config"
	"github.com/nebari-dev/provenance-collector/internal/discovery"
	k8s "github.com/nebari-dev/provenance-collector/internal/kubernetes"
	"github.com/nebari-dev/provenance-collector/internal/registry"
	"github.com/nebari-dev/provenance-collector/internal/report"
	"github.com/nebari-dev/provenance-collector/internal/verify"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting provenance collector", "version", report.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("collection failed", "error", err)
		os.Exit(1)
	}

	slog.Info("provenance collection completed successfully")
}

func run(ctx context.Context) error {
	cfg := config.Load()

	retention := cfg.ReportRetention.String()
	if cfg.ReportRetention < 0 {
		retention = "disabled"
	}

	slog.Info("configuration loaded",
		"namespaces", cfg.Namespaces,
		"excludeNamespaces", cfg.ExcludeNamespaces,
		"verifySignatures", cfg.VerifySignatures,
		"checkUpdates", cfg.CheckUpdates,
		"updateLevel", cfg.UpdateLevel,
		"skipPrerelease", cfg.SkipPrerelease,
		"checkSBOM", cfg.CheckSBOM,
		"checkProvenance", cfg.CheckProvenance,
		"helmEnabled", cfg.HelmEnabled,
		"reportOutput", cfg.ReportOutput,
		"reportRetention", retention,
		"registryTimeout", cfg.RegistryTimeout,
		"clusterName", cfg.ClusterName,
	)

	// Build Kubernetes client
	client, err := k8s.NewClient(cfg.Kubeconfig)
	if err != nil {
		return err
	}

	restCfg, err := k8s.RestConfig(cfg.Kubeconfig)
	if err != nil {
		return err
	}

	// --- Discovery ---
	slog.Info("discovering container images")
	imgDiscoverer := discovery.NewImageDiscoverer(client, cfg.Namespaces, cfg.ExcludeNamespaces)
	discoveredImages, err := imgDiscoverer.Discover(ctx)
	if err != nil {
		return err
	}
	slog.Info("image discovery complete", "count", len(discoveredImages))

	// Build image inputs for the generator
	var imageInputs []report.ImageInput
	namespacesScanned := make(map[string]bool)
	for _, di := range discoveredImages {
		namespacesScanned[di.Namespace] = true
		imageInputs = append(imageInputs, report.ImageInput{
			Image:        di.Image,
			Namespace:    di.Namespace,
			WorkloadKind: di.OwnerKind,
			WorkloadName: di.OwnerName,
		})
	}

	var nsList []string
	for ns := range namespacesScanned {
		nsList = append(nsList, ns)
	}

	// Helm discovery
	var helmSources []report.HelmSource
	if cfg.HelmEnabled {
		slog.Info("discovering helm releases")
		helmDiscoverer := discovery.NewHelmDiscoverer(client, restCfg, cfg.Namespaces, cfg.ExcludeNamespaces)
		releases, err := helmDiscoverer.Discover(ctx)
		if err != nil {
			slog.Warn("helm discovery failed, continuing without helm data", "error", err)
		} else {
			slog.Info("helm discovery complete", "count", len(releases))
			for _, r := range releases {
				helmSources = append(helmSources, report.HelmSource{
					ReleaseName:  r.Name,
					Namespace:    r.Namespace,
					ChartName:    r.ChartName,
					ChartVersion: r.ChartVersion,
					AppVersion:   r.AppVersion,
					Status:       r.Status,
				})
			}
		}
	}

	// --- Enrichment dependencies ---
	digestResolver := registry.NewDigestResolver(cfg.RegistryTimeout)
	var updateChecker report.UpdateChecker
	if cfg.CheckUpdates {
		updateChecker = registry.NewUpdateChecker(cfg.SkipPrerelease, cfg.UpdateLevel)
	}
	var sigVerifier report.SignatureVerifier
	if cfg.VerifySignatures {
		sigVerifier = verify.NewSignatureVerifier(cfg.CosignPublicKey)
	}
	var sbomDisc report.SBOMDiscoverer
	if cfg.CheckSBOM {
		sbomDisc = verify.NewSBOMDiscoverer()
	}
	var provChecker report.ProvenanceChecker
	if cfg.CheckProvenance {
		provChecker = verify.NewProvenanceChecker()
	}

	// --- Generate report ---
	slog.Info("generating provenance report")
	gen := report.NewGenerator(
		report.GeneratorConfig{
			VerifySignatures: cfg.VerifySignatures,
			CheckUpdates:     cfg.CheckUpdates,
			ClusterName:      cfg.ClusterName,
			Concurrency:      10,
		},
		digestResolver,
		updateChecker,
		sigVerifier,
		sbomDisc,
		provChecker,
	)

	provReport := gen.Generate(ctx, imageInputs, helmSources, nsList)

	// --- Write report ---
	var writer report.Writer
	switch cfg.ReportOutput {
	case "configmap":
		slog.Info("writing report to configmap", "name", cfg.ReportConfigMap, "namespace", cfg.ReportConfigMapNamespace)
		writer = report.NewConfigMapWriter(client, cfg.ReportConfigMap, cfg.ReportConfigMapNamespace)
	case "pvc":
		slog.Info("writing report to filesystem", "path", cfg.ReportPath, "retention", cfg.ReportRetention)
		writer = report.NewPVCWriter(cfg.ReportPath, cfg.ReportRetention)
	case "http", "":
		if cfg.ReportUploadURL == "" {
			return fmt.Errorf("PROVENANCE_REPORT_UPLOAD_URL is required when PROVENANCE_REPORT_OUTPUT=http")
		}
		slog.Info("uploading report to dashboard", "url", cfg.ReportUploadURL, "timeout", cfg.ReportUploadTimeout)
		writer = report.NewHTTPWriter(cfg.ReportUploadURL, cfg.ReportUploadTimeout)
	default:
		return fmt.Errorf("unknown PROVENANCE_REPORT_OUTPUT %q (expected http, pvc, or configmap)", cfg.ReportOutput)
	}

	if err := writer.Write(ctx, provReport); err != nil {
		return err
	}

	slog.Info("report written successfully",
		"totalImages", provReport.Summary.TotalImages,
		"uniqueImages", provReport.Summary.UniqueImages,
		"signedImages", provReport.Summary.SignedImages,
		"helmReleases", provReport.Summary.TotalHelmReleases,
	)

	return nil
}
