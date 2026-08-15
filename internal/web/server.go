package web

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
	"github.com/EduGoGroup/wapp-platform-console/internal/authclient"
	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/EduGoGroup/wapp-shared/ui"
	"github.com/gin-gonic/gin"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static/css/app.css
var appCSS []byte

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// NewRouter construye el motor Gin sin rate limiter desmontable: si cfg.RateLimitEnabled crea uno,
// se cierra de inmediato en vez de descartarse. Esta función no tiene forma de exponer el cleanup más
// tarde (usa NewRouterWithLimiter si necesitas apagarlo de forma ordenada al final de la vida del
// router); descartarlo sin más dejaba viva para siempre la goroutine de cleanupLoop de
// keyedRateLimiter en cualquier caller que activara el limiter por esta vía.
func NewRouter(cfg *config.Config) *gin.Engine {
	router, cleanup := NewRouterWithLimiter(cfg)
	if cleanup != nil {
		cleanup()
	}
	return router
}

// NewRouterWithLimiter construye el motor Gin y una función de limpieza para el rate limiter.
func NewRouterWithLimiter(cfg *config.Config) (*gin.Engine, func()) {
	router := gin.New()

	if err := router.SetTrustedProxies(parseTrustedProxies(cfg.TrustedProxies)); err != nil {
		slog.Error("lista de proxies de confianza inválida", "valor", cfg.TrustedProxies, "error", err)
		panic(err)
	}

	router.Use(gin.Recovery())
	router.Use(slogMiddleware())
	router.Use(SecurityHeadersMiddleware(cfg))
	router.Use(CORSMiddleware(cfg))

	var rateLimiter *keyedRateLimiter
	if cfg.RateLimitEnabled {
		var rlMiddleware gin.HandlerFunc
		rlMiddleware, rateLimiter = RateLimitMiddleware(cfg)
		router.Use(rlMiddleware)
	}

	var tmpl *template.Template
	root := template.New("").Funcs(template.FuncMap{
		"hasPrefix": strings.HasPrefix,
		"has": func(list []string, item string) bool {
			for _, v := range list {
				if v == item {
					return true
				}
			}
			return false
		},
		"yield": func(name string, data any) (template.HTML, error) {
			if name == "" {
				return "", nil
			}
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
				slog.Error("error al renderizar plantilla yield", "nombre", name, "error", err)
				return "", err
			}
			return template.HTML(buf.String()), nil // #nosec G203
		},
	})
	tmpl, err := root.ParseFS(templatesFS,
		"templates/layouts/*.html",
		"templates/pages/*.html",
	)
	if err != nil {
		slog.Error("no se pudieron compilar las plantillas HTML", "error", err)
		panic(err)
	}
	router.SetHTMLTemplate(tmpl)

	// Estilos CSS estáticos
	router.GET("/static/css/app.css", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		c.Data(http.StatusOK, "text/css; charset=utf-8", appCSS)
	})
	router.GET("/static/css/theme-platform.css", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		data, err := ui.GetCSS("theme-platform.css")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	})
	router.GET("/static/css/wapp-tokens.css", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		data, err := ui.GetCSS("wapp-tokens.css")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	})
	router.GET("/static/css/wapp-components.css", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		data, err := ui.GetCSS("wapp-components.css")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	})

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "time": time.Now().UTC().Format(time.RFC3339)})
	})

	router.Use(CSRFMiddleware(cfg))

	// Clientes
	adminTransport := adminclient.NewTransport(cfg.AdminAPIBaseURL, cfg.UpstreamTimeout)
	tenantsClient := adminclient.NewTenantsClient(adminTransport)
	installationsClient := adminclient.NewInstallationsClient(adminTransport)
	accessRequestsClient := adminclient.NewAccessRequestsClient(adminTransport)
	authClient := authclient.NewClient(cfg.IdentityBaseURL, cfg.PublicAPIBaseURL, cfg.UpstreamTimeout)

	authH := NewAuthHandler(cfg, authClient)
	tenantsH := NewTenantsHandler(tenantsClient, installationsClient)
	provH := NewProvisioningHandler(tenantsClient)
	accessH := NewAccessRequestsHandler(accessRequestsClient, tenantsClient)

	// Rutas públicas
	router.GET("/login", authH.ShowLogin)
	router.POST("/login", authH.DoLogin)
	router.POST("/logout", authH.DoLogout)

	// Rutas protegidas (AuthMiddleware de plataforma)
	protected := router.Group("/")
	protected.Use(authH.AuthMiddleware())
	protected.Use(RequestDeadlineMiddleware(cfg))

	protected.GET("/", tenantsH.ShowTenants)
	protected.GET("/tenants/new", provH.ShowNewTenant)
	protected.POST("/tenants/new", provH.DoCreateTenant)
	protected.GET("/tenants/:id", tenantsH.ShowTenantDetail)
	protected.POST("/tenants/:id/revoke", tenantsH.DoRevokeTenant)
	protected.POST("/tenants/:id/restore", tenantsH.DoRestoreTenant)
	protected.POST("/tenants/:id/enrollment-codes", provH.DoIssueEnrollmentCode)

	protected.GET("/access-requests", accessH.ShowAccessRequests)
	protected.POST("/access-requests/:id/approve", accessH.DoApproveAccessRequest)
	protected.POST("/access-requests/:id/reject", accessH.DoRejectAccessRequest)

	var cleanup func()
	if rateLimiter != nil {
		cleanup = rateLimiter.Close
	}

	return router, cleanup
}

// slogMiddleware envía cada petición HTTP a slog (diagnóstico). Copiado tal cual de
// wapp-guardian-bff/internal/web/server.go:271 (M-12): la consola ejecuta cortes y restauraciones
// cross-tenant, así que necesita la misma traza mínima (quién, desde dónde, qué status) que el BFF.
func slogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()
		if status >= 400 {
			slog.Warn("petición web con error",
				"status", status, "method", c.Request.Method, "path", path,
				"latency", latency, "ip", c.ClientIP())
		} else {
			slog.Info("petición web completada",
				"status", status, "method", c.Request.Method, "path", path, "latency", latency)
		}
	}
}

func parseTrustedProxies(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
