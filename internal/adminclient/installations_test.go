package adminclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestListInstallations_EscapesTenantIDInPath verifica que tenantID se escapa correctamente
// en el path, evitando inyección de query string.
func TestListInstallations_EscapesTenantIDInPath(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	client := NewInstallationsClient(NewTransport(srv.URL, 0))
	// Usar un tenantID malicioso que contiene ? para intentar inyectar query string
	maliciousTenantID := "t-3?limit=9999"
	_, err := client.ListInstallations(context.Background(), "tok", maliciousTenantID)
	if err != nil {
		t.Fatalf("ListInstallations falló: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// RawQuery debe estar vacío: el ? fue escapado en el path, no es un separador de query
	if receivedQuery != "" {
		t.Errorf("RawQuery debería estar vacío (el ? no debe inyectar parámetros), got %q", receivedQuery)
	}
}
