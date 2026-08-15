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
//
// La cookie es HttpOnly SIEMPRE (el JS nunca la lee; el token lo incrusta el servidor al renderizar,
// {{ .CSRFToken }}, y no hay un solo <script> en este repo que necesite leerla) y SameSite=Lax SIEMPRE,
// con independencia de `cfg.CookieSameSite` (esa config es para la cookie de SESIÓN, no para esta). El
// fail-safe CSRF no se degrada a None aunque la sesión se configure así: detrás de esta consola está el
// kill-switch de toda la plataforma.
func CSRFMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(csrfCookieName)
		if err != nil || token == "" {
			buf := make([]byte, 32)
			if _, rerr := rand.Read(buf); rerr != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			token = base64.RawURLEncoding.EncodeToString(buf)
			c.SetSameSite(http.SameSiteLaxMode)
			c.SetCookie(csrfCookieName, token, 86400, "/", "", cfg.CookieSecure, true)
		}

		c.Set(csrfFieldName, token)

		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut ||
			c.Request.Method == http.MethodPatch || c.Request.Method == http.MethodDelete {
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
