package adminclient

import (
	"context"
	"encoding/json"
	"errors"
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

// TestApproveAccessRequest_ConflictPartialSkipped cubre el Trabajo 2 (code review 056 · T11): un 409
// con cuerpo {"local":"ok","identity":"skipped","reason":...} (platformadmin.ApprovePartialResult,
// ErrSystemsUnionUnavailable) debe decodificarse como *PartialApprovalError con Identity=="skipped", NO
// como un RejectionError genérico de mensaje vacío.
func TestApproveAccessRequest_ConflictPartialSkipped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"local":    "ok",
			"identity": "skipped",
			"reason":   "no se puede leer el conjunto actual de systems en identity",
		})
	}))
	defer srv.Close()

	client := NewAccessRequestsClient(NewTransport(srv.URL, 0))
	err := client.ApproveAccessRequest(context.Background(), "tok", "req-1", "tenant-1", "role", []string{"wapp.bff"})

	var partial *PartialApprovalError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v (%T), quería *PartialApprovalError", err, err)
	}
	if partial.Identity != "skipped" {
		t.Fatalf("Identity = %q, quería \"skipped\"", partial.Identity)
	}
	if partial.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, quería 409", partial.StatusCode)
	}
	if partial.Reason == "" {
		t.Fatal("Reason no debe quedar vacío: es lo que explica por qué no es transitorio")
	}
}

// TestApproveAccessRequest_ConflictAlreadyResolvedIsNotPartial es la trampa del Trabajo 2: el OTRO 409
// legítimo ("la solicitud ya fue resuelta", ErrConflict) es texto plano (http.Error), sin las claves
// local/identity/reason. No debe decodificarse como *PartialApprovalError.
func TestApproveAccessRequest_ConflictAlreadyResolvedIsNotPartial(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "la solicitud ya fue resuelta o la persona ya pertenece a otra empresa", http.StatusConflict)
	}))
	defer srv.Close()

	client := NewAccessRequestsClient(NewTransport(srv.URL, 0))
	err := client.ApproveAccessRequest(context.Background(), "tok", "req-1", "tenant-1", "role", []string{"wapp.bff"})

	var partial *PartialApprovalError
	if errors.As(err, &partial) {
		t.Fatalf("un 409 sin el cuerpo parcial NO debe decodificarse como *PartialApprovalError: %+v", partial)
	}
	var rej *RejectionError
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v (%T), quería *RejectionError", err, err)
	}
	if rej.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, quería 409", rej.StatusCode)
	}
}

// TestApproveAccessRequest_BadGatewayPartialFailed cubre el 502 (ErrSystemsSyncFailed) con el cuerpo
// REAL que manda platformadmin ({"local":"ok","identity":"failed","reason":...}): también debe
// decodificarse como *PartialApprovalError, esta vez con Identity=="failed".
func TestApproveAccessRequest_BadGatewayPartialFailed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"local":    "ok",
			"identity": "failed",
			"reason":   "fallo al sincronizar systems en identity tras aprobar localmente",
		})
	}))
	defer srv.Close()

	client := NewAccessRequestsClient(NewTransport(srv.URL, 0))
	err := client.ApproveAccessRequest(context.Background(), "tok", "req-1", "tenant-1", "role", []string{"wapp.bff"})

	var partial *PartialApprovalError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v (%T), quería *PartialApprovalError", err, err)
	}
	if partial.Identity != "failed" {
		t.Fatalf("Identity = %q, quería \"failed\"", partial.Identity)
	}
	if partial.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, quería 502", partial.StatusCode)
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
