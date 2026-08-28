package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// TestKeyedRateLimiter_CloseTwiceDoesNotPanic fija el defecto (a): Close() hacía close(l.stop) a
// pelo, así que la segunda llamada entraba en pánico ("close of closed channel"). Close() es la
// función de limpieza que NewRouterWithLimiter devuelve al llamador: nada impide que un caller la
// invoque en un defer y también en un camino de error, y ese doble cierre tumbaba el proceso.
func TestKeyedRateLimiter_CloseTwiceDoesNotPanic(t *testing.T) {
	t.Parallel()

	l := newKeyedRateLimiter(5, 10, 0, 0)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("el segundo Close() entró en pánico: %v", r)
		}
	}()

	l.allow("10.0.0.1")
	l.Close()
	if got := l.lenEntries(); got != 0 {
		t.Fatalf("entradas tras Close() = %d, want 0 (Close libera el mapa)", got)
	}

	// Tras Close() el limitador SIGUE atendiendo: hay callers (NewRouter y sus tests) que cierran y
	// después sirven peticiones con el mismo router.
	l.allow("10.0.0.2")

	l.Close() // segunda llamada: ni pánico ni efecto (sync.Once)
	if got := l.lenEntries(); got != 1 {
		t.Fatalf("entradas tras el 2º Close() = %d, want 1: Close no es idempotente, "+
			"volvió a vaciar el mapa", got)
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

// TestKeyedRateLimiter_PurgaPerezosaDesalojaInactivas prueba el mecanismo en sí, con reloj inyectado
// y sin esperas: allow() barre las claves inactivas de forma amortizada (como mucho una vez por
// purgeEvery), de modo que el mapa no depende de ninguna goroutine para dejar de crecer.
func TestKeyedRateLimiter_PurgaPerezosaDesalojaInactivas(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	fake := base

	l := newKeyedRateLimiter(100, 100, 5*time.Minute, time.Minute)
	defer l.Close()
	l.now = func() time.Time { return fake }
	// El constructor selló lastPurge con el reloj REAL; se realinea con el falso para que la ventana
	// amortizada se mida sobre el mismo reloj (es justo lo que habría hecho el constructor con él).
	l.lastPurge = base

	for _, k := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		l.allow(k)
	}
	if got := l.lenEntries(); got != 3 {
		t.Fatalf("entradas tras 3 claves distintas = %d, want 3", got)
	}

	// El reloj avanza más que el TTL: las 3 quedan inactivas. La siguiente petición (de una 4ª clave)
	// debe barrerlas antes de atenderse.
	fake = base.Add(10 * time.Minute)
	l.allow("10.0.0.4")

	if got := l.lenEntries(); got != 1 {
		t.Fatalf("entradas tras superar el TTL = %d, want 1 (solo la clave recién vista): "+
			"la purga perezosa no desalojó las inactivas", got)
	}
}
