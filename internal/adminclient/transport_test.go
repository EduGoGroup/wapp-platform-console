package adminclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewTransport_AppliesConfiguredTimeout fija el hallazgo #4 de CODE-REVIEW-2026-08-15:
// defaultTimeout estaba fijo a 15s sin importar cfg.UpstreamTimeout, así que
// WAPP_CONSOLE_UPSTREAM_TIMEOUT_SECS no tenía ningún efecto sobre las llamadas admin (:8100). Se
// pasa por un cliente real (TenantsClient) para probar la ruta completa NewTransport → HTTPClient,
// no solo el campo aislado.
func TestNewTransport_AppliesConfiguredTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewTenantsClient(NewTransport(srv.URL, 20*time.Millisecond))

	start := time.Now()
	_, err := client.ListTenants(context.Background(), "tok", 0, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ListTenants con timeout de 20ms contra servidor de 200ms = nil, want error de timeout")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("ListTenants tardó %v; el timeout configurado (20ms) no se aplicó (habría cortado mucho antes)", elapsed)
	}
}

// TestNewTransport_ZeroTimeoutFallsBackToDefault documenta el fallback: timeout<=0 no debe
// significar "sin límite" (eso dejaría al cliente colgado ante un upstream que nunca responde), cae
// al default histórico de 15s.
func TestNewTransport_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	t.Parallel()
	tr := NewTransport("http://127.0.0.1:8100", 0)
	if tr.HTTPClient.Timeout != defaultTimeout {
		t.Fatalf("HTTPClient.Timeout con timeout<=0 = %v, want default %v", tr.HTTPClient.Timeout, defaultTimeout)
	}
}
