package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nebari-dev/provenance-collector/internal/config"
	"github.com/nebari-dev/provenance-collector/internal/report"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestParseFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    options
		wantErr bool
	}{
		{name: "default keeps the configured sink", args: nil, want: options{}},
		{name: "once implies stdout", args: []string{"--once"}, want: options{once: true, output: "-"}},
		{name: "once with a file", args: []string{"--once", "--output", "/tmp/r.json"}, want: options{once: true, output: "/tmp/r.json"}},
		{name: "output alone", args: []string{"--output=-"}, want: options{output: "-"}},
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

func TestLogWriterKeepsStdoutForTheReport(t *testing.T) {
	if logWriter(options{output: "-"}) != os.Stderr {
		t.Error("logs must go to stderr when the report goes to stdout")
	}
	if logWriter(options{}) != os.Stdout {
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

// --once / --output bypass the http/pvc/configmap sink entirely: no upload
// URL is needed and nothing touches the cluster.
func TestSelectWriterOutputStdout(t *testing.T) {
	cfg := &config.Config{ReportOutput: "http"} // no upload URL: would fail without --output
	var stdout bytes.Buffer
	w, err := selectWriter(cfg, options{once: true, output: "-"}, fake.NewSimpleClientset(), &stdout)
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
	client := fake.NewSimpleClientset()
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
	cms, _ := client.CoreV1().ConfigMaps("").List(context.Background(), metav1ListAll)
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
	w, err := selectWriter(&config.Config{ReportOutput: "pvc", ReportPath: t.TempDir()}, options{}, nil, io.Discard)
	if err != nil {
		t.Fatalf("pvc: %v", err)
	}
	if _, ok := w.(*report.PVCWriter); !ok {
		t.Errorf("pvc mode returned %T", w)
	}
}

var metav1ListAll = metav1.ListOptions{}
