package registry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Options are the registry connection settings shared by every registry call
// the collector makes (digest resolution, tag listing, signature, SBOM and
// provenance lookups).
type Options struct {
	// AuthFile is a Docker config.json (PROVENANCE_REGISTRY_AUTH), or a
	// directory holding one as config.json or .dockerconfigjson (the key of a
	// kubernetes.io/dockerconfigjson Secret mounted as a volume). Credentials
	// found there take precedence over the default keychain ($DOCKER_CONFIG,
	// ~/.docker/config.json and credential helpers), which is still consulted
	// for registries the file does not mention.
	AuthFile string
	// CAFile is a PEM bundle of extra CA certificates trusted for registry TLS
	// (PROVENANCE_REGISTRY_CA_FILE), added to the system pool.
	CAFile string
	// Insecure lists registry hosts (host or host:port) that may be reached
	// over plain HTTP or with an unverified TLS certificate
	// (PROVENANCE_REGISTRY_INSECURE).
	Insecure []string
}

// Client carries the resolved keychain and transport for Options. A nil
// *Client behaves like NewClient(Options{}): default keychain, system roots.
type Client struct {
	keychain  authn.Keychain
	transport http.RoundTripper
	insecure  map[string]bool
}

// NewClient resolves Options into a Client. It fails if the auth or CA file
// is set but cannot be read, so a misconfigured deployment stops at startup
// instead of silently falling back to anonymous pulls.
func NewClient(o Options) (*Client, error) {
	c := &Client{
		keychain: authn.DefaultKeychain,
		insecure: make(map[string]bool, len(o.Insecure)),
	}
	for _, h := range o.Insecure {
		if h = strings.TrimSpace(h); h != "" {
			c.insecure[h] = true
		}
	}

	if o.AuthFile != "" {
		kc, err := keychainFromFile(o.AuthFile)
		if err != nil {
			return nil, err
		}
		c.keychain = authn.NewMultiKeychain(kc, authn.DefaultKeychain)
	}

	base := remote.DefaultTransport.(*http.Transport).Clone()
	if o.CAFile != "" {
		pem, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading registry CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("registry CA file %s contains no PEM certificates", o.CAFile)
		}
		base.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	c.transport = base

	if len(c.insecure) > 0 {
		skip := base.Clone()
		if skip.TLSClientConfig == nil {
			skip.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		skip.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // opt-in per registry host via PROVENANCE_REGISTRY_INSECURE
		c.transport = &hostTransport{secure: base, insecure: skip, hosts: c.insecure}
	}
	return c, nil
}

func (c *Client) orDefault() *Client {
	if c != nil {
		return c
	}
	d, _ := NewClient(Options{}) // cannot fail without files
	return d
}

// Keychain returns the keychain used to authenticate registry requests.
func (c *Client) Keychain() authn.Keychain { return c.orDefault().keychain }

// Transport returns the HTTP transport used for registry requests.
func (c *Client) Transport() http.RoundTripper { return c.orDefault().transport }

// IsInsecure reports whether registry host is listed as insecure.
func (c *Client) IsInsecure(host string) bool { return c.orDefault().insecure[host] }

// ParseReference parses an image reference, marking it insecure (HTTP
// fallback allowed) when its registry is listed in Options.Insecure.
func (c *Client) ParseReference(s string) (name.Reference, error) {
	ref, err := name.ParseReference(s)
	if err != nil {
		return nil, err
	}
	if c.IsInsecure(ref.Context().RegistryStr()) {
		return name.ParseReference(s, name.Insecure)
	}
	return ref, nil
}

// RemoteOptions returns go-containerregistry remote options carrying the
// client's auth and transport.
func (c *Client) RemoteOptions(ctx context.Context) []remote.Option {
	return []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(c.Keychain()),
		remote.WithTransport(c.Transport()),
	}
}

// CraneOptions returns crane options for a call against imageRef.
func (c *Client) CraneOptions(ctx context.Context, imageRef string) []crane.Option {
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(c.Keychain()),
		crane.WithTransport(c.Transport()),
	}
	// imageRef may be a bare repository (tag listing); ParseReference still
	// yields its registry.
	if ref, err := name.ParseReference(imageRef); err == nil && c.IsInsecure(ref.Context().RegistryStr()) {
		opts = append(opts, crane.Insecure)
	}
	return opts
}

// hostTransport skips TLS verification only for the listed registry hosts.
type hostTransport struct {
	secure, insecure http.RoundTripper
	hosts            map[string]bool
}

func (t *hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.hosts[req.URL.Host] || t.hosts[req.URL.Hostname()] {
		return t.insecure.RoundTrip(req)
	}
	return t.secure.RoundTrip(req)
}

// fileKeychain resolves credentials from one Docker config.json.
type fileKeychain struct {
	cf *configfile.ConfigFile
}

func keychainFromFile(path string) (authn.Keychain, error) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		found := false
		for _, name := range []string{"config.json", ".dockerconfigjson"} {
			candidate := filepath.Join(path, name)
			if _, err := os.Stat(candidate); err == nil {
				path, found = candidate, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("registry auth directory %s has no config.json or .dockerconfigjson", path)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading registry auth file: %w", err)
	}
	defer func() { _ = f.Close() }()
	cf, err := dockerconfig.LoadFromReader(f)
	if err != nil {
		return nil, fmt.Errorf("parsing registry auth file %s: %w", path, err)
	}
	return &fileKeychain{cf: cf}, nil
}

func (k *fileKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	key := target.RegistryStr()
	if key == name.DefaultRegistry {
		key = authn.DefaultAuthKey
	}
	cfg, err := k.cf.GetAuthConfig(key)
	if err != nil {
		return nil, err
	}
	if cfg.Username == "" && cfg.Password == "" && cfg.Auth == "" &&
		cfg.IdentityToken == "" && cfg.RegistryToken == "" {
		return authn.Anonymous, nil
	}
	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}
