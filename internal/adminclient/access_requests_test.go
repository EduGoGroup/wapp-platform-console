package adminclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestApproveAccessRequest_EscapesIDInPath verifica que un ID con caracteres especiales
// se escapa correctamente en el path, evitando inyección de query string.
// El test pasa un ID malicioso con ? para verificar que no se interpreta como query string.
func TestApproveAccessRequest_EscapesIDInPath(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQuery = r.URL.RawQuery
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client := NewAccessRequestsClient(NewTransport(srv.URL, 0))
	// Usar un ID malicioso que contiene ? para intentar inyectar query string
	maliciousID := "req-1?limit=9999"
	err := client.ApproveAccessRequest(context.Background(), "tok", maliciousID, "tenant-1", "role", []string{})
	if err != nil {
		t.Fatalf("ApproveAccessRequest falló: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// RawQuery debe estar vacío: el ? fue escapado en el path, no es un separador de query
	if receivedQuery != "" {
		t.Errorf("RawQuery debería estar vacío (el ? no debe inyectar parámetros), got %q", receivedQuery)
	}
}

// TestRejectAccessRequest_EscapesIDInPath verifica que un ID con caracteres especiales
// se escapa correctamente en el path, evitando inyección de query string.
func TestRejectAccessRequest_EscapesIDInPath(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var receivedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQuery = r.URL.RawQuery
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client := NewAccessRequestsClient(NewTransport(srv.URL, 0))
	// Usar un ID malicioso que contiene ? para intentar inyectar query string
	maliciousID := "req-2?x=1"
	err := client.RejectAccessRequest(context.Background(), "tok", maliciousID, "reason")
	if err != nil {
		t.Fatalf("RejectAccessRequest falló: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// RawQuery debe estar vacío: los caracteres especiales fueron escapados en el path
	if receivedQuery != "" {
		t.Errorf("RawQuery debería estar vacío (los ? y = no deben inyectar parámetros), got %q", receivedQuery)
	}
}
