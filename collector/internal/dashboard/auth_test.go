package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// userInfoStub stands in for Keycloak's /protocol/openid-connect/userinfo.
// Tokens map to canned responses; an unknown token returns 401.
func userInfoStub(t *testing.T, responses map[string]string, hits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/protocol/openid-connect/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(h, "Bearer ")
		body, ok := responses[token]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func TestMe_AuthDisabled(t *testing.T) {
	srv := NewServer(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp meResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.AuthEnabled {
		t.Errorf("expected authEnabled=false when no issuer configured")
	}
	if resp.CanRunScan {
		t.Errorf("expected canRunScan=false")
	}
	// Default features are all-off — matches the chart's opt-in posture.
	if resp.Features.TimelineDeltas {
		t.Errorf("expected features.timelineDeltas=false by default")
	}
}

func TestMe_FeaturesReflectWithFeatures(t *testing.T) {
	srv := NewServer(t.TempDir()).WithFeatures(Features{TimelineDeltas: true})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp meResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !resp.Features.TimelineDeltas {
		t.Errorf("expected features.timelineDeltas=true after WithFeatures")
	}
}

func TestMe_NoBearer(t *testing.T) {
	stub := userInfoStub(t, nil, nil)
	defer stub.Close()

	srv := NewServer(t.TempDir()).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp meResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.AuthEnabled {
		t.Errorf("expected authEnabled=true")
	}
	if resp.CanRunScan {
		t.Errorf("expected canRunScan=false without bearer")
	}
}

func TestMe_AdminGroup(t *testing.T) {
	stub := userInfoStub(t, map[string]string{
		"admin-token": `{"sub":"u1","email":"a@x","groups":["/admins","/users"]}`,
	}, nil)
	defer stub.Close()

	srv := NewServer(t.TempDir()).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp meResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.CanRunScan {
		t.Errorf("expected canRunScan=true for /admins member, got %+v", resp)
	}
	if resp.Email != "a@x" {
		t.Errorf("expected email a@x, got %q", resp.Email)
	}
}

func TestMe_NonAdminGroup(t *testing.T) {
	stub := userInfoStub(t, map[string]string{
		"user-token": `{"sub":"u2","email":"u@x","groups":["/users"]}`,
	}, nil)
	defer stub.Close()

	srv := NewServer(t.TempDir()).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp meResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CanRunScan {
		t.Errorf("non-admin user should not see canRunScan=true: %+v", resp)
	}
	// Identity should still surface for the UI to show who's logged in.
	if resp.Email != "u@x" {
		t.Errorf("expected email u@x, got %q", resp.Email)
	}
}

func TestMe_InvalidToken(t *testing.T) {
	stub := userInfoStub(t, map[string]string{}, nil) // every token 401s
	defer stub.Close()

	srv := NewServer(t.TempDir()).WithAuth(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer bogus")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp meResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CanRunScan {
		t.Errorf("expected canRunScan=false when userinfo returns 401")
	}
	if resp.Email != "" {
		t.Errorf("expected no email on failed validation, got %q", resp.Email)
	}
}

func TestUserInfo_Cached(t *testing.T) {
	var hits int32
	stub := userInfoStub(t, map[string]string{
		"admin-token": `{"sub":"u1","email":"a@x","groups":["/admins"]}`,
	}, &hits)
	defer stub.Close()

	a := newAuthenticator(AuthConfig{
		IssuerURL:   stub.URL,
		AdminGroups: []string{"/admins"},
		CacheTTL:    50 * time.Millisecond,
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer admin-token")

	for range 3 {
		_ = a.identify(r.Context(), r)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 userinfo call within TTL, got %d", got)
	}

	time.Sleep(80 * time.Millisecond)
	_ = a.identify(r.Context(), r)
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 userinfo calls after TTL expiry, got %d", got)
	}
}

func TestParseGroupsClaim(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"array", `["a","b"]`, []string{"a", "b"}},
		{"single string", `"only"`, []string{"only"}},
		{"null", `null`, nil},
		{"empty", ``, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := parseGroupsClaim([]byte(tc.raw))
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("idx %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBearerFrom(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"Bearer abc":     "abc",
		"bearer xyz":     "xyz",
		"Basic foo":      "",
		"Bearer  spaces": "spaces",
	}
	for h, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		if got := bearerFrom(r); got != want {
			t.Errorf("header %q: got %q want %q", h, got, want)
		}
	}
}
