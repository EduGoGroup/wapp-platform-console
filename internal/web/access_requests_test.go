package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// TestAccessRequests_ApproveDefaultsSystemsWhenNoneSelected cubre el default de
// access_requests_handler.go:67-69 (`if len(systems)==0`): si el operador no marca ninguna casilla de
// "Apps", la aprobación debe caer a ["wapp.bff", "wapp.edge"], no a una lista vacía que dejaría al
// usuario sin acceso a ningún sistema tras ser "aprobado".
func TestAccessRequests_ApproveDefaultsSystemsWhenNoneSelected(t *testing.T) {
	t.Parallel()

	var (
		mu           sync.Mutex
		approvedBody map[string]any
	)

	adminSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/access-requests/req-1/approve" && r.Method == http.MethodPost {
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&approvedBody)
			mu.Unlock()
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

	mu.Lock()
	defer mu.Unlock()
	got := anySliceToStrings(approvedBody["systems"])
	if !equalStringSlices(got, []string{"wapp.bff", "wapp.edge"}) {
		t.Fatalf("systems por defecto = %v, want [wapp.bff wapp.edge]", got)
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
