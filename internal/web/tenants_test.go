package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestTenants_RevokeRequiresSlugConfirmation(t *testing.T) {
	t.Parallel()
	var revokeCalled bool

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/tenants/revoke" && r.Method == http.MethodPost {
			revokeCalled = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// 1. Slug incorrecto -> no llama al backend y redirige con error
	formMismatch := url.Values{
		"expected_slug": {"empresa-alfa"},
		"slug_confirm":  {"otro-slug"},
		"reason":        {"Mora"},
	}
	recMismatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMismatch, sess)
	if recMismatch.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recMismatch.Code)
	}
	if revokeCalled {
		t.Fatal("Revoke no debía llamarse con slug incorrecto")
	}

	// 2. Slug correcto -> llama al backend
	formMatch := url.Values{
		"expected_slug": {"empresa-alfa"},
		"slug_confirm":  {"empresa-alfa"},
		"reason":        {"Mora"},
	}
	recMatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMatch, sess)
	if recMatch.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recMatch.Code)
	}
	if !revokeCalled {
		t.Fatal("Revoke debía ser llamado con slug correcto")
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
