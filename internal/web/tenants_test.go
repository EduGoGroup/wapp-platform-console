package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTenants_ListAndDetail(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":           "t-1",
						"slug":         "empresa-alfa",
						"display_name": "Empresa Alfa",
						"plan_id":      "standard",
						"revoked_at":   nil,
					},
				},
				"limit":  50,
				"offset": 0,
			})
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                  "t-1",
				"slug":                "empresa-alfa",
				"display_name":        "Empresa Alfa",
				"plan_id":             "standard",
				"revoked_at":          nil,
				"created_at":          "2026-08-14T00:00:00Z",
				"installations_count": 1,
				"features":            []string{"cart_basic"},
			})
		case r.URL.Path == "/admin/tenants/t-1/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"edge_id":       "edge-001",
						"sessions":      2,
						"last_seen_at":  "2026-08-14T12:00:00Z",
						"lease_revoked": false,
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// 1. Listado
	reqList := httptest.NewRequest(http.MethodGet, "/", nil)
	reqList.AddCookie(sess)
	recList := httptest.NewRecorder()
	router.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", recList.Code)
	}
	if !strings.Contains(recList.Body.String(), "Empresa Alfa") {
		t.Fatal("esperado 'Empresa Alfa' en el listado")
	}

	// 2. Detalle
	reqDetail := httptest.NewRequest(http.MethodGet, "/tenants/t-1", nil)
	reqDetail.AddCookie(sess)
	recDetail := httptest.NewRecorder()
	router.ServeHTTP(recDetail, reqDetail)

	if recDetail.Code != http.StatusOK {
		t.Fatalf("GET /tenants/t-1 status = %d, want 200", recDetail.Code)
	}
	body := recDetail.Body.String()
	if !strings.Contains(body, "empresa-alfa") || !strings.Contains(body, "edge-001") {
		t.Fatal("esperado slug y edge_id en el detalle")
	}
}

// TestTenants_RevokeRequiresSlugConfirmation cierra C-06: la barrera humana del corte debe validarse
// contra el slug REAL del tenant `id`, resuelto del lado servidor (GET /admin/tenants/{id}), nunca
// contra un campo que viaje en el propio POST. El caso 3 es el que de verdad prueba esto: un
// `slug_confirm` que sea el slug legítimo de OTRA empresa no puede colar como confirmación de esta.
func TestTenants_RevokeRequiresSlugConfirmation(t *testing.T) {
	t.Parallel()
	var revokeCalled atomic.Bool

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "t-1",
				"slug":         "empresa-alfa",
				"display_name": "Empresa Alfa",
			})
		case r.URL.Path == "/admin/tenants/revoke" && r.Method == http.MethodPost:
			revokeCalled.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// 1. Slug incorrecto (no es el slug de ninguna empresa real) -> no llama al backend y redirige con error
	revokeCalled.Store(false)
	formMismatch := url.Values{
		"slug_confirm": {"otro-slug"},
		"reason":       {"Mora"},
	}
	recMismatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMismatch, sess)
	if recMismatch.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recMismatch.Code)
	}
	if revokeCalled.Load() {
		t.Fatal("Revoke no debía llamarse con slug incorrecto")
	}

	// 2. Slug correcto para la empresa objetivo (t-1, "empresa-alfa") -> llama al backend
	revokeCalled.Store(false)
	formMatch := url.Values{
		"slug_confirm": {"empresa-alfa"},
		"reason":       {"Mora"},
	}
	recMatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMatch, sess)
	if recMatch.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recMatch.Code)
	}
	if !revokeCalled.Load() {
		t.Fatal("Revoke debía ser llamado con slug correcto")
	}

	// 3. Slug correcto, pero de OTRA empresa ("empresa-beta") -> no debe colar como confirmación de t-1
	//    ("empresa-alfa"). Esta es la prueba que el bug original (comparar el form contra sí mismo)
	//    NO podía superar: cualquier string no vacío que "coincidiera consigo mismo" pasaba.
	revokeCalled.Store(false)
	formCrossTenant := url.Values{
		"slug_confirm": {"empresa-beta"},
		"reason":       {"Mora"},
	}
	recCrossTenant := postFormWithCSRF(router, "/tenants/t-1/revoke", formCrossTenant, sess)
	if recCrossTenant.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recCrossTenant.Code)
	}
	if revokeCalled.Load() {
		t.Fatal("Revoke no debía llamarse con el slug de otra empresa")
	}
}

// TestTenants_RevokeUnreachableTenantDoesNotCallRevoke cubre el camino fail-closed: si el servidor no
// puede resolver el slug real del tenant objetivo (API admin caída, id inexistente), el corte no debe
// proceder aunque el operador haya escrito cualquier cosa.
func TestTenants_RevokeUnreachableTenantDoesNotCallRevoke(t *testing.T) {
	t.Parallel()
	var revokeCalled atomic.Bool

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/admin/tenants/t-missing" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/admin/tenants/revoke" && r.Method == http.MethodPost:
			revokeCalled.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	form := url.Values{"slug_confirm": {"lo-que-sea"}, "reason": {"Mora"}}
	rec := postFormWithCSRF(router, "/tenants/t-missing/revoke", form, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if revokeCalled.Load() {
		t.Fatal("Revoke no debía llamarse cuando el tenant objetivo no se pudo resolver")
	}
}

func TestTenants_CreateAndIssueEnrollmentCode(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":   "t-new",
				"slug": "nueva-empresa",
			})
		case r.URL.Path == "/admin/tenants/t-new/enrollment-codes" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":       "ACT-12345-67890",
				"expires_at": "2026-08-15T00:00:00Z",
			})
		case r.URL.Path == "/admin/tenants/t-new" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "t-new",
				"slug":         "nueva-empresa",
				"display_name": "Nueva Empresa",
				"plan_id":      "standard",
			})
		case r.URL.Path == "/admin/tenants/t-new/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// 1. Alta de empresa
	form := url.Values{
		"slug":         {"nueva-empresa"},
		"display_name": {"Nueva Empresa C.A."},
		"plan_id":      {"standard"},
	}
	recCreate := postFormWithCSRF(router, "/tenants/new", form, sess)
	if recCreate.Code != http.StatusOK {
		t.Fatalf("POST /tenants/new status = %d, want 200. Body: %s", recCreate.Code, recCreate.Body.String())
	}
	if !strings.Contains(recCreate.Body.String(), "Empresa Creada con Éxito") {
		t.Fatal("esperado mensaje de éxito en creación")
	}

	// 2. Emisión de código
	recCode := postFormWithCSRF(router, "/tenants/t-new/enrollment-codes", url.Values{}, sess)
	if recCode.Code != http.StatusOK {
		t.Fatalf("POST /tenants/t-new/enrollment-codes status = %d, want 200", recCode.Code)
	}
	if !strings.Contains(recCode.Body.String(), "ACT-12345-67890") {
		t.Fatal("esperado código de activación en pantalla")
	}
}
