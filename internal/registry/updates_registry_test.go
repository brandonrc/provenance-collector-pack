package registry

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

// tagRegistry serves one repository with the given tags.
func tagRegistry(t *testing.T, repo string, tags ...string) string {
	t.Helper()
	srv := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if err := crane.Push(img, host+"/"+repo+":"+tag); err != nil {
			t.Fatalf("push %s: %v", tag, err)
		}
	}
	return host + "/" + repo
}

func TestUpdateCheckerAgainstRegistry(t *testing.T) {
	repo := tagRegistry(t, "team/app",
		"1.2.0", "1.2.3", "1.2.4", "1.3.0", "1.4.0-rc.1", "2.0.0", "2.1.0-beta.1", "latest", "sha-deadbeef")

	cases := []struct {
		name           string
		ref            string
		skipPrerelease bool
		level          string
		want           string // LatestInMajor|NewestAvailable|UpdateAvailable
	}{
		{"patch level sees everything", repo + ":1.2.3", true, "patch", "1.3.0|2.0.0|true"},
		{"prereleases included when asked", repo + ":1.2.3", false, "patch", "1.4.0-rc.1|2.1.0-beta.1|true"},
		{"minor level flags the major bump", repo + ":1.3.0", true, "minor", "|2.0.0|true"},
		{"major level ignores minor bumps", repo + ":2.0.0", true, "major", "||false"},
		{"already newest", repo + ":2.0.0", true, "patch", "||false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := NewUpdateChecker(tc.skipPrerelease, tc.level, nil).Check(context.Background(), tc.ref)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			got := info.LatestInMajor + "|" + info.NewestAvailable + "|" + map[bool]string{true: "true", false: "false"}[info.UpdateAvailable]
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestUpdateCheckerNonSemverTags(t *testing.T) {
	c := NewUpdateChecker(true, "patch", nil)
	for _, ref := range []string{
		"localhost:1/app:latest",      // latest: nothing to compare
		"localhost:1/app",             // implicit latest
		"localhost:1/app:sha-1234abc", // not semver
		"localhost:1/app@sha256:0000000000000000000000000000000000000000000000000000000000000000",
	} {
		info, err := c.Check(context.Background(), ref)
		if err != nil {
			t.Errorf("%s: registry must not be contacted, got %v", ref, err)
			continue
		}
		if info.UpdateAvailable {
			t.Errorf("%s: unexpected update %+v", ref, info)
		}
	}
}

func TestUpdateCheckerRegistryError(t *testing.T) {
	if _, err := NewUpdateChecker(true, "patch", nil).Check(context.Background(), "localhost:1/app:1.0.0"); err == nil {
		t.Error("an unreachable registry must surface an error so the report can warn")
	}
}
