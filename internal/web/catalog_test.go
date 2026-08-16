package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// Estos dos tests cierran un fallo que llevaba vivo desde el primer commit de la consola y que ningún
// gate veía: los `<option>` de los formularios ofrecían identificadores INVENTADOS. El operador elegía
// el valor por defecto y el cloud contestaba un error opaco:
//
//   - "Plan inicial" ofrecía `standard` (preseleccionado) y `enterprise`, que NO existen en
//     `public.plans`. `tenants.plan_id` es FK a `plans(id)` => violación de FK => 500 sin explicación
//     (el mapeo de errores del cloud solo reconoce la violación de UNIQUE).
//   - "Rol" ofrecía `admin`, que NO existe en `public.iam_roles`. `resolveRoleID` busca por `name` o
//     UUID y devuelve ErrInvalidInput => 400: nombrar al administrador de una empresa era IMPOSIBLE
//     desde la UI.
//
// La lista sigue duplicada del catálogo del cloud (deuda anotada en las propias plantillas). Lo que
// aportan estos tests es el lazo que faltaba: si alguien añade o renombra una opción sin que exista
// en el catálogo real, el paquete se pone rojo aquí en vez de en producción.
//
// Fuente de verdad de ambos conjuntos: las migraciones de `cloud/wapp-cloud-platform`, en
// `internal/platform/storage/postgres/migrations/structure/`.

// planesSembrados es el catálogo `public.plans` completo: 0032_entitlements.sql siembra `basic` y
// `pro`; 0039_seed_plan_taxonomy.sql añade `commerce`, `advisor_ai` y `advisor_ai_pro`. Ninguna
// migración posterior borra ni renombra filas de `plans`.
var planesSembrados = map[string]bool{
	"basic":          true,
	"pro":            true,
	"commerce":       true,
	"advisor_ai":     true,
	"advisor_ai_pro": true,
}

// rolesSembrados es el catálogo `public.iam_roles` de PLANTILLAS globales (tenant_id NULL):
// 0015_iam_roles.sql siembra `tenant_admin`, `operator` y `viewer`; 0059_platform_admin.sql añade
// `platform_admin`, que a propósito NO se ofrece en la bandeja (ver rolesOfrecibles).
var rolesSembrados = map[string]bool{
	"tenant_admin":   true,
	"operator":       true,
	"viewer":         true,
	"platform_admin": true,
}

// rolesOfrecibles son los roles que la bandeja de acceso PUEDE conceder. Es un subconjunto estricto de
// rolesSembrados: `platform_admin` es el rol de la propia plataforma (tenants.revoke.any /
// tenants.restore.any), no el de un cliente, y este formulario adscribe al usuario a UN tenant.
// Ofrecerlo convertiría la bandeja de autoservicio en una vía de escalada de privilegios.
var rolesOfrecibles = map[string]bool{
	"tenant_admin": true,
	"operator":     true,
	"viewer":       true,
}

// optionValues extrae los `value` de los <option> del <select name="<name>"> del HTML renderizado.
func optionValues(t *testing.T, html, selectName string) []string {
	t.Helper()

	selectRe := regexp.MustCompile(`(?s)<select[^>]*\bname="` + regexp.QuoteMeta(selectName) + `"[^>]*>(.*?)</select>`)
	block := selectRe.FindStringSubmatch(html)
	if block == nil {
		t.Fatalf("no se encontró el <select name=%q> en la página renderizada", selectName)
	}

	optionRe := regexp.MustCompile(`<option[^>]*\bvalue="([^"]*)"`)
	var values []string
	for _, m := range optionRe.FindAllStringSubmatch(block[1], -1) {
		if m[1] == "" { // placeholder tipo "Selecciona..." (disabled)
			continue
		}
		values = append(values, m[1])
	}
	if len(values) == 0 {
		t.Fatalf("el <select name=%q> se renderizó sin ninguna opción con value", selectName)
	}
	return values
}

// selectedValue devuelve el value de la opción marcada `selected` en ese select.
func selectedValue(t *testing.T, html, selectName string) string {
	t.Helper()

	selectRe := regexp.MustCompile(`(?s)<select[^>]*\bname="` + regexp.QuoteMeta(selectName) + `"[^>]*>(.*?)</select>`)
	block := selectRe.FindStringSubmatch(html)
	if block == nil {
		t.Fatalf("no se encontró el <select name=%q> en la página renderizada", selectName)
	}
	selRe := regexp.MustCompile(`<option[^>]*\bvalue="([^"]*)"[^>]*\bselected`)
	m := selRe.FindStringSubmatch(block[1])
	if m == nil {
		return ""
	}
	return m[1]
}

// TestTenantNew_PlanOptionsExistenEnElCatalogo falla si el formulario de alta ofrece un plan que no
// está sembrado en `public.plans` — es decir, si vuelve a proponer una FK que revienta.
func TestTenantNew_PlanOptionsExistenEnElCatalogo(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	router := NewRouter(testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	req := httptest.NewRequest(http.MethodGet, "/tenants/new", nil)
	req.AddCookie(adminSessionCookie(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tenants/new = %d, esperado 200", rec.Code)
	}

	for _, plan := range optionValues(t, rec.Body.String(), "plan_id") {
		if !planesSembrados[plan] {
			t.Errorf("el alta ofrece el plan %q, que NO existe en public.plans (0032/0039): "+
				"tenants.plan_id es FK a plans(id) y el alta fallará con un 500 opaco", plan)
		}
	}

	sel := selectedValue(t, rec.Body.String(), "plan_id")
	if sel == "" {
		t.Error("ningún plan viene preseleccionado: el operador puede enviar el formulario sin decidir")
	} else if !planesSembrados[sel] {
		t.Errorf("el plan preseleccionado %q no existe en public.plans: el camino por defecto está roto", sel)
	}
}

// TestAccessRequests_RoleOptionsExistenEnElCatalogo falla si la bandeja ofrece un rol que
// `resolveRoleID` no sabe resolver (400), o si empieza a ofrecer `platform_admin`.
func TestAccessRequests_RoleOptionsExistenEnElCatalogo(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/access-requests" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id": "req-1", "user_id": "u-100", "email": "operador@cliente.com",
						"origin": "bff", "created_at": "2026-08-14T10:00:00Z",
						"systems": []string{"wapp.bff"}, "systems_known": true,
					},
				},
			})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"id": "t-1", "slug": "empresa-alfa", "display_name": "Empresa Alfa", "plan_id": "basic", "revoked_at": nil},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	router := NewRouter(testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	req := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
	req.AddCookie(adminSessionCookie(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /access-requests = %d, esperado 200", rec.Code)
	}

	roles := optionValues(t, rec.Body.String(), "role")
	for _, role := range roles {
		if !rolesSembrados[role] {
			t.Errorf("la bandeja ofrece el rol %q, que NO existe en public.iam_roles (0015/0059): "+
				"resolveRoleID devolverá ErrInvalidInput y la aprobación fallará con 400", role)
			continue
		}
		if !rolesOfrecibles[role] {
			t.Errorf("la bandeja ofrece el rol %q, que existe pero es de la PLATAFORMA, no de un "+
				"cliente: concederlo desde el autoservicio es una escalada de privilegios", role)
		}
	}

	// El administrador de la empresa TIENE que poder nombrarse desde aquí: es el rol con el que
	// arranca cada cliente nuevo. Sin él, el alta por consola queda a medias.
	var tieneAdmin bool
	for _, role := range roles {
		if role == "tenant_admin" {
			tieneAdmin = true
		}
	}
	if !tieneAdmin {
		t.Error("la bandeja no ofrece `tenant_admin`: no hay forma de nombrar al administrador de una empresa")
	}

	if sel := selectedValue(t, rec.Body.String(), "role"); sel != "" && !rolesOfrecibles[sel] {
		t.Errorf("el rol preseleccionado %q no es concedible: el camino por defecto está roto", sel)
	}
}
