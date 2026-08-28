package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRateLimit_BlocksAfterBurst cubre A-07d (d): `testConfig` fija RateLimitEnabled:false, así que el
// limitador entero quedaba sin ejercitar por ningún test. Aquí se habilita con un burst mínimo para
// que la respuesta 429 sea observable de forma determinista tras agotarlo.
//
// El ALGORITMO del limitador (purga amortizada, Close idempotente, ráfaga por clave) lo prueba
// `wapp-shared/web`. Lo que se prueba aquí es el CABLEADO de esta consola: que su router lo monta,
// que la config lo enciende y que el 429 sale por el camino real.
func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	t.Parallel()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	cfg.RateLimitEnabled = true
	cfg.RateLimitRPS = 0.001 // prácticamente sin recarga durante el test
	cfg.RateLimitBurst = 2

	router, cleanup := NewRouterWithLimiter(cfg)
	defer cleanup()

	// httptest.NewRequest deja RemoteAddr fijo ("192.0.2.1:1234"), así que las tres peticiones
	// comparten la misma clave de limitador (IP; el request llega sin sesión, luego sin user_id).
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

// TestNewRouter_RateLimiterPurgaSusEntradas fija el defecto (b) por el camino de NewRouter, que es el
// que usan casi todos los callers: el limitador que monta NewRouter TIENE que purgar sus entradas.
// Antes, NewRouter llamaba al cleanup nada más construir el router: eso mataba el barrido y dejaba el
// limitador operando con el mapa `entries` creciendo sin tope (una entrada por IP de cliente, para
// siempre).
//
// La purga se observa por comportamiento, sin tocar el mapa: con rps ≈ 0 el bucket no recarga NUNCA,
// así que la única forma de que una clave 429-eada vuelva a recibir 200 es que su entrada se haya
// desalojado y se haya creado un bucket nuevo con el burst entero.
func TestNewRouter_RateLimiterPurgaSusEntradas(t *testing.T) {
	t.Parallel()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	cfg.RateLimitEnabled = true
	cfg.RateLimitRPS = 0.001 // ~1 token cada 1000 s: no hay recarga observable durante el test
	cfg.RateLimitBurst = 1
	cfg.RateLimitTTL = 30 * time.Millisecond
	cfg.RateLimitPurgeEvery = time.Millisecond

	router := NewRouter(cfg)

	do := func() int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
		return rec.Code
	}

	if got := do(); got != http.StatusOK {
		t.Fatalf("1ª petición (dentro del burst) = %d, want 200", got)
	}
	if got := do(); got != http.StatusTooManyRequests {
		t.Fatalf("2ª petición (burst agotado) = %d, want 429", got)
	}

	// Espera > TTL: la entrada de esta IP queda inactiva y debe desalojarse.
	time.Sleep(cfg.RateLimitTTL + 10*time.Millisecond)

	if got := do(); got != http.StatusOK {
		t.Fatalf("3ª petición tras superar el TTL = %d, want 200: la entrada NO se purgó "+
			"(el limitador de NewRouter sigue operando con el mapa sin barrer)", got)
	}
}
