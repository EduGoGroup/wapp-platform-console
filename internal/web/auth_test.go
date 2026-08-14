package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuth_LoginSuccess(t *testing.T) {
	t.Parallel()

	// Simular identity-core (:8200)
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" && r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["email"] == "admin@wapp.local" && body["password"] == "1234567890AB" && body["system"] == "wapp.platform" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"identity_token": "id-token-123",
					"refresh_token":  "rt-token-123",
					"token_type":     "Bearer",
					"expires_at":     time.Now().Add(time.Hour).Format(time.RFC3339),
				})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer identitySrv.Close()

	// Simular cloud-platform (:8103) para exchange
	platformSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/exchange" && r.Method == http.MethodPost {
			exp := time.Now().Add(time.Hour)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"context_token": makeAdminToken(t, exp),
				"token_type":    "Bearer",
				"expires_at":    exp.Format(time.RFC3339),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer platformSrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", platformSrv.URL, identitySrv.URL)
	router := NewRouter(cfg)

	form := url.Values{
		"email":    {"admin@wapp.local"},
		"password": {"1234567890AB"},
	}
	rec := postFormWithCSRF(router, "/login", form, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want 303. Body: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}

	foundCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			foundCookie = true
			if !c.HttpOnly {
				t.Error("cookie de sesión debe ser HttpOnly")
			}
		}
	}
	if !foundCookie {
		t.Fatal("cookie de sesión no fue emitida")
	}
}

func TestAuth_LoginInvalidCreds(t *testing.T) {
	t.Parallel()
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	form := url.Values{
		"email":    {"wrong@wapp.local"},
		"password": {"wrongpass"},
	}
	rec := postFormWithCSRF(router, "/login", form, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /login status = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Credenciales inválidas") {
		t.Fatalf("mensaje de error esperado en cuerpo: %s", body)
	}
}

func TestAuth_LogoutClearsCookie(t *testing.T) {
	t.Parallel()
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	sess := adminSessionCookie(t)
	rec := postFormWithCSRF(router, "/logout", url.Values{}, sess)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}

	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cookie de sesión no fue caducada")
	}
}

func TestAuth_UnauthenticatedRedirectsToLogin(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / sin auth status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}
}
