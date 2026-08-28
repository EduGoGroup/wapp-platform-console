package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	sharedweb "github.com/EduGoGroup/wapp-shared/web"
	webgin "github.com/EduGoGroup/wapp-shared/web/gin"
	"github.com/gin-gonic/gin"
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
		"email":                 {"a@b.com"},
		"password":              {"secret"},
		sharedweb.CSRFFieldName: {csrf.Value + "-tampered"},
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

// TestCSRF_RejectsPatchWithoutToken cierra la lista de métodos mutantes del CODE-REVIEW-2026-08-15
// (§5): faltaba PATCH junto a POST/PUT/DELETE, así que una ruta PATCH quedaba fuera de la protección
// CSRF. Ninguna ruta real de la consola usa hoy PATCH, así que se monta un router mínimo solo con el
// middleware y una ruta de sonda, igual que se probaría cualquier método mutante nuevo.
func TestCSRF_RejectsPatchWithoutToken(t *testing.T) {
	t.Parallel()
	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")

	probe := gin.New()
	probe.Use(webgin.CSRF(csrfOptions(cfg)))
	probe.PATCH("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPatch, "/probe", nil)
	rec := httptest.NewRecorder()
	probe.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH /probe sin CSRF status = %d, want 403", rec.Code)
	}
}

// TestLogin_AlwaysRendersCSRFField cierra el hallazgo #5 de CODE-REVIEW-2026-08-15: el campo oculto
// estaba envuelto en `{{ if .CSRFToken }}`, así que un token vacío (bug futuro en el middleware, no
// hoy) haría desaparecer el campo del formulario y el login fallaría con un 403 inexplicable en vez
// de fallar de forma visible (campo presente pero vacío). El campo debe estar SIEMPRE en el HTML.
func TestLogin_AlwaysRendersCSRFField(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatal("el formulario de login debe llevar SIEMPRE el campo oculto csrf_token, sin condicional")
	}

	var csrfCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatal("cookie CSRF no fue emitida en GET /login")
	}
	if !strings.Contains(body, `value="`+csrfCookie.Value+`"`) {
		t.Fatal("el campo oculto csrf_token debe llevar el valor real del token, no vacío")
	}
}
