package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	testUser = "collector"
	testPass = "s3cret"
)

// withBasicAuth puts an in-memory registry behind HTTP basic auth, the way a
// private registry answers anonymous pulls with 401.
func withBasicAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != testUser || p != testPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// pushImage writes a random image to host/repo:tag (pushing with credentials
// when the registry requires them) and returns its digest.
func pushImage(t *testing.T, host, repoTag string, auth bool) string {
	t.Helper()
	img, err := random.Image(128, 1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.ParseReference(host + "/" + repoTag)
	if err != nil {
		t.Fatal(err)
	}
	opts := []remote.Option{}
	if auth {
		opts = append(opts, remote.WithAuth(&authn.Basic{Username: testUser, Password: testPass}))
	}
	if err := remote.Write(ref, img, opts...); err != nil {
		t.Fatalf("push %s: %v", ref, err)
	}
	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d.String()
}

func writeDockerConfig(t *testing.T, host string) string {
	t.Helper()
	cfg := map[string]any{"auths": map[string]any{
		host: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(testUser + ":" + testPass))},
	}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolateDockerConfig(t *testing.T) {
	t.Helper()
	// Keep the default keychain from picking up the developer's own creds.
	t.Setenv("DOCKER_CONFIG", t.TempDir())
}

func TestDigestResolverUsesRegistryAuth(t *testing.T) {
	isolateDockerConfig(t)
	srv := httptest.NewServer(withBasicAuth(ggcrregistry.New()))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	want := pushImage(t, host, "private/app:1.0.0", true)
	ref := host + "/private/app:1.0.0"

	anon := NewDigestResolver(5*time.Second, nil)
	if _, err := anon.Resolve(context.Background(), ref); err == nil {
		t.Fatal("anonymous resolve against a private registry should fail")
	}

	client, err := NewClient(Options{AuthFile: writeDockerConfig(t, host)})
	if err != nil {
		t.Fatal(err)
	}
	r := NewDigestResolver(5*time.Second, client)
	got, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve with PROVENANCE_REGISTRY_AUTH: %v", err)
	}
	if got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}

	// Second lookup is served from the cache even if the registry is gone.
	srv.Close()
	if again, err := r.Resolve(context.Background(), ref); err != nil || again != want {
		t.Errorf("cached Resolve = %q, %v", again, err)
	}
}

func TestDigestResolverTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	host := strings.TrimPrefix(srv.URL, "http://")

	start := time.Now()
	_, err := NewDigestResolver(100*time.Millisecond, nil).Resolve(context.Background(), host+"/slow/app:1")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("timeout not applied: took %s", time.Since(start))
	}
}

func writeCAFile(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// pushTLS pushes through the test server's own trusted client.
func pushTLS(t *testing.T, srv *httptest.Server, repoTag string) string {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "https://")
	img, err := random.Image(128, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := crane.Push(img, host+"/"+repoTag, crane.WithTransport(srv.Client().Transport)); err != nil {
		t.Fatalf("push: %v", err)
	}
	d, _ := img.Digest()
	return d.String()
}

func TestCustomCAFile(t *testing.T) {
	srv := httptest.NewTLSServer(ggcrregistry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	want := pushTLS(t, srv, "corp/app:2.0.0")
	ref := host + "/corp/app:2.0.0"

	if _, err := NewDigestResolver(5*time.Second, nil).Resolve(context.Background(), ref); err == nil {
		t.Fatal("a registry with a private CA should fail without PROVENANCE_REGISTRY_CA_FILE")
	}

	client, err := NewClient(Options{CAFile: writeCAFile(t, srv)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewDigestResolver(5*time.Second, client).Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve with CA file: %v", err)
	}
	if got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}
}

func TestInsecureRegistry(t *testing.T) {
	srv := httptest.NewTLSServer(ggcrregistry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	want := pushTLS(t, srv, "lab/app:0.1.0")

	other, err := NewClient(Options{Insecure: []string{"some-other-registry:5000"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDigestResolver(5*time.Second, other).Resolve(context.Background(), host+"/lab/app:0.1.0"); err == nil {
		t.Fatal("TLS verification must stay on for hosts not listed as insecure")
	}

	client, err := NewClient(Options{Insecure: []string{" ", host}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewDigestResolver(5*time.Second, client).Resolve(context.Background(), host+"/lab/app:0.1.0")
	if err != nil {
		t.Fatalf("Resolve against an insecure-listed host: %v", err)
	}
	if got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}

	if !client.IsInsecure(host) || client.IsInsecure("ghcr.io") {
		t.Error("IsInsecure does not match the configured hosts")
	}
	ref, err := client.ParseReference(host + "/lab/app:0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Context().Scheme() != "http" {
		t.Errorf("insecure reference should allow the http fallback, scheme = %s", ref.Context().Scheme())
	}
	if _, err := client.ParseReference(":::bad"); err == nil {
		t.Error("ParseReference accepted an invalid reference")
	}
}

func TestNewClientErrors(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	badJSON := filepath.Join(dir, "config.json")
	if err := os.WriteFile(badJSON, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]Options{
		"missing auth file": {AuthFile: filepath.Join(dir, "nope.json")},
		"bad auth file":     {AuthFile: badJSON},
		"missing CA file":   {CAFile: filepath.Join(dir, "nope.pem")},
		"CA file not PEM":   {CAFile: notPEM},
	}
	for name, o := range cases {
		if _, err := NewClient(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestFileKeychain(t *testing.T) {
	isolateDockerConfig(t)
	path := writeDockerConfig(t, "registry.example.com")
	kc, err := keychainFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	resolve := func(ref string) *authn.AuthConfig {
		t.Helper()
		r, err := name.ParseReference(ref)
		if err != nil {
			t.Fatal(err)
		}
		a, err := kc.Resolve(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := a.Authorization()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	if got := resolve("registry.example.com/team/app:1"); got.Username != testUser || got.Password != testPass {
		t.Errorf("credentials not resolved from the auth file: %+v", got)
	}
	if got := resolve("ghcr.io/other/app:1"); got.Username != "" || got.Auth != "" {
		t.Errorf("unlisted registry should be anonymous, got %+v", got)
	}

	// Docker Hub credentials live under the legacy index URL key.
	hub := filepath.Join(t.TempDir(), "config.json")
	data := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("hubuser:hubpass")) + `"}}}`
	if err := os.WriteFile(hub, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Options{AuthFile: hub})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := name.ParseReference("nginx:1.27")
	a, err := client.Keychain().Resolve(r.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Authorization()
	if cfg.Username != "hubuser" {
		t.Errorf("docker hub credentials not found, got %+v", cfg)
	}
}

func TestNilClientDefaults(t *testing.T) {
	var c *Client
	if c.Keychain() == nil || c.Transport() == nil {
		t.Fatal("nil client must fall back to defaults")
	}
	if c.IsInsecure("localhost:5000") {
		t.Error("nil client must not mark hosts insecure")
	}
	if n := len(c.RemoteOptions(context.Background())); n != 3 {
		t.Errorf("RemoteOptions len = %d", n)
	}
	if n := len(c.CraneOptions(context.Background(), "ghcr.io/a/b:1")); n != 3 {
		t.Errorf("CraneOptions len = %d", n)
	}
}

// A kubernetes.io/dockerconfigjson Secret mounted as a directory exposes the
// file as .dockerconfigjson; pointing PROVENANCE_REGISTRY_AUTH at the
// directory works for both that and a plain config.json.
func TestAuthFileDirectory(t *testing.T) {
	isolateDockerConfig(t)
	for _, fileName := range []string{".dockerconfigjson", "config.json"} {
		dir := t.TempDir()
		src := writeDockerConfig(t, "registry.example.com")
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fileName), data, 0o600); err != nil {
			t.Fatal(err)
		}
		client, err := NewClient(Options{AuthFile: dir})
		if err != nil {
			t.Fatalf("%s: %v", fileName, err)
		}
		r, _ := name.ParseReference("registry.example.com/a/b:1")
		a, err := client.Keychain().Resolve(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		cfg, _ := a.Authorization()
		if cfg.Username != testUser {
			t.Errorf("%s: credentials not found, got %+v", fileName, cfg)
		}
	}
	if _, err := NewClient(Options{AuthFile: t.TempDir()}); err == nil {
		t.Error("an empty auth directory must fail at startup")
	}
}
