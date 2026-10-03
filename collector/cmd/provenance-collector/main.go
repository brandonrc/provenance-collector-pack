package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	"k8s.io/client-go/kubernetes"
)

// options are the command-line flags. With neither flag set the collector
// behaves exactly as before: one collection, written to the sink chosen by
// PROVENANCE_REPORT_OUTPUT (http, pvc or configmap).
type options struct {
	// once requests a single run whose report goes to output instead of the
	// configured sink. Implies output "-" when output is empty.
	once bool
	// output is a file path, or "-" for stdout. When set, the report is
	// written there and PROVENANCE_REPORT_OUTPUT / the upload URL are ignored.
	output string
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("provenance-collector", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&o.once, "once", false, "run one collection and write the report to --output (default stdout) instead of the configured sink")
	fs.StringVar(&o.output, "output", "", `write the report JSON to this file path, or "-" for stdout, instead of the configured sink`)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if o.once && o.output == "" {
		o.output = "-"
	}
	return o, nil
}

// logWriter keeps stdout clean for the report when it is written to stdout.
func logWriter(o options) io.Writer {
	if o.output == "-" {
		return os.Stderr
	}
	return os.Stdout
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter(opts), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting provenance collector", "version", report.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, opts); err != nil {
		slog.Error("collection failed", "error", err)
		os.Exit(1)
	}

	slog.Info("provenance collection completed successfully")
}

func run(ctx context.Context, opts options) error {
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
		"reportOutput", effectiveOutput(cfg, opts),
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
	writer, err := selectWriter(cfg, opts, client, os.Stdout)
	if err != nil {
		return err
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

func effectiveOutput(cfg *config.Config, opts options) string {
	if opts.output != "" {
		return "file:" + opts.output
	}
	return cfg.ReportOutput
}

// selectWriter picks the report sink: --output wins, otherwise
// PROVENANCE_REPORT_OUTPUT (http by default).
func selectWriter(cfg *config.Config, opts options, client kubernetes.Interface, stdout io.Writer) (report.Writer, error) {
	if opts.output != "" {
		slog.Info("writing report to file", "path", opts.output)
		return report.NewFileWriter(opts.output, stdout), nil
	}
	switch cfg.ReportOutput {
	case "configmap":
		slog.Info("writing report to configmap", "name", cfg.ReportConfigMap, "namespace", cfg.ReportConfigMapNamespace)
		return report.NewConfigMapWriter(client, cfg.ReportConfigMap, cfg.ReportConfigMapNamespace), nil
	case "pvc":
		slog.Info("writing report to filesystem", "path", cfg.ReportPath, "retention", cfg.ReportRetention)
		return report.NewPVCWriter(cfg.ReportPath, cfg.ReportRetention), nil
	case "http", "":
		if cfg.ReportUploadURL == "" {
			return nil, fmt.Errorf("PROVENANCE_REPORT_UPLOAD_URL is required when PROVENANCE_REPORT_OUTPUT=http (or pass --output)")
		}
		slog.Info("uploading report to dashboard", "url", cfg.ReportUploadURL, "timeout", cfg.ReportUploadTimeout)
		return report.NewHTTPWriter(cfg.ReportUploadURL, cfg.ReportUploadTimeout), nil
	default:
		return nil, fmt.Errorf("unknown PROVENANCE_REPORT_OUTPUT %q (expected http, pvc, or configmap)", cfg.ReportOutput)
	}
}
