package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type keyedRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	rps     rate.Limit
	burst   int
	stop    chan struct{}
}

func newKeyedRateLimiter(rps float64, burst int) *keyedRateLimiter {
	l := &keyedRateLimiter{
		entries: make(map[string]*limiterEntry),
		rps:     rate.Limit(rps),
		burst:   burst,
		stop:    make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *keyedRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[key]
	if !exists {
		entry = &limiterEntry{
			limiter: rate.NewLimiter(l.rps, l.burst),
		}
		l.entries[key] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

func (l *keyedRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for k, e := range l.entries {
				if now.Sub(e.lastSeen) > 5*time.Minute {
					delete(l.entries, k)
				}
			}
			l.mu.Unlock()
		case <-l.stop:
			return
		}
	}
}

func (l *keyedRateLimiter) Close() {
	close(l.stop)
}

// RateLimitMiddleware limita las peticiones por IP o usuario.
func RateLimitMiddleware(cfg *config.Config) (gin.HandlerFunc, *keyedRateLimiter) {
	limiter := newKeyedRateLimiter(cfg.RateLimitRPS, int(cfg.RateLimitBurst))
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
