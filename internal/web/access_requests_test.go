package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAccessRequests_ListApproveReject(t *testing.T) {
	t.Parallel()

	var (
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
			approveCalled = true
			_ = json.NewDecoder(r.Body).Decode(&approvedBody)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/admin/access-requests/req-1/reject" && r.Method == http.MethodPost:
			rejectCalled = true
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
	if !approveCalled {
		t.Fatal("endpoint de aprobación no fue invocado")
	}
	if approvedBody["tenant_id"] != "t-1" || approvedBody["role"] != "operator" {
		t.Fatalf("cuerpo de aprobación inválido: %v", approvedBody)
	}

	// 3. Rechazar
	formReject := url.Values{
		"reason": {"No pertenece a la empresa"},
	}
	recReject := postFormWithCSRF(router, "/access-requests/req-1/reject", formReject, sess)
	if recReject.Code != http.StatusSeeOther {
		t.Fatalf("POST reject status = %d, want 303", recReject.Code)
	}
	if !rejectCalled {
		t.Fatal("endpoint de rechazo no fue invocado")
	}
}
