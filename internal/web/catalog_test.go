package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
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

// palabrasDeRotulo son las palabras que la etiqueta del plan puede llevar ADEMÁS del identificador:
// la marca de que el plan es implícito. Deliberadamente cortísima — si alguien necesita ampliarla,
// que sea una decisión consciente y no un plan inventado colándose como prosa.
var palabrasDeRotulo = map[string]bool{
	"por":     true,
	"defecto": true,
}

// planLabels extrae el texto de cada elemento marcado con data-field="plan" en el HTML renderizado.
// Ese marcador acota la búsqueda al rótulo del plan y solo a él: la página de detalle lista justo
// debajo las features (`cart_basic`, `intakes_export`…), que no son identificadores de plan y
// dispararían falsos positivos si se barriera el documento entero.
func planLabels(t *testing.T, html string) []string {
	t.Helper()

	// El marcador va en el elemento MÁS INTERNO, el que solo contiene texto: por eso basta con
	// leer hasta el siguiente '<'.
	re := regexp.MustCompile(`<[a-zA-Z]+[^>]*\bdata-field="plan"[^>]*>([^<]*)<`)
	var labels []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		labels = append(labels, strings.TrimSpace(m[1]))
	}
	return labels
}

// TestTenantViews_PlanMostradoExisteEnElCatalogo cierra el hueco que dejaron los dos tests de arriba:
// solo miraban los `value` de los `<select>`, así que el fantasma `standard` sobrevivió en los
// FALLBACKS de presentación de `tenants.html` y `tenant_detail.html`. Una empresa con `plan_id` NULL
// se anunciaba como `standard` — un plan que no existe en `public.plans` — mientras la misma ficha
// listaba debajo las features de `basic`.
//
// La verdad la fija el cloud: `COALESCE(plan_id, 'basic')` en las dos consultas de entitlements
// (wapp-cloud-platform, internal/entitlements/postgres.go:151 y :235). El rótulo tiene que nombrar
// un plan del catálogo real, y el test falla si nombra cualquier otra cosa.
func TestTenantViews_PlanMostradoExisteEnElCatalogo(t *testing.T) {
	t.Parallel()

	// Empresa SIN plan asignado: `plan_id` a null es exactamente el caso que se vio mal en UAT.
	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"id": "t-sin-plan", "slug": "empresa-sin-plan", "display_name": "Empresa Sin Plan",
					"plan_id": nil, "revoked_at": nil},
			}})
		case r.URL.Path == "/admin/tenants/t-sin-plan" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "t-sin-plan", "slug": "empresa-sin-plan", "display_name": "Empresa Sin Plan",
				"plan_id": nil, "revoked_at": nil, "created_at": "2026-08-14T00:00:00Z",
				"installations_count": 0,
				// Las features que el cloud devuelve para un plan NULL son, precisamente, las de
				// `basic`: si el rótulo dijera otro plan, la ficha se contradiría a sí misma.
				"features": []string{"cart_basic", "intakes_export", "menu", "survey"},
			})
		case r.URL.Path == "/admin/tenants/t-sin-plan/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	router := NewRouter(testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200"))
	sess := adminSessionCookie(t)

	casos := []struct {
		nombre string
		ruta   string
	}{
		{"listado", "/"},
		{"detalle", "/tenants/t-sin-plan"},
	}

	tokenRe := regexp.MustCompile(`[a-z][a-z0-9_]*`)

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, caso.ruta, nil)
			req.AddCookie(sess)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, esperado 200", caso.ruta, rec.Code)
			}

			labels := planLabels(t, rec.Body.String())
			if len(labels) == 0 {
				t.Fatalf("no se encontró ningún rótulo de plan (data-field=\"plan\") en %s: "+
					"si se quitó el marcador, este test dejó de vigilar la página", caso.ruta)
			}

			for _, label := range labels {
				var nombraUnPlan bool
				for _, token := range tokenRe.FindAllString(strings.ToLower(label), -1) {
					switch {
					case planesSembrados[token]:
						nombraUnPlan = true
					case palabrasDeRotulo[token]:
						// marca de plan implícito, no es un identificador
					default:
						t.Errorf("%s muestra el plan %q, y %q NO existe en public.plans (0032/0039): "+
							"la consola estaría anunciando un plan que la plataforma no aplica",
							caso.ruta, label, token)
					}
				}
				if !nombraUnPlan {
					t.Errorf("%s muestra %q, que no nombra ningún plan del catálogo: el operador no "+
						"puede saber qué aplica la plataforma (plan_id NULL ⇒ 'basic' por el "+
						"COALESCE de entitlements/postgres.go:151)", caso.ruta, label)
				}
			}
		})
	}
}
