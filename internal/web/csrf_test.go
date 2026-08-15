package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestCSRF_CookieIsHardened cierra C-08: la cookie CSRF debe ser HttpOnly y SameSite=Lax SIEMPRE, sin
// importar cómo se configure `CookieSameSite` (esa config gobierna la cookie de SESIÓN, no esta). El
// caso decisivo es el segundo: con `CookieSameSite=none` configurado explícitamente, la cookie CSRF NO
// debe degradarse a SameSite=None.
func TestCSRF_CookieIsHardened(t *testing.T) {
	t.Parallel()

	for _, sameSiteCfg := range []string{"lax", "strict", "none"} {
		t.Run("CookieSameSite="+sameSiteCfg, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")
			cfg.CookieSameSite = sameSiteCfg
			router := NewRouter(cfg)

			req := httptest.NewRequest(http.MethodGet, "/login", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			var csrfCookie *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == csrfCookieName {
					csrfCookie = c
				}
			}
			if csrfCookie == nil {
				t.Fatal("cookie CSRF no fue emitida")
			}
			if !csrfCookie.HttpOnly {
				t.Error("cookie CSRF debe ser HttpOnly siempre")
			}
			if csrfCookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie CSRF SameSite = %v, want Lax (fail-safe, no degradable a None)", csrfCookie.SameSite)
			}
		})
	}
}

// TestCSRF_RejectsMutatingWithWrongToken complementa TestCSRF_RejectsMutatingWithoutToken (que solo
// cubre el token AUSENTE): un token presente pero que no coincide con la cookie también debe rechazarse.
func TestCSRF_RejectsMutatingWithWrongToken(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	csrf := mintCSRF(router)

	form := url.Values{
		"email":       {"a@b.com"},
		"password":    {"secret"},
		csrfFieldName: {csrf.Value + "-tampered"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /login con CSRF incorrecto status = %d, want 403", rec.Code)
	}
}
