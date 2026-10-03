package registry

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/nebari-dev/provenance-collector/internal/report"
)

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		ref      string
		wantRepo string
		wantTag  string
	}{
		{"nginx:1.27", "nginx", "1.27"},
		{"nginx", "nginx", "latest"},
		{"docker.io/library/nginx:1.27-alpine", "docker.io/library/nginx", "1.27-alpine"},
		{"ghcr.io/org/repo:v2.0.0", "ghcr.io/org/repo", "v2.0.0"},
		{"myregistry.com:5000/myimage:tag", "myregistry.com:5000/myimage", "tag"},
		{"nginx@sha256:abc123", "nginx", ""},
		{"ghcr.io/org/repo@sha256:abc", "ghcr.io/org/repo", ""},
	}

	for _, tc := range tests {
		repo, tag := parseImageRef(tc.ref)
		if repo != tc.wantRepo || tag != tc.wantTag {
			t.Errorf("parseImageRef(%q) = (%q, %q), want (%q, %q)",
				tc.ref, repo, tag, tc.wantRepo, tc.wantTag)
		}
	}
}

func TestShouldFlag(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		latest      string
		newest      string
		updateLevel string
		want        bool
	}{
		// patch level — flag everything
		{"patch: patch bump", "1.2.3", "1.2.5", "", "patch", true},
		{"patch: minor bump", "1.2.3", "1.3.0", "", "patch", true},
		{"patch: major bump", "1.2.3", "", "2.0.0", "patch", true},
		{"patch: no update", "1.2.3", "", "", "patch", false},

		// minor level — skip patch-only
		{"minor: patch bump only", "1.2.3", "1.2.5", "", "minor", false},
		{"minor: minor bump", "1.2.3", "1.3.0", "", "minor", true},
		{"minor: major bump", "1.2.3", "", "2.0.0", "minor", true},
		{"minor: patch in major, minor in newest", "1.2.3", "1.2.5", "1.3.0", "minor", true},

		// major level — only major
		{"major: patch bump only", "1.2.3", "1.2.5", "", "major", false},
		{"major: minor bump only", "1.2.3", "1.3.0", "", "major", false},
		{"major: major bump", "1.2.3", "", "2.0.0", "major", true},
		{"major: minor in major, major in newest", "1.2.3", "1.5.0", "2.0.0", "major", true},
		{"major: no update", "2.0.0", "", "", "major", false},

		// empty strings
		{"empty candidates", "1.0.0", "", "", "patch", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker := &RegistryUpdateChecker{updateLevel: tc.updateLevel}
			info := &report.UpdateInfo{
				CurrentTag:      tc.current,
				LatestInMajor:   tc.latest,
				NewestAvailable: tc.newest,
			}
			got := checker.shouldFlag(mustParse(tc.current), info)
			if got != tc.want {
				t.Errorf("shouldFlag(%s, level=%s) = %v, want %v (latest=%s newest=%s)",
					tc.current, tc.updateLevel, got, tc.want, tc.latest, tc.newest)
			}
		})
	}
}

func TestNewUpdateChecker_DefaultLevel(t *testing.T) {
	c := NewUpdateChecker(false, "")
	rc := c.(*RegistryUpdateChecker)
	if rc.updateLevel != UpdateLevelPatch {
		t.Errorf("expected default level %q, got %q", UpdateLevelPatch, rc.updateLevel)
	}
}

func TestNewUpdateChecker_SkipPrerelease(t *testing.T) {
	c := NewUpdateChecker(true, "minor")
	rc := c.(*RegistryUpdateChecker)
	if !rc.skipPrerelease {
		t.Error("expected skipPrerelease=true")
	}
	if rc.updateLevel != "minor" {
		t.Errorf("expected level minor, got %s", rc.updateLevel)
	}
}

func mustParse(s string) *semver.Version {
	v, err := semver.NewVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}
