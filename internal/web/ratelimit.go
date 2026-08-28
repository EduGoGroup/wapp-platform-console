package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	defaultLimiterTTL        = 5 * time.Minute
	defaultLimiterPurgeEvery = time.Minute
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// keyedRateLimiter es un token-bucket en memoria por clave (IP del cliente o, si hay sesión, user_id).
//
// La purga de claves inactivas es PEREZOSA: la hace allow() de forma amortizada (como mucho una vez
// cada purgeEvery), no una goroutine de fondo. Es deliberado: el limitador lo construye NewRouter,
// que no expone ningún ciclo de vida al llamador, así que una goroutine de barrido solo tenía dos
// finales posibles —quedarse viva para siempre, o pararse y dejar el mapa `entries` creciendo sin
// tope con una entrada por IP—. Sin goroutine no hay ninguna de las dos.
//
// Contrapartida asumida: si el tráfico cesa por completo, el mapa se queda con las entradas que
// hubiera (no crece más, pero tampoco se vacía) hasta la siguiente petición, que las barre.
type keyedRateLimiter struct {
	mu         sync.Mutex
	entries    map[string]*limiterEntry
	rps        rate.Limit
	burst      int
	ttl        time.Duration // inactividad tras la cual se desaloja una clave
	purgeEvery time.Duration // cada cuánto, como mucho, allow() intenta el barrido
	lastPurge  time.Time
	now        func() time.Time // inyectable: los tests lo sustituyen por un reloj falso
	stopOnce   sync.Once
}

// newKeyedRateLimiter crea el limitador. ttl y purgeEvery <= 0 caen a los valores por defecto.
func newKeyedRateLimiter(rps float64, burst int, ttl, purgeEvery time.Duration) *keyedRateLimiter {
	if ttl <= 0 {
		ttl = defaultLimiterTTL
	}
	if purgeEvery <= 0 {
		purgeEvery = defaultLimiterPurgeEvery
	}
	now := time.Now
	return &keyedRateLimiter{
		entries:    make(map[string]*limiterEntry),
		rps:        rate.Limit(rps),
		burst:      burst,
		ttl:        ttl,
		purgeEvery: purgeEvery,
		lastPurge:  now(),
		now:        now,
	}
}

func (l *keyedRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.purgeLocked(now)

	entry, exists := l.entries[key]
	if !exists {
		entry = &limiterEntry{
			limiter: rate.NewLimiter(l.rps, l.burst),
		}
		l.entries[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}

// purgeLocked desaloja las claves inactivas más viejas que ttl. Amortizado: no hace nada si no ha
// pasado purgeEvery desde el último barrido, de modo que el coste por petición es una comparación de
// tiempos. Exige l.mu tomado.
func (l *keyedRateLimiter) purgeLocked(now time.Time) {
	if now.Sub(l.lastPurge) < l.purgeEvery {
		return
	}
	l.lastPurge = now
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > l.ttl {
			delete(l.entries, k)
		}
	}
}

// lenEntries devuelve el número de claves vivas en el limitador (solo lo usan los tests de purga).
func (l *keyedRateLimiter) lenEntries() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Close libera de golpe las entradas del limitador; es la función de limpieza que
// NewRouterWithLimiter entrega al dueño del ciclo de vida (bootstrap la llama en un defer al apagar).
//
// Es IDEMPOTENTE (sync.Once): antes hacía close(l.stop) a pelo y la segunda llamada entraba en pánico
// con "close of closed channel". Y NO inhabilita el limitador: allow() sigue funcionando y purgando
// después de Close(), porque hay callers que cierran y siguen sirviendo peticiones con el router.
func (l *keyedRateLimiter) Close() {
	l.stopOnce.Do(func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.entries = make(map[string]*limiterEntry)
		l.lastPurge = l.now()
	})
}

// RateLimitMiddleware limita las peticiones por IP o usuario.
func RateLimitMiddleware(cfg *config.Config) (gin.HandlerFunc, *keyedRateLimiter) {
	limiter := newKeyedRateLimiter(cfg.RateLimitRPS, int(cfg.RateLimitBurst), cfg.RateLimitTTL, cfg.RateLimitPurgeEvery)
	return func(c *gin.Context) {
		key := c.ClientIP()
		if uid, exists := c.Get(ctxUserID); exists {
			if s, ok := uid.(string); ok && s != "" {
				key = s
			}
		}
		if !limiter.allow(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Demasiadas peticiones. Por favor, espera unos segundos.",
			})
			return
		}
		c.Next()
	}, limiter
}
