package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestTemplates_NoInlineStyles cierra A-04: la CSP (`style-src 'self' 'nonce-%s'`) no admite atributos
// `style="..."` — style-src-attr hereda de style-src y un nonce nunca habilita un atributo. Este test
// renderiza cada página autenticada y falla si reaparece un `style="` inline, para que una regresión
// futura no vuelva a servir la consola "a medias" sin que ningún gate lo note.
func TestTemplates_NoInlineStyles(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "t-1", "slug": "empresa-alfa", "display_name": "Empresa Alfa", "plan_id": "basic", "revoked_at": nil},
				},
			})
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "t-1", "slug": "empresa-alfa", "display_name": "Empresa Alfa", "plan_id": "basic",
				"revoked_at": nil, "created_at": "2026-08-14T00:00:00Z", "features": []string{"cart_basic"},
			})
		case r.URL.Path == "/admin/tenants/t-1/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.URL.Path == "/admin/access-requests" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"id": "req-1", "user_id": "u-1", "email": "op@cliente.com", "origin": "bff", "created_at": "2026-08-14T10:00:00Z"},
			}})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "t-new", "slug": "nueva-empresa"})
		case r.URL.Path == "/admin/tenants/t-1/enrollment-codes" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "ACT-11111-22222", "expires_at": "2026-08-15T00:00:00Z"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	pages := []struct {
		name string
		req  func() *http.Request
	}{
		{"login", func() *http.Request { return httptest.NewRequest(http.MethodGet, "/login", nil) }},
		{"tenants", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.AddCookie(sess)
			return r
		}},
		{"tenant_detail", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/tenants/t-1", nil)
			r.AddCookie(sess)
			return r
		}},
		{"tenant_new", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/tenants/new", nil)
			r.AddCookie(sess)
			return r
		}},
		{"access_requests", func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
			r.AddCookie(sess)
			return r
		}},
	}

	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, p.req())
			if rec.Code != http.StatusOK {
				t.Fatalf("GET status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), `style="`) {
				t.Errorf("la página %q sirve un atributo style= inline; la CSP lo descarta en el navegador (A-04)", p.name)
			}
		})
	}

	// tenant_created: solo se renderiza tras un alta exitosa.
	form := url.Values{"slug": {"nueva-empresa"}, "display_name": {"Nueva Empresa"}, "plan_id": {"basic"}}
	recCreate := postFormWithCSRF(router, "/tenants/new", form, sess)
	if strings.Contains(recCreate.Body.String(), `style="`) {
		t.Error("la página tenant_created sirve un atributo style= inline; la CSP lo descarta en el navegador (A-04)")
	}

	// tenant_detail con código de enrolamiento recién emitido: es el bloque con más marcado propio
	// (enrollment-code-box) y el que el informe señaló como "el fallo se hace más engañoso".
	recCode := postFormWithCSRF(router, "/tenants/t-1/enrollment-codes", url.Values{}, sess)
	if strings.Contains(recCode.Body.String(), `style="`) {
		t.Error("la página tenant_detail con código de enrolamiento sirve un atributo style= inline; la CSP lo descarta en el navegador (A-04)")
	}
}
