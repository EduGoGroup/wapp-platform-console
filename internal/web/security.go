package web

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware emite las cabeceras HTTP de seguridad con CSP de nonce dinámico.
func SecurityHeadersMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		nonceBytes := make([]byte, 16)
		if _, err := rand.Read(nonceBytes); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
		c.Set("csp_nonce", nonce)

		csp := fmt.Sprintf("default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'nonce-%s'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none';",
			nonce, nonce)

		c.Header("Content-Security-Policy", csp)
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		if cfg.HSTSEnabled {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

// CORSMiddleware implementa política CORS segura basada en lista blanca estricta.
func CORSMiddleware(cfg *config.Config) gin.HandlerFunc {
	allowed := make(map[string]bool)
	for _, o := range strings.Split(cfg.AllowedOrigins, ",") {
		o = strings.TrimSpace(o)
		if o != "" && o != "*" {
			allowed[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if c.Request.Method == http.MethodOptions {
			if origin != "" && !allowed[origin] {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
