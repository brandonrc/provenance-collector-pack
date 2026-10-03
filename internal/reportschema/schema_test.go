package reportschema

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/nebari-dev/provenance-collector/internal/report"
)

const goldenReport = "testdata/report.golden.json"

func compile(t *testing.T) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(ID, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(ID)
	if err != nil {
		t.Fatalf("compiling %s: %v", Path, err)
	}
	return s
}

func load(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The committed schema must match what the types generate.
func TestSchemaUpToDate(t *testing.T) {
	t.Chdir("../..")
	want, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run `go run ./hack/genschema`", Path)
	}
}

// The golden report produced by an end-to-end collector run validates.
func TestGoldenReportValidates(t *testing.T) {
	t.Chdir("../..")
	if err := compile(t).Validate(load(t, goldenReport)); err != nil {
		t.Fatalf("%s does not match %s:\n%v", goldenReport, Path, err)
	}
}

func TestSchemaRejectsContractBreaks(t *testing.T) {
	t.Chdir("../..")
	s := compile(t)
	mutate := func(f func(m map[string]any)) any {
		var m map[string]any
		data, err := os.ReadFile(goldenReport)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		data, _ = json.Marshal(m)
		v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	meta := func(m map[string]any) map[string]any { return m["metadata"].(map[string]any) }

	cases := map[string]func(m map[string]any){
		"missing schemaVersion":  func(m map[string]any) { delete(meta(m), "schemaVersion") },
		"other major version":    func(m map[string]any) { meta(m)["schemaVersion"] = "2.0.0" },
		"missing images":         func(m map[string]any) { delete(m, "images") },
		"generatedAt not a time": func(m map[string]any) { meta(m)["generatedAt"] = "yesterday" },
		"summary count as string": func(m map[string]any) {
			m["summary"].(map[string]any)["totalImages"] = "6"
		},
		"warnings not strings": func(m map[string]any) { m["warnings"] = []any{1} },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if err := s.Validate(mutate(f)); err == nil {
				t.Error("schema accepted a broken report")
			}
		})
	}

	// Additive changes within a major version are allowed.
	newer := mutate(func(m map[string]any) {
		meta(m)["schemaVersion"] = "1.9.0"
		m["somethingNew"] = true
	})
	if err := s.Validate(newer); err != nil {
		t.Errorf("schema must accept a newer minor version with extra fields: %v", err)
	}
}

func TestSchemaVersionInSchema(t *testing.T) {
	t.Chdir("../..")
	data, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), report.SchemaVersion) {
		t.Errorf("%s does not mention schemaVersion %s", Path, report.SchemaVersion)
	}
}
