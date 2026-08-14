package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
)

// CSRFMiddleware implementa protección CSRF mediante cookie double-submit.
func CSRFMiddleware(cfg *config.Config) gin.HandlerFunc {
	sameSite := http.SameSiteLaxMode
	switch cfg.CookieSameSite {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	}

	return func(c *gin.Context) {
		token, err := c.Cookie(csrfCookieName)
		if err != nil || token == "" {
			buf := make([]byte, 32)
			if _, rerr := rand.Read(buf); rerr != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			token = base64.RawURLEncoding.EncodeToString(buf)
			c.SetSameSite(sameSite)
			c.SetCookie(csrfCookieName, token, 86400, "/", "", cfg.CookieSecure, false)
		}

		c.Set(csrfFieldName, token)

		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
			sentToken := c.PostForm(csrfFieldName)
			if sentToken == "" {
				sentToken = c.GetHeader("X-CSRF-Token")
			}
			if sentToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(sentToken)) != 1 {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "Petición no válida (token de seguridad ausente o incorrecto). Recarga la página e inténtalo de nuevo.",
				})
				return
			}
		}

		c.Next()
	}
}
