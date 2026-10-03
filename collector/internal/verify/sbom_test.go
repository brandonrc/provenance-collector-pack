package verify

import (
	"context"
	"testing"
)

func TestDetectSBOMFormat_InTotoSPDX(t *testing.T) {
	payload := `{
		"_type": "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://spdx.dev/Document",
		"predicate": {"spdxVersion": "SPDX-2.3"}
	}`
	got := detectSBOMFormat([]byte(payload))
	if got != "spdx" {
		t.Errorf("expected spdx, got %q", got)
	}
}

func TestDetectSBOMFormat_InTotoCycloneDX(t *testing.T) {
	payload := `{
		"_type": "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://cyclonedx.org/bom/v1.4",
		"predicate": {"bomFormat": "CycloneDX"}
	}`
	got := detectSBOMFormat([]byte(payload))
	if got != "cyclonedx" {
		t.Errorf("expected cyclonedx, got %q", got)
	}
}

func TestDetectSBOMFormat_InTotoUnknownPredicateWithSPDXContent(t *testing.T) {
	payload := `{
		"_type": "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://example.com/custom",
		"predicate": {"spdxVersion": "SPDX-2.3", "SPDXID": "SPDXRef-DOCUMENT"}
	}`
	got := detectSBOMFormat([]byte(payload))
	if got != "spdx" {
		t.Errorf("expected spdx from predicate content, got %q", got)
	}
}

func TestDetectSBOMFormat_RawSPDXContent(t *testing.T) {
	payload := `{"spdxVersion": "SPDX-2.3", "SPDXID": "SPDXRef-DOCUMENT"}`
	got := detectSBOMFormat([]byte(payload))
	if got != "spdx" {
		t.Errorf("expected spdx from raw content, got %q", got)
	}
}

func TestDetectSBOMFormat_RawCycloneDXContent(t *testing.T) {
	payload := `{"bomFormat": "CycloneDX", "specVersion": "1.4"}`
	got := detectSBOMFormat([]byte(payload))
	if got != "cyclonedx" {
		t.Errorf("expected cyclonedx from raw content, got %q", got)
	}
}

func TestDetectSBOMFormat_Empty(t *testing.T) {
	got := detectSBOMFormat([]byte("{}"))
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestDetectSBOMFormat_InvalidJSON(t *testing.T) {
	// Falls back to content-based detection
	got := detectSBOMFormat([]byte("not json but has SPDXRef- in it"))
	if got != "spdx" {
		t.Errorf("expected spdx from fallback, got %q", got)
	}
}

func TestDetectSBOMFormat_NoMatch(t *testing.T) {
	got := detectSBOMFormat([]byte("just some random text"))
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestNewSBOMDiscoverer(t *testing.T) {
	d := NewSBOMDiscoverer()
	if d == nil {
		t.Fatal("expected non-nil discoverer")
	}
	if _, ok := d.(*OCISBOMDiscoverer); !ok {
		t.Error("expected *OCISBOMDiscoverer")
	}
}

func TestSBOMFormatFromPredicate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://spdx.dev/Document", "spdx"},
		{"https://spdx.dev/Document/v2.3", "spdx"},
		{"https://cyclonedx.org/bom", "cyclonedx"},
		{"https://cyclonedx.org/bom/v1.5", "cyclonedx"},
		{"https://slsa.dev/provenance/v1", ""},
		{"https://in-toto.io/provenance/v1", ""},
		{"", ""},
		{"random", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := sbomFormatFromPredicate(tc.input); got != tc.want {
				t.Errorf("sbomFormatFromPredicate(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestOCISBOMDiscoverer_InvalidRef(t *testing.T) {
	d := NewSBOMDiscoverer()
	info, err := d.Discover(context.Background(), ":::invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.HasSBOM {
		t.Error("expected no SBOM for invalid ref")
	}
}

func TestOCISBOMDiscoverer_UnreachableImage(t *testing.T) {
	d := NewSBOMDiscoverer()
	// Registry calls fail; should return HasSBOM=false without an error.
	info, err := d.Discover(context.Background(), "localhost:1/nonexistent:v0.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.HasSBOM {
		t.Error("expected no SBOM for unreachable image")
	}
}

func TestDetectFromContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"spdx url", `"documentNamespace": "https://spdx.dev/Document/test"`, "spdx"},
		{"spdx ref", `"SPDXID": "SPDXRef-Package"`, "spdx"},
		{"cyclonedx upper", `"bomFormat": "CycloneDX"`, "cyclonedx"},
		{"cyclonedx lower", `"specVersion": "cyclonedx/1.4"`, "cyclonedx"},
		{"no match", `{"name": "test"}`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectFromContent(tc.input)
			if got != tc.expected {
				t.Errorf("detectFromContent(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
