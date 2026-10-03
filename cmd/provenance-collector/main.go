package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/provenance-collector/internal/config"
	"github.com/nebari-dev/provenance-collector/internal/discovery"
	k8s "github.com/nebari-dev/provenance-collector/internal/kubernetes"
	"github.com/nebari-dev/provenance-collector/internal/registry"
	"github.com/nebari-dev/provenance-collector/internal/report"
	"github.com/nebari-dev/provenance-collector/internal/verify"
)

// options are the command-line flags. The collector always performs a single
// collection and exits (it is run as a CronJob or by another tool). With no
// flags the report goes to the sink chosen by PROVENANCE_REPORT_OUTPUT (http,
// pvc or configmap), exactly as before flags existed.
type options struct {
	// output is a file path, or "-" for stdout. When set, the report is
	// written there and PROVENANCE_REPORT_OUTPUT / the upload URL are ignored.
	output string
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("provenance-collector", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: provenance-collector [--output <path|->]\n\n"+
			"Runs one collection and exits. Configuration comes from PROVENANCE_* environment variables.\n\n")
		fs.PrintDefaults()
	}
	fs.StringVar(&o.output, "output", "", `write the report JSON to this file path (atomically), or "-" for stdout, instead of the PROVENANCE_REPORT_OUTPUT sink`)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return o, nil
}

// logWriter keeps stdout clean for the report when it is written to stdout.
func logWriter(o options, stdout, stderr io.Writer) io.Writer {
	if o.output == "-" {
		return stderr
	}
	return stdout
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter(opts, os.Stdout, os.Stderr), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting provenance collector", "version", report.Version, "schemaVersion", report.SchemaVersion)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, config.Load(), opts, defaultDeps()); err != nil {
		slog.Error("collection failed", "error", err)
		cancel()
		os.Exit(1)
	}

	slog.Info("provenance collection completed successfully")
}

// enrichers are the registry-backed lookups the report generator runs per
// image. A nil field disables that step.
type enrichers struct {
	digests    report.DigestResolver
	updates    report.UpdateChecker
	signatures report.SignatureVerifier
	sbom       report.SBOMDiscoverer
	provenance report.ProvenanceChecker
}

// deps are run's external collaborators. defaultDeps wires the real cluster
// and registries; tests substitute a fake clientset and stub enrichers so a
// whole collection runs offline (see the golden report test).
type deps struct {
	stdout    io.Writer
	kube      func(kubeconfig string) (kubernetes.Interface, error)
	helm      func(client kubernetes.Interface, namespaces, exclude []string) discovery.HelmDiscoverer
	enrichers func(cfg *config.Config) (enrichers, error)
	now       func() time.Time
}

func defaultDeps() deps {
	return deps{
		stdout:    os.Stdout,
		kube:      k8s.NewClient,
		helm:      discovery.NewHelmDiscoverer,
		enrichers: registryEnrichers,
		now:       time.Now,
	}
}

// registryEnrichers builds the real enrichers, all sharing one registry
// client so auth (PROVENANCE_REGISTRY_AUTH), extra CAs and insecure hosts
// apply to every registry call.
func registryEnrichers(cfg *config.Config) (enrichers, error) {
	client, err := registry.NewClient(registry.Options{
		AuthFile: cfg.RegistryAuth,
		CAFile:   cfg.RegistryCAFile,
		Insecure: cfg.RegistryInsecure,
	})
	if err != nil {
		return enrichers{}, err
	}
	e := enrichers{digests: registry.NewDigestResolver(cfg.RegistryTimeout, client)}
	if cfg.CheckUpdates {
		e.updates = registry.NewUpdateChecker(cfg.SkipPrerelease, cfg.UpdateLevel, client)
	}
	if cfg.VerifySignatures {
		e.signatures = verify.NewSignatureVerifier(cfg.CosignPublicKey, client)
	}
	if cfg.CheckSBOM {
		e.sbom = verify.NewSBOMDiscoverer(client)
	}
	if cfg.CheckProvenance {
		e.provenance = verify.NewProvenanceChecker(client)
	}
	return e, nil
}

func run(ctx context.Context, cfg *config.Config, opts options, d deps) error {
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
		"registryAuth", cfg.RegistryAuth != "",
		"registryCAFile", cfg.RegistryCAFile,
		"registryInsecure", cfg.RegistryInsecure,
		"clusterName", cfg.ClusterName,
	)

	client, err := d.kube(cfg.Kubeconfig)
	if err != nil {
		return err
	}

	// Fail before discovery if the sink is misconfigured or the registry
	// settings are unusable, rather than after a long collection.
	writer, err := selectWriter(cfg, opts, client, d.stdout)
	if err != nil {
		return err
	}
	enrich, err := d.enrichers(cfg)
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

	imageInputs := make([]report.ImageInput, 0, len(discoveredImages))
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

	nsList := make([]string, 0, len(namespacesScanned))
	for ns := range namespacesScanned {
		nsList = append(nsList, ns)
	}
	sort.Strings(nsList)

	var warnings []string
	var helmSources []report.HelmSource
	if cfg.HelmEnabled {
		slog.Info("discovering helm releases")
		releases, err := d.helm(client, cfg.Namespaces, cfg.ExcludeNamespaces).Discover(ctx)
		if err != nil {
			// Partial or total failure: keep whatever was listed and say what
			// was missed, so "0 releases" and "could not read Secrets" are
			// distinguishable in the report.
			slog.Warn("helm discovery incomplete", "error", err, "releasesFound", len(releases))
			warnings = append(warnings, helmWarnings(err)...)
		}
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

	// --- Generate report ---
	slog.Info("generating provenance report")
	gen := report.NewGenerator(
		report.GeneratorConfig{
			VerifySignatures: cfg.VerifySignatures,
			CheckUpdates:     cfg.CheckUpdates,
			ClusterName:      cfg.ClusterName,
			Concurrency:      10,
			Now:              d.now,
		},
		enrich.digests,
		enrich.updates,
		enrich.signatures,
		enrich.sbom,
		enrich.provenance,
	)

	provReport := gen.Generate(ctx, imageInputs, helmSources, nsList, warnings...)

	// A cancelled run (SIGTERM, or the caller's timeout) leaves every
	// remaining registry lookup failed; don't publish that as a report.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("collection interrupted: %w", err)
	}

	// --- Write report ---
	if err := writer.Write(ctx, provReport); err != nil {
		return err
	}

	slog.Info("report written successfully",
		"totalImages", provReport.Summary.TotalImages,
		"uniqueImages", provReport.Summary.UniqueImages,
		"signedImages", provReport.Summary.SignedImages,
		"helmReleases", provReport.Summary.TotalHelmReleases,
		"warnings", len(provReport.Warnings),
	)

	return nil
}

// helmWarnings turns a Helm discovery error into one warning per failed
// namespace (or a single warning when discovery failed outright).
func helmWarnings(err error) []string {
	var errs []error
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	} else {
		errs = []error{err}
	}
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, "helm: "+e.Error())
	}
	return out
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
