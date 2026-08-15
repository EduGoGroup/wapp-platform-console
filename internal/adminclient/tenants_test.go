package adminclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestListTenants_BoundsSuccessBody fija el hallazgo #7 de CODE-REVIEW-2026-08-15: los cuerpos de
// ÉXITO se decodificaban sin io.LimitReader (solo el camino de error estaba acotado). Aquí el
// servidor devuelve un cuerpo bien formado pero deliberadamente mayor que maxSuccessBody: con el
// límite en vigor, el decode debe fallar por corte (EOF inesperado) en vez de leer el cuerpo entero
// sin tope.
func TestListTenants_BoundsSuccessBody(t *testing.T) {
	t.Parallel()
	padding := strings.Repeat("a", maxSuccessBody+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"limit":0,"offset":0,"padding":"` + padding + `"}`))
	}))
	defer srv.Close()

	client := NewTenantsClient(NewTransport(srv.URL, 0))
	_, err := client.ListTenants(context.Background(), "tok", 0, 0)
	if err == nil {
		t.Fatal("ListTenants con cuerpo mayor que maxSuccessBody = nil, want error de decode por corte")
	}
}

// TestGetTenant_EscapesIDInPath verifica que un ID con caracteres especiales
// se escapa correctamente en el path, evitando inyección de query string.
func TestGetTenant_EscapesIDInPath(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"","slug":"","display_name":"","plan_id":"","installations_count":0,"features":[]}`))
	}))
	defer srv.Close()

	client := NewTenantsClient(NewTransport(srv.URL, 0))
	// Usar un ID malicioso que contiene ? para intentar inyectar query string
	maliciousID := "t-1?limit=9999"
	_, err := client.GetTenant(context.Background(), "tok", maliciousID)
	if err != nil {
		t.Fatalf("GetTenant falló: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// RawQuery debe estar vacío: el ? fue escapado en el path, no es un separador de query
	if receivedQuery != "" {
		t.Errorf("RawQuery debería estar vacío (el ? no debe inyectar parámetros), got %q", receivedQuery)
	}
}

// TestIssueEnrollmentCode_EscapesTenantIDInPath verifica que tenantID se escapa correctamente,
// evitando inyección de query string.
func TestIssueEnrollmentCode_EscapesTenantIDInPath(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"ABC","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewTenantsClient(NewTransport(srv.URL, 0))
	// Usar un tenantID malicioso que contiene ? para intentar inyectar query string
	maliciousTenantID := "t-2?x=1"
	_, err := client.IssueEnrollmentCode(context.Background(), "tok", maliciousTenantID, 3600)
	if err != nil {
		t.Fatalf("IssueEnrollmentCode falló: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// RawQuery debe estar vacío: los caracteres especiales fueron escapados en el path
	if receivedQuery != "" {
		t.Errorf("RawQuery debería estar vacío (los ? y = no deben inyectar parámetros), got %q", receivedQuery)
	}
}
