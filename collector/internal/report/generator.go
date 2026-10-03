package report

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Version is set at build time via ldflags.
var Version = "dev"

// ImageSource provides discovered images from the cluster.
type ImageSource interface {
	Image() string
	Namespace() string
	WorkloadKind() string
	WorkloadName() string
}

// DigestResolver resolves image references to digests.
type DigestResolver interface {
	Resolve(ctx context.Context, imageRef string) (string, error)
}

// UpdateChecker checks for available image updates.
type UpdateChecker interface {
	Check(ctx context.Context, imageRef string) (*UpdateInfo, error)
}

// SignatureVerifier checks image signatures.
type SignatureVerifier interface {
	Verify(ctx context.Context, imageRef string) (*SignatureInfo, error)
}

// SBOMDiscoverer checks for attached SBOMs.
type SBOMDiscoverer interface {
	Discover(ctx context.Context, imageRef string) (*SBOMInfo, error)
}

// ProvenanceChecker checks for SLSA provenance attestations.
type ProvenanceChecker interface {
	Check(ctx context.Context, imageRef string) (*ProvenanceInfo, error)
}

// HelmSource provides discovered Helm releases.
type HelmSource struct {
	ReleaseName  string
	Namespace    string
	ChartName    string
	ChartVersion string
	AppVersion   string
	Status       string
}

// GeneratorConfig controls which enrichment steps run.
type GeneratorConfig struct {
	VerifySignatures bool
	CheckUpdates     bool
	ClusterName      string
	Concurrency      int
}

// Generator orchestrates image/helm discovery and enrichment into a report.
type Generator struct {
	cfg               GeneratorConfig
	digestResolver    DigestResolver
	updateChecker     UpdateChecker
	sigVerifier       SignatureVerifier
	sbomDiscoverer    SBOMDiscoverer
	provenanceChecker ProvenanceChecker
}

// NewGenerator creates a report Generator with the provided dependencies.
func NewGenerator(
	cfg GeneratorConfig,
	digestResolver DigestResolver,
	updateChecker UpdateChecker,
	sigVerifier SignatureVerifier,
	sbomDiscoverer SBOMDiscoverer,
	provenanceChecker ProvenanceChecker,
) *Generator {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	return &Generator{
		cfg:               cfg,
		digestResolver:    digestResolver,
		updateChecker:     updateChecker,
		sigVerifier:       sigVerifier,
		sbomDiscoverer:    sbomDiscoverer,
		provenanceChecker: provenanceChecker,
	}
}

// ImageInput is the data needed from discovery to generate an image record.
type ImageInput struct {
	Image        string
	Namespace    string
	WorkloadKind string
	WorkloadName string
}

// Generate produces a ProvenanceReport from discovered images and helm releases.
func (g *Generator) Generate(ctx context.Context, images []ImageInput, helmReleases []HelmSource, namespacesScanned []string) *ProvenanceReport {
	report := &ProvenanceReport{
		Metadata: ReportMetadata{
			GeneratedAt:       time.Now().UTC(),
			CollectorVersion:  Version,
			ClusterName:       g.cfg.ClusterName,
			NamespacesScanned: namespacesScanned,
		},
	}

	// Process images with bounded concurrency
	imageRecords := make([]ImageRecord, len(images))
	sem := make(chan struct{}, g.cfg.Concurrency)
	var wg sync.WaitGroup

	for i, img := range images {
		wg.Add(1)
		go func(idx int, input ImageInput) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			record := g.processImage(ctx, input)
			imageRecords[idx] = record
		}(i, img)
	}
	wg.Wait()

	report.Images = imageRecords

	// Process Helm releases
	for _, hr := range helmReleases {
		record := HelmRecord{
			ReleaseName: hr.ReleaseName,
			Namespace:   hr.Namespace,
			Chart:       hr.ChartName,
			Version:     hr.ChartVersion,
			AppVersion:  hr.AppVersion,
			Status:      hr.Status,
		}
		report.HelmReleases = append(report.HelmReleases, record)
	}

	// Compute summary
	report.Summary = g.computeSummary(report)

	return report
}

func (g *Generator) processImage(ctx context.Context, input ImageInput) ImageRecord {
	record := ImageRecord{
		Image:     input.Image,
		Namespace: input.Namespace,
		Workload: WorkloadRef{
			Kind: input.WorkloadKind,
			Name: input.WorkloadName,
		},
	}

	// Resolve digest
	if g.digestResolver != nil {
		digest, err := g.digestResolver.Resolve(ctx, input.Image)
		if err != nil {
			slog.Warn("failed to resolve digest", "image", input.Image, "error", err)
		} else {
			record.Digest = digest
		}
	}

	// Check for updates
	if g.cfg.CheckUpdates && g.updateChecker != nil {
		update, err := g.updateChecker.Check(ctx, input.Image)
		if err != nil {
			slog.Warn("failed to check updates", "image", input.Image, "error", err)
		} else if update != nil && update.UpdateAvailable {
			record.Update = update
		}
	}

	// Verify signatures
	if g.cfg.VerifySignatures && g.sigVerifier != nil {
		sig, err := g.sigVerifier.Verify(ctx, input.Image)
		if err != nil {
			slog.Warn("failed to verify signature", "image", input.Image, "error", err)
		} else {
			record.Signature = sig
		}
	}

	// Check for SBOM
	if g.sbomDiscoverer != nil {
		sbom, err := g.sbomDiscoverer.Discover(ctx, input.Image)
		if err != nil {
			slog.Warn("failed to check SBOM", "image", input.Image, "error", err)
		} else if sbom != nil && sbom.HasSBOM {
			record.SBOM = sbom
		}
	}

	// Check for SLSA provenance
	if g.provenanceChecker != nil {
		prov, err := g.provenanceChecker.Check(ctx, input.Image)
		if err != nil {
			slog.Warn("failed to check provenance", "image", input.Image, "error", err)
		} else if prov != nil && prov.HasProvenance {
			record.Provenance = prov
		}
	}

	return record
}

func (g *Generator) computeSummary(r *ProvenanceReport) ReportSummary {
	uniqueImages := make(map[string]bool)
	s := ReportSummary{
		TotalImages:       len(r.Images),
		TotalHelmReleases: len(r.HelmReleases),
	}

	for _, img := range r.Images {
		uniqueImages[img.Image] = true
		if img.Signature != nil && img.Signature.Signed {
			s.SignedImages++
			if img.Signature.Verified {
				s.VerifiedImages++
			}
		}
		if img.SBOM != nil && img.SBOM.HasSBOM {
			s.ImagesWithSBOM++
		}
		if img.Provenance != nil && img.Provenance.HasProvenance {
			s.ImagesWithProvenance++
		}
		if img.Update != nil && img.Update.UpdateAvailable {
			s.ImagesWithUpdates++
		}
	}
	s.UniqueImages = len(uniqueImages)

	for _, hr := range r.HelmReleases {
		if hr.Update != nil && hr.Update.UpdateAvailable {
			s.HelmReleasesWithUpdates++
		}
	}

	return s
}
