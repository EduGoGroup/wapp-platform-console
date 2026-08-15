package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRateLimit_BlocksAfterBurst cubre A-07d (d): `testConfig` fija RateLimitEnabled:false, así que
// ratelimit.go entero quedaba sin ejercitar por ningún test. Aquí se habilita con un burst mínimo para
// que la respuesta 429 sea observable de forma determinista tras agotarlo.
func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	t.Parallel()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	cfg.RateLimitEnabled = true
	cfg.RateLimitRPS = 0.001 // prácticamente sin recarga durante el test
	cfg.RateLimitBurst = 2

	router, cleanup := NewRouterWithLimiter(cfg)
	defer cleanup()

	// httptest.NewRequest deja RemoteAddr fijo ("192.0.2.1:1234"), así que las tres peticiones
	// comparten la misma clave de limitador (IP; el request llega sin sesión, luego sin ctxUserID).
	newReq := func() *http.Request { return httptest.NewRequest(http.MethodGet, "/login", nil) }

	codes := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, newReq())
		codes = append(codes, rec.Code)
	}

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("las 2 primeras peticiones (dentro del burst) = %v, want [200 200 ...]", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("la 3ª petición (fuera del burst) = %d, want 429. Todas: %v", codes[2], codes)
	}
}
