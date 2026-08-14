package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	sharedjwt "github.com/EduGoGroup/wapp-shared/auth/jwt"
	"github.com/golang-jwt/jwt/v5"
)

func testConfig(adminURL, publicURL, identityURL string) *config.Config {
	return &config.Config{
		Environment:      "local",
		HTTPAddr:         ":0",
		AdminAPIBaseURL:  adminURL,
		PublicAPIBaseURL: publicURL,
		IdentityBaseURL:  identityURL,
		CookieSecure:     false,
		CookieSameSite:   "lax",
		RateLimitEnabled: false,
		UpstreamTimeout:  5 * time.Second,
	}
}

func makeAdminToken(t *testing.T, exp time.Time) string {
	t.Helper()
	claims := sharedjwt.Claims{
		UserID:           "u-admin-1",
		TenantID:         "00000000-0000-0000-0000-000000000000",
		Roles:            []string{"platform_admin"},
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("dummy"))
	if err != nil {
		t.Fatalf("firmar token: %v", err)
	}
	return signed
}

func adminSessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	token := makeAdminToken(t, time.Now().Add(time.Hour))
	val, err := encodeSession(sessionData{AccessToken: token, RefreshToken: "rt-1"})
	if err != nil {
		t.Fatalf("encodeSession: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: val}
}

func mintCSRF(router http.Handler) *http.Cookie {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(rec, req)
	for _, sc := range rec.Result().Cookies() {
		if sc.Name == csrfCookieName {
			return sc
		}
	}
	return &http.Cookie{Name: csrfCookieName, Value: ""}
}

func postFormWithCSRF(router http.Handler, path string, form url.Values, sessionCookie *http.Cookie) *httptest.ResponseRecorder {
	csrf := mintCSRF(router)
	form.Set(csrfFieldName, csrf.Value)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	if sessionCookie != nil {
		req.AddCookie(sessionCookie)
	}
	router.ServeHTTP(rec, req)
	return rec
}

func TestHealthz_Success(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", rec.Code)
	}
}

func TestSecurityHeaders_Present(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", rec.Header().Get("X-Frame-Options"))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", rec.Header().Get("Referrer-Policy"))
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "nonce-") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q, esperado nonce y frame-ancestors 'none'", csp)
	}
}

func TestCSRF_RejectsMutatingWithoutToken(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))
	form := url.Values{"email": {"a@b.com"}, "password": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /login sin CSRF status = %d, want 403", rec.Code)
	}
}

func TestStaticCSS_ServedSameOrigin(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	for _, path := range []string{
		"/static/css/app.css",
		"/static/css/theme-platform.css",
		"/static/css/wapp-tokens.css",
		"/static/css/wapp-components.css",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
				t.Fatalf("GET %s Content-Type = %q, want text/css", path, ct)
			}
		})
	}
}

func TestSetTrustedProxies_PanicsOnInvalidFormat(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("esperado panic con TrustedProxies inválido")
		}
	}()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	cfg.TrustedProxies = "not-an-ip-address"
	_ = NewRouter(cfg)
}
