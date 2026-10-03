package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/provenance-collector/internal/config"
	"github.com/nebari-dev/provenance-collector/internal/discovery"
	"github.com/nebari-dev/provenance-collector/internal/report"
)

// The golden report lives at the repository root so other tools (the security
// posture pack's contract test) can consume it next to schema/.
const goldenPath = "../../testdata/report.golden.json"

var update = flag.Bool("update", false, "rewrite testdata/report.golden.json from the current code")

func TestParseFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    options
		wantErr bool
	}{
		{name: "default keeps the configured sink", args: nil, want: options{}},
		{name: "stdout", args: []string{"--output", "-"}, want: options{output: "-"}},
		{name: "file", args: []string{"--output=/tmp/r.json"}, want: options{output: "/tmp/r.json"}},
		{name: "once is not a flag", args: []string{"--once"}, wantErr: true},
		{name: "unknown flag", args: []string{"--bogus"}, wantErr: true},
		{name: "positional argument", args: []string{"extra"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFlags(tc.args, io.Discard)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseFlagsHelp(t *testing.T) {
	var usage bytes.Buffer
	_, err := parseFlags([]string{"-h"}, &usage)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("want flag.ErrHelp, got %v", err)
	}
	if !strings.Contains(usage.String(), "--output") && !strings.Contains(usage.String(), "-output") {
		t.Errorf("usage does not mention --output:\n%s", usage.String())
	}
}

func TestLogWriterKeepsStdoutForTheReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if logWriter(options{output: "-"}, &stdout, &stderr) != &stderr {
		t.Error("logs must go to stderr when the report goes to stdout")
	}
	if logWriter(options{output: "/tmp/r.json"}, &stdout, &stderr) != &stdout {
		t.Error("logs should stay on stdout when the report goes to a file")
	}
	if logWriter(options{}, &stdout, &stderr) != &stdout {
		t.Error("default logging target changed")
	}
}

func sampleReport() *report.ProvenanceReport {
	return &report.ProvenanceReport{
		Metadata: report.ReportMetadata{GeneratedAt: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), CollectorVersion: "test"},
		Images: []report.ImageRecord{{
			Image: "nginx:1.27", Digest: "sha256:abc", Namespace: "default",
			Workload:  report.WorkloadRef{Kind: "ReplicaSet", Name: "nginx-5d4f"},
			Signature: &report.SignatureInfo{Signed: true},
		}},
		Summary: report.ReportSummary{TotalImages: 1, UniqueImages: 1, SignedImages: 1},
	}
}

// --output bypasses the http/pvc/configmap sink entirely: no upload URL is
// needed and nothing touches the cluster.
func TestSelectWriterOutputStdout(t *testing.T) {
	cfg := &config.Config{ReportOutput: "http"} // no upload URL: would fail without --output
	var stdout bytes.Buffer
	w, err := selectWriter(cfg, options{output: "-"}, fake.NewClientset(), &stdout)
	if err != nil {
		t.Fatalf("selectWriter: %v", err)
	}
	if err := w.Write(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var got report.ProvenanceReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the report: %v", err)
	}
	if got.Summary.SignedImages != 1 || got.Images[0].Workload.Kind != "ReplicaSet" {
		t.Errorf("unexpected report %+v", got)
	}
}

func TestSelectWriterOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	client := fake.NewClientset()
	w, err := selectWriter(&config.Config{ReportOutput: "configmap"}, options{output: path}, client, io.Discard)
	if err != nil {
		t.Fatalf("selectWriter: %v", err)
	}
	if err := w.Write(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("report file not written: %v", err)
	}
	cms, err := client.CoreV1().ConfigMaps("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing configmaps: %v", err)
	}
	if len(cms.Items) != 0 {
		t.Error("--output must not also write the configmap sink")
	}
}

func TestSelectWriterDefaultBehaviourUnchanged(t *testing.T) {
	if _, err := selectWriter(&config.Config{ReportOutput: "http"}, options{}, nil, io.Discard); err == nil {
		t.Error("http mode without an upload URL must still fail")
	}
	if _, err := selectWriter(&config.Config{ReportOutput: "nope"}, options{}, nil, io.Discard); err == nil {
		t.Error("unknown output mode must still fail")
	}
	cases := map[string]struct {
		cfg  config.Config
		want string
	}{
		"pvc":       {config.Config{ReportOutput: "pvc", ReportPath: t.TempDir()}, "*report.PVCWriter"},
		"configmap": {config.Config{ReportOutput: "configmap"}, "*report.ConfigMapWriter"},
		"http":      {config.Config{ReportOutput: "http", ReportUploadURL: "http://dash/internal/reports"}, "*report.HTTPWriter"},
	}
	for name, tc := range cases {
		w, err := selectWriter(&tc.cfg, options{}, fake.NewClientset(), io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := fmt.Sprintf("%T", w); got != tc.want {
			t.Errorf("%s mode returned %s, want %s", name, got, tc.want)
		}
	}
}

// --- end-to-end run() against a fake cluster and stub registries ---

func boolPtr(b bool) *bool { return &b }

func pod(ns, name, ownerKind, ownerName string, containers, initContainers []string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	if ownerKind != "" {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: ownerKind, Name: ownerName, Controller: boolPtr(true)}}
	}
	for i, img := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: fmt.Sprintf("c%d", i), Image: img})
	}
	for i, img := range initContainers {
		p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{Name: fmt.Sprintf("init%d", i), Image: img})
	}
	return p
}

func ns(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// fakeCluster is a small but representative cluster: a Deployment-owned
// ReplicaSet with an init container, a StatefulSet, a bare Pod, an image used
// by two workloads, and an excluded namespace.
func fakeCluster() []runtime.Object {
	return []runtime.Object{
		ns("default"), ns("monitoring"), ns("kube-system"),
		pod("default", "web-7d9f-abcde", "ReplicaSet", "web-7d9f", []string{"ghcr.io/example/web:1.4.2"}, []string{"busybox:1.36"}),
		pod("default", "debug", "", "", []string{"ghcr.io/example/unreachable:0.1.0"}, nil),
		pod("monitoring", "prometheus-0", "StatefulSet", "prometheus", []string{"quay.io/prometheus/prometheus:v2.53.0", "busybox:1.36"}, nil),
		pod("kube-system", "coredns-1", "ReplicaSet", "coredns-5d78", []string{"registry.k8s.io/coredns/coredns:v1.11.1"}, nil),
	}
}

type stubDigests map[string]string

func (s stubDigests) Resolve(_ context.Context, ref string) (string, error) {
	if d, ok := s[ref]; ok {
		return d, nil
	}
	return "", fmt.Errorf("resolving digest for %s: GET https://%s: 401 Unauthorized", ref, strings.SplitN(ref, "/", 2)[0])
}

type stubUpdates map[string]*report.UpdateInfo

func (s stubUpdates) Check(_ context.Context, ref string) (*report.UpdateInfo, error) {
	if u, ok := s[ref]; ok {
		return u, nil
	}
	return &report.UpdateInfo{}, nil
}

type stubSignatures map[string]*report.SignatureInfo

func (s stubSignatures) Verify(_ context.Context, ref string) (*report.SignatureInfo, error) {
	if v, ok := s[ref]; ok {
		return v, nil
	}
	return &report.SignatureInfo{}, nil
}

type stubSBOM map[string]string

func (s stubSBOM) Discover(_ context.Context, ref string) (*report.SBOMInfo, error) {
	if f, ok := s[ref]; ok {
		return &report.SBOMInfo{HasSBOM: true, Format: f}, nil
	}
	return &report.SBOMInfo{}, nil
}

type stubProvenance map[string]string

func (s stubProvenance) Check(_ context.Context, ref string) (*report.ProvenanceInfo, error) {
	if pt, ok := s[ref]; ok {
		return &report.ProvenanceInfo{HasProvenance: true, PredicateType: pt}, nil
	}
	return &report.ProvenanceInfo{}, nil
}

type stubHelm struct {
	releases []discovery.DiscoveredRelease
	err      error
}

func (s stubHelm) Discover(context.Context) ([]discovery.DiscoveredRelease, error) {
	return s.releases, s.err
}

func stubEnrichers(*config.Config) (enrichers, error) {
	return enrichers{
		digests: stubDigests{
			"ghcr.io/example/web:1.4.2":               "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			"busybox:1.36":                            "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			"quay.io/prometheus/prometheus:v2.53.0":   "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			"registry.k8s.io/coredns/coredns:v1.11.1": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		},
		updates: stubUpdates{
			"ghcr.io/example/web:1.4.2": {CurrentTag: "1.4.2", LatestInMajor: "1.6.0", NewestAvailable: "2.0.1", UpdateAvailable: true},
		},
		signatures: stubSignatures{
			"ghcr.io/example/web:1.4.2":               {Signed: true, Verified: true},
			"registry.k8s.io/coredns/coredns:v1.11.1": {Signed: true},
			"ghcr.io/example/unreachable:0.1.0":       {Error: "fetching signed entity: GET https://ghcr.io: 401 Unauthorized"},
		},
		sbom: stubSBOM{
			"ghcr.io/example/web:1.4.2":             "spdx",
			"quay.io/prometheus/prometheus:v2.53.0": "cyclonedx",
		},
		provenance: stubProvenance{
			"ghcr.io/example/web:1.4.2": "https://slsa.dev/provenance/v1",
		},
	}, nil
}

func testDeps(stdout io.Writer, objs ...runtime.Object) deps {
	return deps{
		stdout: stdout,
		kube: func(string) (kubernetes.Interface, error) {
			return fake.NewClientset(objs...), nil
		},
		helm: func(kubernetes.Interface, []string, []string) discovery.HelmDiscoverer {
			return stubHelm{
				releases: []discovery.DiscoveredRelease{
					{Name: "prometheus", Namespace: "monitoring", ChartName: "prometheus", ChartVersion: "25.8.0", AppVersion: "v2.53.0", Status: "deployed"},
					{Name: "web", Namespace: "default", ChartName: "web", ChartVersion: "0.3.1", AppVersion: "1.4.2", Status: "failed"},
				},
				err: errors.Join(&discovery.NamespaceError{
					Namespace: "kube-system",
					Err:       errors.New(`secrets is forbidden: User "system:serviceaccount:provenance:collector" cannot list resource "secrets"`),
				}),
			}
		},
		enrichers: stubEnrichers,
		now:       func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) },
	}
}

func goldenConfig() *config.Config {
	return &config.Config{
		ExcludeNamespaces: []string{"kube-public"},
		VerifySignatures:  true,
		CheckUpdates:      true,
		CheckSBOM:         true,
		CheckProvenance:   true,
		HelmEnabled:       true,
		ReportOutput:      "http", // no upload URL: --output must bypass it
		ClusterName:       "golden",
	}
}

// TestRunGoldenReport runs a whole collection with --output <file> and
// compares the result to testdata/report.golden.json. That file is the
// published example of the report contract: other tools test against it, and
// schema/report.schema.json must accept it. Regenerate with
//
//	go test ./cmd/provenance-collector -run Golden -update
func TestRunGoldenReport(t *testing.T) {
	oldVersion := report.Version
	report.Version = "v0.0.0-golden"
	t.Cleanup(func() { report.Version = oldVersion })

	out := filepath.Join(t.TempDir(), "report.json")
	if err := run(context.Background(), goldenConfig(), options{output: out}, testDeps(io.Discard, fakeCluster()...)); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}

	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", goldenPath)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("report differs from %s; if the change is intended, bump report.SchemaVersion as needed and run with -update.\n--- got ---\n%s", goldenPath, got)
	}

	// Spot-check the contract rather than trusting the golden file blindly.
	var r report.ProvenanceReport
	if err := json.Unmarshal(got, &r); err != nil {
		t.Fatal(err)
	}
	if r.Metadata.SchemaVersion != report.SchemaVersion {
		t.Errorf("schemaVersion = %q", r.Metadata.SchemaVersion)
	}
	if r.Summary.TotalImages != 6 || r.Summary.UniqueImages != 5 || r.Summary.TotalHelmReleases != 2 {
		t.Errorf("summary = %+v", r.Summary)
	}
	wantWarnings := []string{
		"helm: listing helm releases in namespace kube-system: secrets is forbidden",
		"image ghcr.io/example/unreachable:0.1.0: digest not resolved",
	}
	if len(r.Warnings) != len(wantWarnings) {
		t.Fatalf("warnings = %q", r.Warnings)
	}
	for i, w := range wantWarnings {
		if !strings.HasPrefix(r.Warnings[i], w) {
			t.Errorf("warnings[%d] = %q, want prefix %q", i, r.Warnings[i], w)
		}
	}
}

// With --output - the report is the only thing on stdout.
func TestRunOutputStdout(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), goldenConfig(), options{output: "-"}, testDeps(&stdout, fakeCluster()...)); err != nil {
		t.Fatalf("run: %v", err)
	}
	dec := json.NewDecoder(&stdout)
	var r report.ProvenanceReport
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("stdout is not a report: %v", err)
	}
	if dec.More() {
		t.Error("stdout holds more than one JSON document")
	}
	if r.Metadata.ClusterName != "golden" {
		t.Errorf("clusterName = %q", r.Metadata.ClusterName)
	}
}

func TestRunHelmDisabledAndNoEnrichment(t *testing.T) {
	cfg := goldenConfig()
	cfg.HelmEnabled = false
	cfg.Namespaces = []string{"monitoring"}
	d := testDeps(nil, fakeCluster()...)
	d.helm = func(kubernetes.Interface, []string, []string) discovery.HelmDiscoverer {
		t.Fatal("helm discovery must not run when disabled")
		return nil
	}
	d.enrichers = func(*config.Config) (enrichers, error) { return enrichers{}, nil }
	out := filepath.Join(t.TempDir(), "r.json")
	if err := run(context.Background(), cfg, options{output: out}, d); err != nil {
		t.Fatalf("run: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var r report.ProvenanceReport
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.HelmReleases) != 0 || len(r.Warnings) != 0 {
		t.Errorf("unexpected helm data or warnings: %+v", r)
	}
	if len(r.Metadata.NamespacesScanned) != 1 || r.Metadata.NamespacesScanned[0] != "monitoring" {
		t.Errorf("namespacesScanned = %v", r.Metadata.NamespacesScanned)
	}
	if r.Images[0].Digest != "" || r.Images[0].Signature != nil {
		t.Errorf("no enrichers configured, got %+v", r.Images[0])
	}
}

func TestRunErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]struct {
		output string
		cfg    func(*config.Config)
		mutate func(*deps)
		want   string
	}{
		"kube client": {
			output: "-",
			mutate: func(d *deps) { d.kube = func(string) (kubernetes.Interface, error) { return nil, boom } },
			want:   "boom",
		},
		"enrichers": {
			output: "-",
			mutate: func(d *deps) { d.enrichers = func(*config.Config) (enrichers, error) { return enrichers{}, boom } },
			want:   "boom",
		},
		"sink misconfigured": {
			cfg:  func(c *config.Config) { c.ReportOutput = "carrier-pigeon" },
			want: "unknown PROVENANCE_REPORT_OUTPUT",
		},
		"unwritable output": {
			output: filepath.Join(t.TempDir(), "missing", "r.json"),
			want:   "creating temp file",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := goldenConfig()
			if tc.cfg != nil {
				tc.cfg(cfg)
			}
			d := testDeps(io.Discard, fakeCluster()...)
			if tc.mutate != nil {
				tc.mutate(&d)
			}
			err := run(context.Background(), cfg, options{output: tc.output}, d)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// A cancelled collection must not leave a report behind.
func TestRunCancelledWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := testDeps(io.Discard, fakeCluster()...)
	d.helm = func(kubernetes.Interface, []string, []string) discovery.HelmDiscoverer {
		cancel() // SIGTERM arrives mid-run
		return stubHelm{}
	}
	out := filepath.Join(t.TempDir(), "r.json")
	err := run(ctx, goldenConfig(), options{output: out}, d)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no report should be written after cancellation (stat: %v)", statErr)
	}
}

func TestHelmWarnings(t *testing.T) {
	if got := helmWarnings(errors.New("listing namespaces: forbidden")); len(got) != 1 || got[0] != "helm: listing namespaces: forbidden" {
		t.Errorf("single error: %q", got)
	}
	joined := errors.Join(
		&discovery.NamespaceError{Namespace: "a", Err: errors.New("x")},
		&discovery.NamespaceError{Namespace: "b", Err: errors.New("y")},
	)
	got := helmWarnings(joined)
	if len(got) != 2 || !strings.Contains(got[1], "namespace b") {
		t.Errorf("joined errors: %q", got)
	}
}

func TestRegistryEnrichers(t *testing.T) {
	e, err := registryEnrichers(&config.Config{VerifySignatures: true, CheckUpdates: true, CheckSBOM: true, CheckProvenance: true})
	if err != nil {
		t.Fatal(err)
	}
	if e.digests == nil || e.updates == nil || e.signatures == nil || e.sbom == nil || e.provenance == nil {
		t.Errorf("all enrichers should be enabled: %+v", e)
	}
	e, err = registryEnrichers(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if e.digests == nil || e.updates != nil || e.signatures != nil || e.sbom != nil || e.provenance != nil {
		t.Errorf("only digest resolution is unconditional: %+v", e)
	}
	if _, err := registryEnrichers(&config.Config{RegistryCAFile: "/nonexistent/ca.pem"}); err == nil {
		t.Error("an unreadable CA file must fail at startup")
	}
}
