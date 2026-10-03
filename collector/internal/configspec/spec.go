// Package configspec is the single source of truth for the env vars the
// collector and dashboard binaries read at startup.
//
// Adding or renaming an env var means editing this file. The doc generator
// (hack/gendocs) renders docs/configuration.md from Vars, and the static
// check (hack/checkenvs) fails the build if any os.Getenv call under cmd/
// or internal/ refers to a name not present here. Together those two keep
// the docs and the code from drifting apart again.
//
// This package only describes the env vars; the actual parsing still lives
// in internal/config and cmd/dashboard. A future refactor can bind those
// parsers through Vars too (see issue #35), but the immediate goal is just
// to stop the documentation bleed.
package configspec

// Scope captures which binary reads a given env var. "shared" means both.
type Scope string

const (
	ScopeCollector Scope = "collector"
	ScopeDashboard Scope = "dashboard"
	ScopeShared    Scope = "shared"
)

// Kind describes the type the env var's string value is parsed into. The
// generator uses it for the "Type" column; the parsers in main.go choose
// the right conversion routine themselves.
type Kind string

const (
	KindString     Kind = "string"
	KindBool       Kind = "bool"
	KindDuration   Kind = "duration"
	KindBytes      Kind = "bytes"
	KindStringList Kind = "string list"
)

// Var is one env-var entry.
type Var struct {
	Name        string
	Scope       Scope
	Kind        Kind
	Default     string // human-readable; "" means render as "(empty)"
	Description string
}

// Vars is the canonical list. Order within a scope matters for the generated
// doc — keep logically related vars adjacent.
var Vars = []Var{
	// ----------------------------------------------------------------
	// Collector — what to scan
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_NAMESPACES",
		Scope:       ScopeCollector,
		Kind:        KindStringList,
		Default:     "",
		Description: "Comma-separated namespaces to scan. Empty means scan all namespaces.",
	},
	{
		Name:        "PROVENANCE_EXCLUDE_NAMESPACES",
		Scope:       ScopeCollector,
		Kind:        KindStringList,
		Default:     "",
		Description: "Comma-separated namespaces to exclude from the scan.",
	},

	// ----------------------------------------------------------------
	// Collector — registry + update checks
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_REGISTRY_AUTH",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "",
		Description: "Path to a Docker `config.json` for private registry authentication.",
	},
	{
		Name:        "PROVENANCE_REGISTRY_TIMEOUT",
		Scope:       ScopeCollector,
		Kind:        KindDuration,
		Default:     "30s",
		Description: "Timeout for registry operations (digest resolution, tag listing).",
	},
	{
		Name:        "PROVENANCE_CHECK_UPDATES",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Check registries for newer semver tags.",
	},
	{
		Name:        "PROVENANCE_UPDATE_LEVEL",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "patch",
		Description: "Minimum version bump to flag as an update: `patch`, `minor`, or `major`.",
	},
	{
		Name:        "PROVENANCE_SKIP_PRERELEASE",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Ignore alpha / beta / RC versions when checking for updates.",
	},

	// ----------------------------------------------------------------
	// Collector — supply-chain checks
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_VERIFY_SIGNATURES",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Check for cosign signatures on images.",
	},
	{
		Name:        "PROVENANCE_COSIGN_PUBLIC_KEY",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "",
		Description: "Path or KMS URI for the cosign public key. Empty = existence check only, no trust-chain verification.",
	},
	{
		Name:        "PROVENANCE_CHECK_SBOM",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Check for SBOM attestations on discovered images.",
	},
	{
		Name:        "PROVENANCE_CHECK_PROVENANCE",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Check for SLSA provenance attestations on discovered images.",
	},
	{
		Name:        "PROVENANCE_HELM_ENABLED",
		Scope:       ScopeCollector,
		Kind:        KindBool,
		Default:     "true",
		Description: "Discover deployed Helm releases.",
	},

	// ----------------------------------------------------------------
	// Collector — report sink
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_REPORT_OUTPUT",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "http",
		Description: "Report output type: `http` (POST to the dashboard's internal endpoint), `pvc` (write to a shared PVC), or `configmap`.",
	},
	{
		Name:        "PROVENANCE_REPORT_CONFIGMAP",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "provenance-report",
		Description: "ConfigMap name used by the `configmap` report output type.",
	},
	{
		Name:        "PROVENANCE_REPORT_CONFIGMAP_NAMESPACE",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "default",
		Description: "Namespace for the report ConfigMap. Typically overridden by the chart to the release namespace.",
	},
	{
		Name:        "PROVENANCE_REPORT_UPLOAD_URL",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "(set by chart)",
		Description: "Full URL the collector POSTs to in `http` mode, e.g. `http://provenance-collector-web-internal.<ns>.svc:8081/internal/reports`.",
	},
	{
		Name:        "PROVENANCE_REPORT_UPLOAD_TIMEOUT",
		Scope:       ScopeCollector,
		Kind:        KindDuration,
		Default:     "30s",
		Description: "Timeout for the report upload request in `http` mode.",
	},

	// ----------------------------------------------------------------
	// Collector — metadata + Kubernetes
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_CLUSTER_NAME",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "",
		Description: "Cluster name included in report metadata. Informational only.",
	},
	{
		Name:        "KUBECONFIG",
		Scope:       ScopeCollector,
		Kind:        KindString,
		Default:     "",
		Description: "Path to a kubeconfig file. Empty = use in-cluster credentials (the normal case for the chart-deployed CronJob).",
	},

	// ----------------------------------------------------------------
	// Shared — read by both the collector and the dashboard
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_REPORT_PATH",
		Scope:       ScopeShared,
		Kind:        KindString,
		Default:     "/reports",
		Description: "Filesystem path used by the dashboard pod (always) and the collector pod (only in `pvc` mode).",
	},
	{
		Name:        "PROVENANCE_REPORT_RETENTION",
		Scope:       ScopeShared,
		Kind:        KindDuration,
		Default:     "168h",
		Description: "Auto-prune reports older than this. Use `-1` to disable cleanup and keep all reports.",
	},

	// ----------------------------------------------------------------
	// Dashboard — server addresses
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_DASHBOARD_ADDR",
		Scope:       ScopeDashboard,
		Kind:        KindString,
		Default:     ":8080",
		Description: "Listen address for the public HTTP server (UI + read API).",
	},
	{
		Name:        "PROVENANCE_DASHBOARD_INTERNAL_ADDR",
		Scope:       ScopeDashboard,
		Kind:        KindString,
		Default:     ":8081",
		Description: "Listen address for the internal upload endpoint. Never put this behind an Ingress.",
	},
	{
		Name:        "PROVENANCE_UPLOAD_MAX_BYTES",
		Scope:       ScopeDashboard,
		Kind:        KindBytes,
		Default:     "16777216",
		Description: "Max body size in bytes accepted on the internal upload endpoint. Default ~16 MiB.",
	},

	// ----------------------------------------------------------------
	// Dashboard — auth (Run Scan button + /api/me)
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_OIDC_ISSUER",
		Scope:       ScopeDashboard,
		Kind:        KindString,
		Default:     "",
		Description: "OIDC issuer URL the dashboard calls for userinfo (e.g. `https://keycloak.example.com/realms/nebari`). Empty = auth disabled.",
	},
	{
		Name:        "PROVENANCE_ADMIN_GROUPS",
		Scope:       ScopeDashboard,
		Kind:        KindStringList,
		Default:     "",
		Description: "Comma-separated OIDC groups whose members may trigger a scan via the dashboard's Run Scan button.",
	},
	{
		Name:        "PROVENANCE_MANUAL_JOB_TTL",
		Scope:       ScopeDashboard,
		Kind:        KindDuration,
		Default:     "1h",
		Description: "TTL after which dashboard-triggered Jobs are auto-cleaned. `0` (or any zero-duration string) keeps Jobs forever.",
	},

	// ----------------------------------------------------------------
	// Dashboard — scan endpoint wiring
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_NAMESPACE",
		Scope:       ScopeDashboard,
		Kind:        KindString,
		Default:     "(set by chart)",
		Description: "Namespace the dashboard runs in. Used to address the CronJob from `/api/scan`. The chart populates this via the downward API.",
	},
	{
		Name:        "PROVENANCE_CRONJOB_NAME",
		Scope:       ScopeDashboard,
		Kind:        KindString,
		Default:     "(set by chart)",
		Description: "Name of the CronJob the dashboard creates manual Jobs from. The chart sets this to the release's full name.",
	},

	// ----------------------------------------------------------------
	// Dashboard — feature flags
	// ----------------------------------------------------------------
	{
		Name:        "PROVENANCE_FEATURE_TIMELINE_DELTAS",
		Scope:       ScopeDashboard,
		Kind:        KindBool,
		Default:     "false",
		Description: "Opt-in: render a `+N / -N` unique-image delta badge on each timeline card vs the previous scan. Exposed in the chart as `webUI.features.timelineDeltas`.",
	},
}

// ByName looks up an entry by env-var name. Returns false if the name is not
// in Vars — the static check uses this to find references in source that
// haven't been added to the spec yet.
func ByName(name string) (Var, bool) {
	for _, v := range Vars {
		if v.Name == name {
			return v, true
		}
	}
	return Var{}, false
}

// Names returns every env-var name in Vars, in declaration order. Convenience
// for the static checker.
func Names() []string {
	out := make([]string, 0, len(Vars))
	for _, v := range Vars {
		out = append(out, v.Name)
	}
	return out
}
