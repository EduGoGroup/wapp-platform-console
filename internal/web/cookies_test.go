package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	sharedweb "github.com/EduGoGroup/wapp-shared/web"
)

// TestCookieNames_SonLasDeEstaConsolaYNoLasDelBFF custodia el punto más frágil de consumir el
// middleware compartido: en `wapp-shared/web` el nombre de cookie es un PARÁMETRO, no una constante
// de paquete. Si esta consola se olvidara de pasarlo, el módulo caería a sus valores por defecto
// —`wapp_csrf`, que es EXACTAMENTE la cookie CSRF del BFF del cliente (wapp-guardian-bff), y
// `wapp_session`— y las dos consolas del ecosistema se pisarían la cookie en el mismo navegador.
//
// El test afirma los LITERALES, no las constantes del paquete: comprobar `c.Name == csrfCookieName`
// pasaría igual con la constante cambiada, que es justo la regresión que hay que cazar. Y lo hace
// sobre las cookies que salen por el cable en los dos caminos que las emiten (GET /login siembra la
// CSRF; POST /login la de sesión).
func TestCookieNames_SonLasDeEstaConsolaYNoLasDelBFF(t *testing.T) {
	t.Parallel()

	// Los nombres del BFF y los defaults del módulo: ninguno puede salir de esta consola.
	prohibidos := map[string]string{
		"wapp_csrf":             "es la cookie CSRF del BFF del cliente Y el default del módulo",
		"wapp_guardian_session": "es la cookie de sesión del BFF del cliente",
		"wapp_guardian_csrf":    "es del perímetro del cliente",
		"wapp_session":          "es el default del módulo, no el nombre de esta consola",
	}
	if sharedweb.DefaultCSRFCookieName != "wapp_csrf" || sharedweb.DefaultSessionCookieName != "wapp_session" {
		t.Fatalf("los defaults del módulo cambiaron (%q/%q): revisa que sigan sin colisionar con esta consola",
			sharedweb.DefaultCSRFCookieName, sharedweb.DefaultSessionCookieName)
	}

	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"identity_token": "id-token-123",
			"refresh_token":  "rt-token-123",
			"token_type":     "Bearer",
		})
	}))
	defer identitySrv.Close()

	platformSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exp := time.Now().Add(time.Hour)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"context_token": makeAdminToken(t, exp),
			"token_type":    "Bearer",
			"expires_at":    exp.Format(time.RFC3339),
		})
	}))
	defer platformSrv.Close()

	router := NewRouter(testConfig("http://127.0.0.1:8100", platformSrv.URL, identitySrv.URL))

	// GET /login: siembra la cookie CSRF.
	recGet := httptest.NewRecorder()
	router.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/login", nil))

	// POST /login: emite además la de sesión.
	recPost := postFormWithCSRF(router, "/login", url.Values{
		"email":    {"admin@wapp.local"},
		"password": {"test-only-password"},
	}, nil)
	if recPost.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want 303. Body: %s", recPost.Code, recPost.Body.String())
	}

	vistas := map[string]bool{}
	for _, rec := range []*httptest.ResponseRecorder{recGet, recPost} {
		for _, c := range rec.Result().Cookies() {
			vistas[c.Name] = true
			if motivo, prohibida := prohibidos[c.Name]; prohibida {
				t.Errorf("la consola emitió la cookie %q, que %s: los dos perímetros compartirían cookie",
					c.Name, motivo)
			}
		}
	}

	for _, esperada := range []string{"wapp_platform_csrf", "wapp_platform_session"} {
		if !vistas[esperada] {
			t.Errorf("la cookie %q no se emitió; cookies vistas: %v", esperada, vistas)
		}
	}
}
