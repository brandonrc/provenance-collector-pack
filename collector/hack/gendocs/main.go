// gendocs renders docs/src/content/docs/configuration.md from internal/configspec.Vars.
//
// Usage:
//
//	go run ./hack/gendocs           # write docs/src/content/docs/configuration.md
//	go run ./hack/gendocs --check   # exit 1 if the file on disk is stale
//
// `--check` exists so CI can fail a PR that touches the spec but doesn't
// commit the regenerated doc (and vice versa).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"text/template"

	"github.com/nebari-dev/provenance-collector/internal/configspec"
)

const outputPath = "docs/src/content/docs/configuration.md"

var docTemplate = template.Must(template.New("doc").Funcs(template.FuncMap{
	"defaultCell": func(d string) string {
		if d == "" {
			return "*(empty)*"
		}
		// Render parenthesised placeholders (e.g. "(set by chart)") as plain
		// italics so they read as commentary, not as a literal value.
		if d != "" && d[0] == '(' && d[len(d)-1] == ')' {
			return "*" + d + "*"
		}
		return "`" + d + "`"
	},
}).Parse(`---
title: Configuration Reference
description: Environment variables read by the collector and dashboard binaries. Generated from internal/configspec/spec.go.
---

<!-- GENERATED FILE — do not edit by hand.
     Source of truth: internal/configspec/spec.go.
     Regenerate with: make docs (or: go run ./hack/gendocs)
     CI guards drift between this file and the spec. -->

> **Generated** from ` + "`internal/configspec/spec.go`" + `. Edit the spec and run ` + "`make docs`" + ` to update this file.

The collector and dashboard binaries are configured entirely via environment
variables. When deployed with the Helm chart, these are set through
` + "`values.yaml`" + ` and rendered into the relevant pod's env block.

## Collector

Read by ` + "`cmd/provenance-collector`" + ` (the CronJob).

| Variable | Type | Default | Description |
|---|---|---|---|
{{- range .Collector }}
| ` + "`{{ .Name }}`" + ` | {{ .Kind }} | {{ defaultCell .Default }} | {{ .Description }} |
{{- end }}

## Shared

Read by both binaries.

| Variable | Type | Default | Description |
|---|---|---|---|
{{- range .Shared }}
| ` + "`{{ .Name }}`" + ` | {{ .Kind }} | {{ defaultCell .Default }} | {{ .Description }} |
{{- end }}

## Dashboard

Read by ` + "`cmd/dashboard`" + ` (the web UI pod).

| Variable | Type | Default | Description |
|---|---|---|---|
{{- range .Dashboard }}
| ` + "`{{ .Name }}`" + ` | {{ .Kind }} | {{ defaultCell .Default }} | {{ .Description }} |
{{- end }}

## Value formats

### Duration

Duration values use Go's ` + "`time.ParseDuration`" + ` format:

- ` + "`30s`" + ` — 30 seconds
- ` + "`5m`" + ` — 5 minutes
- ` + "`1h30m`" + ` — 1 hour 30 minutes
- ` + "`168h`" + ` — 7 days

Where noted, ` + "`-1`" + ` disables the timeout / retention.

### Boolean

Boolean values accept ` + "`true`" + ` / ` + "`1`" + ` / ` + "`yes`" + ` / ` + "`on`" + ` (case-insensitive) as truthy. Anything else, including unset, is false.

### Bytes

Integer count of bytes (decimal). ` + "`16777216`" + ` is 16 MiB.

### String list

Comma-separated. Whitespace around commas is trimmed; empty entries are dropped.
`))

func main() {
	check := flag.Bool("check", false, "exit 1 if the generated output differs from the file on disk")
	flag.Parse()

	var collector, shared, dashboard []configspec.Var
	for _, v := range configspec.Vars {
		switch v.Scope {
		case configspec.ScopeCollector:
			collector = append(collector, v)
		case configspec.ScopeShared:
			shared = append(shared, v)
		case configspec.ScopeDashboard:
			dashboard = append(dashboard, v)
		default:
			fmt.Fprintf(os.Stderr, "gendocs: var %q has unknown scope %q\n", v.Name, v.Scope)
			os.Exit(2)
		}
	}

	var buf bytes.Buffer
	if err := docTemplate.Execute(&buf, map[string]any{
		"Collector": collector,
		"Shared":    shared,
		"Dashboard": dashboard,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs: render failed:", err)
		os.Exit(2)
	}

	want := buf.Bytes()

	if *check {
		got, err := os.ReadFile(outputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gendocs --check:", err)
			os.Exit(1)
		}
		if !bytes.Equal(got, want) {
			fmt.Fprintf(os.Stderr, "gendocs --check: %s is stale relative to internal/configspec/spec.go.\n", outputPath)
			fmt.Fprintln(os.Stderr, "Run `make docs` (or `go run ./hack/gendocs`) and commit the result.")
			os.Exit(1)
		}
		return
	}

	if err := os.WriteFile(outputPath, want, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs: write failed:", err)
		os.Exit(2)
	}
	fmt.Printf("gendocs: wrote %s (%d entries)\n", outputPath, len(configspec.Vars))
}
