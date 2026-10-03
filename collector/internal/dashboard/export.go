package dashboard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// handleExport returns the selected report rendered as CSV or Markdown.
// Query parameters:
//
//	format=csv|markdown|md (default csv)
//	filename=<timestamped json> (default provenance-latest.json)
//
// The filename param lets the dashboard export whichever report the user is
// currently viewing in the timeline, not just the latest. Path traversal is
// rejected with the same rules as /api/reports/{filename}.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = "provenance-latest.json"
	} else if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	rpt, err := s.loadReport(filename)
	if err != nil {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}

	switch format {
	case "csv":
		s.exportCSV(w, rpt)
	case "markdown", "md":
		s.exportMarkdown(w, rpt)
	default:
		http.Error(w, "unsupported format: use csv or markdown", http.StatusBadRequest)
	}
}

func (s *Server) exportCSV(w http.ResponseWriter, rpt *report.ProvenanceReport) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=provenance-report.csv")

	var b strings.Builder
	b.WriteString("Image,Namespace,Workload Kind,Workload Name,Digest,Signed,Verified,SLSA Provenance,SBOM,SBOM Format,Update Available,Current Tag,Latest In Major\n")

	for _, img := range rpt.Images {
		signed, verified := "false", "false"
		if img.Signature != nil {
			signed = fmt.Sprintf("%t", img.Signature.Signed)
			verified = fmt.Sprintf("%t", img.Signature.Verified)
		}

		slsa := "false"
		if img.Provenance != nil {
			slsa = fmt.Sprintf("%t", img.Provenance.HasProvenance)
		}

		sbom, sbomFmt := "false", ""
		if img.SBOM != nil {
			sbom = fmt.Sprintf("%t", img.SBOM.HasSBOM)
			sbomFmt = img.SBOM.Format
		}

		updateAvail, currentTag, latestInMajor := "false", "", ""
		if img.Update != nil {
			updateAvail = fmt.Sprintf("%t", img.Update.UpdateAvailable)
			currentTag = img.Update.CurrentTag
			latestInMajor = img.Update.LatestInMajor
		}

		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			csvEscape(img.Image),
			csvEscape(img.Namespace),
			csvEscape(img.Workload.Kind),
			csvEscape(img.Workload.Name),
			csvEscape(img.Digest),
			signed, verified, slsa, sbom,
			csvEscape(sbomFmt),
			updateAvail,
			csvEscape(currentTag),
			csvEscape(latestInMajor),
		)
	}

	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) exportMarkdown(w http.ResponseWriter, rpt *report.ProvenanceReport) {
	w.Header().Set("Content-Type", "text/markdown")
	w.Header().Set("Content-Disposition", "attachment; filename=provenance-report.md")

	var b strings.Builder

	b.WriteString("# Provenance Report\n\n")
	fmt.Fprintf(&b, "**Generated:** %s\n\n", rpt.Metadata.GeneratedAt.Format("2006-01-02 15:04:05 UTC"))
	if rpt.Metadata.ClusterName != "" {
		fmt.Fprintf(&b, "**Cluster:** %s\n\n", rpt.Metadata.ClusterName)
	}
	fmt.Fprintf(&b, "**Namespaces:** %s\n\n", strings.Join(rpt.Metadata.NamespacesScanned, ", "))

	s2 := rpt.Summary
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Count |\n|---|---|\n")
	fmt.Fprintf(&b, "| Unique Images | %d |\n", s2.UniqueImages)
	fmt.Fprintf(&b, "| Signed | %d |\n", s2.SignedImages)
	fmt.Fprintf(&b, "| Verified | %d |\n", s2.VerifiedImages)
	fmt.Fprintf(&b, "| SLSA Provenance | %d |\n", s2.ImagesWithProvenance)
	fmt.Fprintf(&b, "| With SBOM | %d |\n", s2.ImagesWithSBOM)
	fmt.Fprintf(&b, "| Updates Available | %d |\n", s2.ImagesWithUpdates)
	fmt.Fprintf(&b, "| Helm Releases | %d |\n", s2.TotalHelmReleases)
	b.WriteString("\n")

	b.WriteString("## Container Images\n\n")
	b.WriteString("| Image | Namespace | Workload | Signed | SLSA | SBOM | Update |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")

	for _, img := range rpt.Images {
		signed := "-"
		if img.Signature != nil {
			switch {
			case img.Signature.Verified:
				signed = "Verified"
			case img.Signature.Signed:
				signed = "Signed"
			default:
				signed = "No"
			}
		}

		slsa := "-"
		if img.Provenance != nil {
			if img.Provenance.HasProvenance {
				slsa = "Yes"
			} else {
				slsa = "No"
			}
		}

		sbom := "-"
		if img.SBOM != nil {
			if img.SBOM.HasSBOM {
				sbom = strings.ToUpper(img.SBOM.Format)
			} else {
				sbom = "No"
			}
		}

		update := "-"
		if img.Update != nil {
			if img.Update.UpdateAvailable {
				update = img.Update.LatestInMajor
				if update == "" {
					update = img.Update.NewestAvailable
				}
			} else {
				update = "Current"
			}
		}

		workload := fmt.Sprintf("%s/%s", img.Workload.Kind, img.Workload.Name)
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
			img.Image, img.Namespace, workload, signed, slsa, sbom, update)
	}

	if len(rpt.HelmReleases) > 0 {
		b.WriteString("\n## Helm Releases\n\n")
		b.WriteString("| Release | Namespace | Chart | Version | App Version | Status |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, hr := range rpt.HelmReleases {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				hr.ReleaseName, hr.Namespace, hr.Chart, hr.Version, hr.AppVersion, hr.Status)
		}
	}

	_, _ = w.Write([]byte(b.String()))
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}
