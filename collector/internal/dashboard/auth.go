package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthConfig wires the optional OIDC-backed authorization layer used to decide
// whether the calling user may trigger a manual scan. When IssuerURL is empty
// or AdminGroups is empty, the dashboard treats authorization as disabled:
// /api/me reports canRunScan=false and /api/scan returns 503.
type AuthConfig struct {
	// IssuerURL is the OIDC issuer base (e.g. "https://kc/realms/nebari").
	// The userinfo endpoint is derived as <IssuerURL>/protocol/openid-connect/userinfo.
	IssuerURL string
	// AdminGroups is the list of groups whose members may trigger a scan.
	// Matched exactly against the values in the userinfo `groups` claim.
	AdminGroups []string
	// HTTPClient is the client used to call /userinfo. Defaults to a client
	// with a 5s timeout.
	HTTPClient *http.Client
	// CacheTTL controls how long a userinfo response is cached per token.
	// Defaults to 60s.
	CacheTTL time.Duration
}

// Identity is the subset of userinfo claims the dashboard cares about.
type Identity struct {
	Subject string   `json:"sub,omitempty"`
	Email   string   `json:"email,omitempty"`
	Groups  []string `json:"groups,omitempty"`
}

type cachedIdentity struct {
	id      *Identity
	expires time.Time
}

type authenticator struct {
	issuerURL   string
	adminGroups map[string]struct{}
	client      *http.Client
	ttl         time.Duration

	mu    sync.Mutex
	cache map[string]cachedIdentity
}

func newAuthenticator(cfg AuthConfig) *authenticator {
	a := &authenticator{
		issuerURL:   strings.TrimRight(cfg.IssuerURL, "/"),
		adminGroups: make(map[string]struct{}, len(cfg.AdminGroups)),
		client:      cfg.HTTPClient,
		ttl:         cfg.CacheTTL,
		cache:       make(map[string]cachedIdentity),
	}
	for _, g := range cfg.AdminGroups {
		if g = strings.TrimSpace(g); g != "" {
			a.adminGroups[g] = struct{}{}
		}
	}
	if a.client == nil {
		a.client = &http.Client{Timeout: 5 * time.Second}
	}
	if a.ttl == 0 {
		a.ttl = 60 * time.Second
	}
	return a
}

// enabled reports whether the authenticator has enough configuration to make
// an admin-group decision. Nil-safe.
func (a *authenticator) enabled() bool {
	return a != nil && a.issuerURL != "" && len(a.adminGroups) > 0
}

// identify resolves the caller's identity from the request's bearer token,
// using the configured OIDC userinfo endpoint. Returns nil when auth is
// disabled, when no token is present, or when validation fails. Errors fail
// closed — we never grant scan access based on an unverifiable token.
func (a *authenticator) identify(ctx context.Context, r *http.Request) *Identity {
	if a == nil || a.issuerURL == "" {
		return nil
	}
	token := bearerFrom(r)
	if token == "" {
		return nil
	}
	h := tokenHash(token)
	if id := a.cacheGet(h); id != nil {
		return id
	}
	id, err := a.fetchUserInfo(ctx, token)
	if err != nil {
		return nil
	}
	a.cachePut(h, id)
	return id
}

// canRunScan returns true iff the identity is a member of at least one
// configured admin group. Nil-safe.
func (a *authenticator) canRunScan(id *Identity) bool {
	if !a.enabled() || id == nil {
		return false
	}
	for _, g := range id.Groups {
		if _, ok := a.adminGroups[g]; ok {
			return true
		}
	}
	return false
}

func (a *authenticator) cacheGet(h string) *Identity {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.cache[h]
	if !ok || time.Now().After(c.expires) {
		delete(a.cache, h)
		return nil
	}
	return c.id
}

func (a *authenticator) cachePut(h string, id *Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[h] = cachedIdentity{id: id, expires: time.Now().Add(a.ttl)}
}

// fetchUserInfo treats the bearer as opaque and delegates validation to the
// issuer's userinfo endpoint: Keycloak verifies the signature against its own
// keys and returns claims, or rejects the request. We never inspect or check
// the token's `iss` claim ourselves, so IssuerURL is just "where the dashboard
// pod sends userinfo requests" — it does NOT need to equal the `iss` value in
// the token. This matters in Nebari's split-horizon DNS setup, where tokens
// minted via the external Keycloak hostname can still be validated by calling
// the internal cluster-DNS Keycloak service.
func (a *authenticator) fetchUserInfo(ctx context.Context, token string) (*Identity, error) {
	url := a.issuerURL + "/protocol/openid-connect/userinfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("userinfo: %s: %s", resp.Status, body)
	}
	var raw struct {
		Sub    string          `json:"sub"`
		Email  string          `json:"email"`
		Groups json.RawMessage `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	groups, _ := parseGroupsClaim(raw.Groups)
	return &Identity{Subject: raw.Sub, Email: raw.Email, Groups: groups}, nil
}

// parseGroupsClaim normalizes the `groups` claim, which Keycloak emits in a
// few different shapes depending on the protocol-mapper configuration:
// a string array, a single string, or absent. We accept all three.
func parseGroupsClaim(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	return nil, fmt.Errorf("unrecognized groups claim shape")
}

func bearerFrom(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// tokenHash hashes the bearer for use as a cache key. The cache never stores
// the raw token.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
