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

func TestAccessRequests_ListApproveReject(t *testing.T) {
	t.Parallel()

	var (
		mu            sync.Mutex
		approveCalled bool
		approvedBody  map[string]any
		rejectCalled  bool
	)

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/access-requests" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":         "req-1",
						"user_id":    "u-100",
						"email":      "operador@cliente.com",
						"origin":     "bff",
						"created_at": "2026-08-14T10:00:00Z",
					},
				},
			})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "t-1", "slug": "cliente-1", "display_name": "Cliente Uno"},
				},
			})
		case r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost:
			mu.Lock()
			approveCalled = true
			_ = json.NewDecoder(r.Body).Decode(&approvedBody)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/admin/access-requests/req-1/reject" && r.Method == http.MethodPost:
			mu.Lock()
			rejectCalled = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// 1. Listado de bandeja
	reqList := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
	reqList.AddCookie(sess)
	recList := httptest.NewRecorder()
	router.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("GET /access-requests status = %d, want 200", recList.Code)
	}
	body := recList.Body.String()
	if !strings.Contains(body, "operador@cliente.com") || !strings.Contains(body, "Cliente Uno") {
		t.Fatal("esperado email de solicitante y empresa en selector")
	}

	// 2. Aprobar
	formApprove := url.Values{
		"tenant_id": {"t-1"},
		"role":      {"operator"},
		"systems":   {"wapp.bff", "wapp.edge"},
	}
	recApprove := postFormWithCSRF(router, "/access-requests/req-1/approve", formApprove, sess)
	if recApprove.Code != http.StatusSeeOther {
		t.Fatalf("POST approve status = %d, want 303", recApprove.Code)
	}
	mu.Lock()
	if !approveCalled {
		t.Fatal("endpoint de aprobación no fue invocado")
	}
	if approvedBody["tenant_id"] != "t-1" || approvedBody["role"] != "operator" {
		t.Fatalf("cuerpo de aprobación inválido: %v", approvedBody)
	}
	if !equalStringSlices(anySliceToStrings(approvedBody["systems"]), []string{"wapp.bff", "wapp.edge"}) {
		t.Fatalf("systems enviado = %v, want [wapp.bff wapp.edge]", approvedBody["systems"])
	}
	mu.Unlock()

	// 3. Rechazar
	formReject := url.Values{
		"reason": {"No pertenece a la empresa"},
	}
	recReject := postFormWithCSRF(router, "/access-requests/req-1/reject", formReject, sess)
	if recReject.Code != http.StatusSeeOther {
		t.Fatalf("POST reject status = %d, want 303", recReject.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if !rejectCalled {
		t.Fatal("endpoint de rechazo no fue invocado")
	}
}

// TestAccessRequests_ApproveRejectsWhenNoSystemsSelected cubre C-05: si el operador no marca ninguna
// casilla de "Apps" (a propósito o por error), la aprobación NO debe inventar un default que conceda
// acceso que nadie pidió. Antes de este fix, access_requests_handler.go:67-69 caía a
// ["wapp.bff", "wapp.edge"] en ese caso; ahora debe rechazar con ?error=missing_systems y no llamar al
// endpoint de aprobación en absoluto.
func TestAccessRequests_ApproveRejectsWhenNoSystemsSelected(t *testing.T) {
	t.Parallel()

	var approveCalled atomic.Bool

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost {
			approveCalled.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	// Sin campo "systems" en el POST (ninguna casilla marcada).
	form := url.Values{"tenant_id": {"t-1"}, "role": {"operator"}}
	rec := postFormWithCSRF(router, "/access-requests/req-1/approve", form, sess)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST approve status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=missing_systems") {
		t.Fatalf("Location = %q, want error=missing_systems", loc)
	}
	if approveCalled.Load() {
		t.Fatal("no debe llamarse al endpoint de aprobación cuando no hay systems seleccionados")
	}
}

// TestAccessRequests_ApproveBadGatewayShowsPartialFeedback cubre M-11 + A-08 para el caso de un 502 de
// :8100 en la aprobación (la plataforma pudo escribir localmente antes de fallar en la propagación): el
// operador debe ver un mensaje fijo y DISTINTO del fallo genérico —nunca el texto crudo del upstream—
// para no reintentar a ciegas una acción que quizás ya se aplicó a medias.
func TestAccessRequests_ApproveBadGatewayShowsPartialFeedback(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "escrito local ok; no se pudo propagar a identity: timeout&detalle=interno",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	form := url.Values{"tenant_id": {"t-1"}, "role": {"operator"}, "systems": {"wapp.bff"}}
	rec := postFormWithCSRF(router, "/access-requests/req-1/approve", form, sess)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=approve_partial") {
		t.Fatalf("Location = %q, want error=approve_partial", loc)
	}
	if strings.Contains(loc, "timeout") || strings.Contains(loc, "detalle=interno") {
		t.Fatalf("el mensaje crudo del upstream NUNCA debe reflejarse en la URL: %q", loc)
	}

	getReq := httptest.NewRequest(http.MethodGet, loc, nil)
	getReq.AddCookie(sess)
	recGet := httptest.NewRecorder()
	router.ServeHTTP(recGet, getReq)
	if !strings.Contains(recGet.Body.String(), flashError("approve_partial")) {
		t.Fatal("esperado el mensaje de aprobación parcial visible tras el redirect")
	}
}

// TestAccessRequests_ApproveConflictSkippedShowsHonestFeedback cubre el Trabajo 2 (code review 056 ·
// T11): un 409 de :8100 con cuerpo {"local":"ok","identity":"skipped","reason":...}
// (ErrSystemsUnionUnavailable) significa que lo local SÍ quedó escrito y los systems de identity NO se
// tocaron A PROPÓSITO — no es transitorio, reintentar no lo arregla. Antes de este fix caía en
// "approve_failed" ("Intenta de nuevo"), el consejo CONTRARIO al correcto.
func TestAccessRequests_ApproveConflictSkippedShowsHonestFeedback(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"local":    "ok",
				"identity": "skipped",
				"reason":   "el usuario ya tiene una aprobación previa y no se puede leer su conjunto actual de systems en identity",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	form := url.Values{"tenant_id": {"t-1"}, "role": {"operator"}, "systems": {"wapp.bff"}}
	rec := postFormWithCSRF(router, "/access-requests/req-1/approve", form, sess)
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=approve_partial_skipped") {
		t.Fatalf("Location = %q, want error=approve_partial_skipped", loc)
	}

	getReq := httptest.NewRequest(http.MethodGet, loc, nil)
	getReq.AddCookie(sess)
	recGet := httptest.NewRecorder()
	router.ServeHTTP(recGet, getReq)
	if !strings.Contains(recGet.Body.String(), flashError("approve_partial_skipped")) {
		t.Fatal("esperado el mensaje de aprobación parcial (skipped) visible tras el redirect")
	}
}

// TestAccessRequests_ApproveAlreadyResolvedConflictStaysGeneric es la trampa del Trabajo 2: existe OTRO
// 409 legítimo ("la solicitud ya fue resuelta", ErrConflict) que NO trae el cuerpo
// {"local","identity","reason"} — es texto plano (http.Error). La distinción por FORMA del cuerpo no
// debe confundirlo con el caso "skipped": debe seguir cayendo en el fallback genérico, nunca en
// approve_partial_skipped.
func TestAccessRequests_ApproveAlreadyResolvedConflictStaysGeneric(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost {
			// Mismo shape que platformadmin.ApproveAccessRequestHandler para ErrConflict: http.Error,
			// texto plano, SIN las claves local/identity/reason.
			http.Error(w, "la solicitud ya fue resuelta o la persona ya pertenece a otra empresa", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	form := url.Values{"tenant_id": {"t-1"}, "role": {"operator"}, "systems": {"wapp.bff"}}
	rec := postFormWithCSRF(router, "/access-requests/req-1/approve", form, sess)
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "approve_partial") {
		t.Fatalf("Location = %q, un 409 sin el cuerpo parcial NO debe caer en approve_partial(_skipped)", loc)
	}
	if !strings.Contains(loc, "error=approve_failed") {
		t.Fatalf("Location = %q, want error=approve_failed (fallback genérico)", loc)
	}
	if strings.Contains(loc, "resuelta") || strings.Contains(loc, "pertenece") {
		t.Fatalf("el mensaje crudo del upstream NUNCA debe reflejarse en la URL: %q", loc)
	}
}

// TestAccessRequests_PrecargaFromCurrentSystems cubre C-05: las casillas se precargan con lo que el
// usuario YA tiene (Systems), no con el origen de la solicitud (antes: Origin=="bff" marcaba BFF). Un
// usuario con wapp.edge que pide acceso desde el BFF debe seguir mostrando Edge marcado, para que
// aprobar sin tocar nada no le quite el acceso (PUT /users/{id}/systems es declarativo).
func TestAccessRequests_PrecargaFromCurrentSystems(t *testing.T) {
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
						"systems": []string{"wapp.edge"}, "systems_known": true,
					},
				},
			})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	req := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `value="wapp.edge" checked`) {
		t.Fatalf("esperado wapp.edge precargado (checked) por Systems actuales: %s", body)
	}
	if strings.Contains(body, `value="wapp.bff" checked`) {
		t.Fatalf("wapp.bff NO debe salir precargado: el usuario no lo tiene hoy: %s", body)
	}
	if strings.Contains(body, "No se pudo leer") {
		t.Fatal("no debe mostrarse el aviso de estado desconocido cuando systems_known=true")
	}
}

// TestAccessRequests_UnknownSystemsWarnsAndLeavesUnchecked cubre el caso en que :8100 todavía no expone
// "systems"/"systems_known" (u otro fallo de lectura): las casillas deben salir DESMARCADAS —nunca "por
// si acaso"— y la pantalla debe avisar que no se pudo leer el estado actual, para que el operador no
// apruebe a ciegas.
func TestAccessRequests_UnknownSystemsWarnsAndLeavesUnchecked(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/access-requests" && r.Method == http.MethodGet:
			// Respuesta "vieja": sin "systems" ni "systems_known" en absoluto.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "req-1", "user_id": "u-100", "email": "operador@cliente.com", "origin": "bff", "created_at": "2026-08-14T10:00:00Z"},
				},
			})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	req := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `value="wapp.bff" checked`) || strings.Contains(body, `value="wapp.edge" checked`) {
		t.Fatalf("ninguna casilla debe salir marcada cuando no se pudo leer el estado actual: %s", body)
	}
	if !strings.Contains(body, "No se pudo leer") {
		t.Fatal("esperado un aviso al operador de que no se pudo leer el estado actual")
	}
}

// TestAccessRequests_NoPlatformCheckbox es una regresión: la bandeja de autoservicio NUNCA debe ofrecer
// wapp.platform (decisión de Jhoan, 2026-08-15) — la consola de plataforma no se concede desde una
// bandeja de autoservicio.
func TestAccessRequests_NoPlatformCheckbox(t *testing.T) {
	t.Parallel()

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/access-requests" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "req-1", "user_id": "u-100", "email": "operador@cliente.com", "origin": "bff", "created_at": "2026-08-14T10:00:00Z"},
				},
			})
		case r.URL.Path == "/admin/tenants" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer adminSrv.Close()

	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)
	sess := adminSessionCookie(t)

	req := httptest.NewRequest(http.MethodGet, "/access-requests", nil)
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "wapp.platform") {
		t.Fatal("la bandeja de acceso no debe ofrecer wapp.platform como casilla de autoservicio")
	}
}

func anySliceToStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
