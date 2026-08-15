package authclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClient_Logout_PropagatesUpstreamError fija el hallazgo #3 de CODE-REVIEW-2026-08-15: antes
// Logout ignoraba el StatusCode de identity y siempre devolvía nil, así que un 500 (o cualquier otro
// error) quedaba invisible para el llamante: la consola decía "sesión cerrada" mientras el refresh
// token seguía vivo allí.
func TestClient_Logout_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "http://127.0.0.1:8103", time.Second)
	err := c.Logout(context.Background(), "rt-1")
	if err == nil {
		t.Fatal("Logout con identity devolviendo 500 = nil, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Logout error = %q, want que mencione el status 500", err.Error())
	}
}

// TestClient_Logout_SucceedsOn2xx cubre el camino feliz (200/204) para que el fix de propagación de
// arriba no se lea como "Logout ahora siempre falla".
func TestClient_Logout_SucceedsOn2xx(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "http://127.0.0.1:8103", time.Second)
			if err := c.Logout(context.Background(), "rt-1"); err != nil {
				t.Fatalf("Logout con identity devolviendo %d = %v, want nil", status, err)
			}
		})
	}
}

// TestNewClient_AppliesConfiguredTimeout fija el hallazgo #4: defaultTimeout estaba fijo a 15s sin
// importar cfg.UpstreamTimeout, así que WAPP_CONSOLE_UPSTREAM_TIMEOUT_SECS no tenía ningún efecto
// sobre login/refresh/logout. Aquí se construye el cliente con un timeout deliberadamente corto
// contra un servidor que tarda más: si el valor configurado no se hubiera aplicado, la petición
// usaría el default de 15s y no fallaría dentro del plazo corto que el test verifica.
func TestNewClient_AppliesConfiguredTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "http://127.0.0.1:8103", 20*time.Millisecond)
	start := time.Now()
	err := c.Logout(context.Background(), "rt-1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Logout con timeout de 20ms contra servidor de 200ms = nil, want error de timeout")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("Logout tardó %v; el timeout configurado (20ms) no se aplicó (habría cortado mucho antes)", elapsed)
	}
}

// TestNewClient_ZeroTimeoutFallsBackToDefault documenta el fallback: timeout<=0 no debe significar
// "sin límite" en el http.Client (eso dejaría al cliente colgado ante un upstream que nunca
// responde), así que cae al default histórico de 15s.
func TestNewClient_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	t.Parallel()
	c := NewClient("http://127.0.0.1:8200", "http://127.0.0.1:8103", 0)
	if c.httpClient.Timeout != defaultTimeout {
		t.Fatalf("httpClient.Timeout con timeout<=0 = %v, want default %v", c.httpClient.Timeout, defaultTimeout)
	}
}
