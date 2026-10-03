// Package reportschema generates the JSON Schema for the collector's report
// document (internal/report.ProvenanceReport). The schema is committed at
// schema/report.schema.json and kept in sync by `go run ./hack/genschema
// --check` in CI and by this package's tests.
//
// It is a separate package so the collector binary does not link the schema
// generator.
package reportschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/invopop/jsonschema"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

// Path is where the schema is committed, relative to the module root.
const Path = "schema/report.schema.json"

// ID is the schema's $id: its raw URL on the default branch.
const ID = "https://raw.githubusercontent.com/nebari-dev/provenance-collector-pack/main/" + Path

const modulePath = "github.com/nebari-dev/provenance-collector"

// Generate renders the schema. It reads Go doc comments from internal/report
// for field descriptions, so it must run with the module root as the working
// directory.
func Generate() ([]byte, error) {
	r := &jsonschema.Reflector{
		// A consumer must accept reports from newer minor versions, which may
		// carry fields this schema does not list.
		AllowAdditionalProperties: true,
	}
	if err := r.AddGoComments(modulePath, "./internal/report"); err != nil {
		return nil, fmt.Errorf("reading doc comments: %w", err)
	}

	s := r.Reflect(&report.ProvenanceReport{})
	s.ID = jsonschema.ID(ID)
	s.Title = "Provenance collector report"
	s.Description = fmt.Sprintf(
		"Report written by provenance-collector (schemaVersion %s). Consumers should accept any report "+
			"whose metadata.schemaVersion has the same major version and ignore unknown fields.",
		report.SchemaVersion)

	meta, ok := s.Definitions["ReportMetadata"]
	if !ok {
		return nil, fmt.Errorf("ReportMetadata definition missing from reflected schema")
	}
	sv, ok := meta.Properties.Get("schemaVersion")
	if !ok {
		return nil, fmt.Errorf("metadata.schemaVersion missing from reflected schema")
	}
	major := regexp.MustCompile(`^\d+`).FindString(report.SchemaVersion)
	sv.Pattern = `^` + major + `\.[0-9]+\.[0-9]+$`
	sv.Examples = []any{report.SchemaVersion}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write(data)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
