package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
						"plan_id":      "basic",
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
				"plan_id":             "basic",
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

// TestTenants_RevokeAndSlugMismatchAreVisibleToOperator cubre A-08: hoy la página se recarga muda tras
// un corte, sea que tuvo éxito o que el slug no coincidió, y el operador no puede distinguir "no hizo
// nada raro" de "cortó de verdad". Este test sigue la redirección de cada camino y confirma que el
// mensaje fijo correspondiente aparece en el HTML resultante.
func TestTenants_RevokeAndSlugMismatchAreVisibleToOperator(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "t-1", "slug": "empresa-alfa", "display_name": "Empresa Alfa",
			})
		case r.URL.Path == "/admin/tenants/t-1/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.URL.Path == "/admin/tenants/revoke" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// Camino 1: slug incorrecto -> el corte no se ejecuta y el operador lo VE.
	formMismatch := url.Values{"slug_confirm": {"otro-slug"}, "reason": {"Mora"}}
	recMismatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMismatch, sess)
	loc := recMismatch.Header().Get("Location")

	getMismatch := httptest.NewRequest(http.MethodGet, loc, nil)
	getMismatch.AddCookie(sess)
	recGetMismatch := httptest.NewRecorder()
	router.ServeHTTP(recGetMismatch, getMismatch)
	if !strings.Contains(recGetMismatch.Body.String(), flashError("slug_mismatch")) {
		t.Fatalf("esperado el mensaje de slug_mismatch visible tras el redirect a %q", loc)
	}

	// Camino 2: slug correcto -> el corte se ejecuta y el operador lo VE.
	formMatch := url.Values{"slug_confirm": {"empresa-alfa"}, "reason": {"Mora"}}
	recMatch := postFormWithCSRF(router, "/tenants/t-1/revoke", formMatch, sess)
	loc2 := recMatch.Header().Get("Location")

	getMatch := httptest.NewRequest(http.MethodGet, loc2, nil)
	getMatch.AddCookie(sess)
	recGetMatch := httptest.NewRecorder()
	router.ServeHTTP(recGetMatch, getMatch)
	if !strings.Contains(recGetMatch.Body.String(), flashSuccess("revoked")) {
		t.Fatalf("esperado el mensaje de éxito visible tras el redirect a %q", loc2)
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
				"plan_id":      "basic",
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
		"plan_id":      {"basic"},
	}
	recCreate := postFormWithCSRF(router, "/tenants/new", form, sess)
	if recCreate.Code != http.StatusOK {
		t.Fatalf("POST /tenants/new status = %d, want 200. Body: %s", recCreate.Code, recCreate.Body.String())
	}
	if !strings.Contains(recCreate.Body.String(), "Empresa Creada con Éxito") {
		t.Fatal("esperado mensaje de éxito en creación")
	}

	// 2. Emisión de código. El POST ya no pinta la pantalla: emite y redirige (M-10), así que el
	// código se ve en el GET siguiente.
	recCode := postFormWithCSRF(router, "/tenants/t-new/enrollment-codes", url.Values{}, sess)
	if recCode.Code != http.StatusSeeOther {
		t.Fatalf("POST /tenants/t-new/enrollment-codes status = %d, want 303", recCode.Code)
	}
	if loc := recCode.Header().Get("Location"); loc != "/tenants/t-new/enrollment-code" {
		t.Fatalf("Location = %q, want /tenants/t-new/enrollment-code", loc)
	}
	recPantalla := seguirRedirect(t, router, recCode, sess)
	if recPantalla.Code != http.StatusOK {
		t.Fatalf("GET de la pantalla del código status = %d, want 200", recPantalla.Code)
	}
	if !strings.Contains(recPantalla.Body.String(), "ACT-12345-67890") {
		t.Fatal("esperado código de activación en pantalla")
	}
}

// TestTenants_IssueEnrollmentCode_SendsTTLKeyNotTTLSeconds fija el contrato de A-03: el servidor
// (platformadmin/handlers.go) lee la clave JSON "ttl", no "ttl_seconds". encoding/json ignora las claves
// desconocidas sin error, así que un desajuste aquí hace que el TTL nunca llegue y el código viva siempre
// el default del servidor (24h) sin ningún aviso ni error visible. Este test no depende de :8100 real:
// verifica el payload que el cliente arma.
func TestTenants_IssueEnrollmentCode_SendsTTLKeyNotTTLSeconds(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		body map[string]any
	)

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants/t-1/enrollment-codes" && r.Method == http.MethodPost:
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":       "ACT-CONTRACT",
				"expires_at": "2026-08-15T05:00:00Z",
			})
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "t-1", "slug": "cliente-1", "display_name": "Cliente Uno",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	rec := postFormWithCSRF(router, "/tenants/t-1/enrollment-codes", url.Values{}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/tenants/t-1/enrollment-code" {
		t.Fatalf("Location = %q, want /tenants/t-1/enrollment-code", loc)
	}
	// El PRG entero: el payload correcto no sirve de nada si la pantalla siguiente no llega a pintar
	// el código que se emitió con él.
	pantalla := seguirRedirect(t, router, rec, sess)
	if pantalla.Code != http.StatusOK {
		t.Fatalf("GET de la pantalla del código status = %d, want 200", pantalla.Code)
	}
	if !strings.Contains(pantalla.Body.String(), "ACT-CONTRACT") {
		t.Fatalf("la pantalla no muestra el código emitido. Body: %s", pantalla.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if _, hasWrongKey := body["ttl_seconds"]; hasWrongKey {
		t.Fatalf("el payload no debe llevar 'ttl_seconds' (el servidor lo ignora en silencio): %v", body)
	}
	ttl, ok := body["ttl"]
	if !ok {
		t.Fatalf("el payload debe llevar la clave 'ttl': %v", body)
	}
	if ttl != float64(86400) {
		t.Fatalf("ttl = %v, want 86400", ttl)
	}
}

// TestTenants_IssueEnrollmentCode_SurvivesTenantRereadFailure cubre la mitad contenida de M-10: el código
// ya se emitió (es de un solo uso) cuando la relectura de GetTenant falla, así que perderlo sería peor
// que mostrar la página con datos de empresa incompletos. Antes de este fix, el error se descartaba y
// `tenant` quedaba nil: html/template abortaba al evaluar `.Tenant.DisplayName` sobre un puntero nil, y
// como Gin ya había escrito `200 OK`, la página quedaba truncada.
func TestTenants_IssueEnrollmentCode_SurvivesTenantRereadFailure(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants/t-1/enrollment-codes" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":       "ACT-SURVIVES",
				"expires_at": "2026-08-15T05:00:00Z",
			})
		case r.URL.Path == "/admin/tenants/t-1" && r.Method == http.MethodGet:
			// La relectura del tenant falla (p.ej. :8100 con hipo momentáneo).
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	rec := postFormWithCSRF(router, "/tenants/t-1/enrollment-codes", url.Values{}, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/tenants/t-1/enrollment-code" {
		t.Fatalf("Location = %q, want /tenants/t-1/enrollment-code", loc)
	}
	// El POST ya no renderiza (M-10, POST-Redirect-GET): la pantalla, y la relectura del tenant que
	// falla, están en el GET siguiente. Lo que este test vigila no cambió: que el código se enseñe
	// igual y la página no quede a medias.
	pantalla := seguirRedirect(t, router, rec, sess)
	if pantalla.Code != http.StatusOK {
		t.Fatalf("GET de la pantalla del código status = %d, want 200", pantalla.Code)
	}
	body := pantalla.Body.String()
	if !strings.Contains(body, "ACT-SURVIVES") {
		t.Fatalf("el código emitido debe mostrarse aunque falle la relectura del tenant, body: %s", body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("la página no debe quedar truncada cuando falla la relectura del tenant")
	}
}
