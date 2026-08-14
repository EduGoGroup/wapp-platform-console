package web

import (
	"context"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
)

// RequestDeadlineMiddleware fija un deadline máximo a toda la cadena de llamadas upstream.
func RequestDeadlineMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.UpstreamTimeout > 0 {
			ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.UpstreamTimeout)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
