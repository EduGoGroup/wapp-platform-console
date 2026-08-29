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
	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/EduGoGroup/wapp-shared/iam"
	"github.com/EduGoGroup/wapp-shared/ui"
	sharedweb "github.com/EduGoGroup/wapp-shared/web"
	webgin "github.com/EduGoGroup/wapp-shared/web/gin"
	"github.com/gin-gonic/gin"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static/css/app.css
var appCSS []byte

// systemWappPlatform es la clave de ESTA aplicación en el catálogo de identity (`iam.systems`). El
// BFF del cliente se presenta con otra (`wapp.bff`): son dos perímetros de autorización distintos y
// compartir el cliente de identidad no los acerca ni un milímetro.
const systemWappPlatform = "wapp.platform"

// NewRouter construye el motor Gin y descarta el cleanup del rate limiter, que aquí no hace falta:
// el limitador de `wapp-shared/web` no arranca ninguna goroutine y purga sus claves inactivas de
// forma perezosa dentro de Allow(), así que no hay barrido que filtrar ni mapa que crezca sin tope.
//
// Usa NewRouterWithLimiter solo si eres el dueño del ciclo de vida y quieres liberar las entradas al
// apagar (lo hace bootstrap).
func NewRouter(cfg *config.Config) *gin.Engine {
	router, _ := NewRouterWithLimiter(cfg)
	return router
}

// NewRouterWithLimiter construye el motor Gin y una función de limpieza para el rate limiter.
func NewRouterWithLimiter(cfg *config.Config) (*gin.Engine, func()) {
	webgin.SetReleaseMode()
	router := gin.New()

	if err := webgin.SetTrustedProxies(router, cfg.TrustedProxies); err != nil {
		slog.Error("lista de proxies de confianza inválida", "valor", cfg.TrustedProxies, "error", err)
		panic(err)
	}

	router.Use(gin.Recovery())
	router.Use(webgin.SlogLogger())
	router.Use(webgin.SecurityHeaders(sharedweb.SecurityOptions{HSTS: cfg.HSTSEnabled}))
	router.Use(webgin.CORS(sharedweb.CORSOptions{
		AllowedOrigins: sharedweb.ParseOrigins(cfg.AllowedOrigins),
	}))

	var rateLimiter *sharedweb.KeyedRateLimiter
	if cfg.RateLimitEnabled {
		rateLimiter = sharedweb.NewKeyedRateLimiter(sharedweb.RateLimiterOptions{
			RPS:        cfg.RateLimitRPS,
			Burst:      int(cfg.RateLimitBurst),
			TTL:        cfg.RateLimitTTL,
			PurgeEvery: cfg.RateLimitPurgeEvery,
		})
		router.Use(webgin.RateLimit(rateLimiter))
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
		"templates/partials/*.html",
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

	router.Use(webgin.CSRF(csrfOptions(cfg)))

	// Clientes
	adminTransport := adminclient.NewTransport(cfg.AdminAPIBaseURL, cfg.UpstreamTimeout)
	tenantsClient := adminclient.NewTenantsClient(adminTransport)
	installationsClient := adminclient.NewInstallationsClient(adminTransport)
	accessRequestsClient := adminclient.NewAccessRequestsClient(adminTransport)

	// El `system` con el que esta consola se presenta ante identity es CAMPO del cliente, no una
	// constante del módulo: el System Gate autoriza aplicaciones (`wapp.platform`), no ecosistemas.
	// Unas opciones que no pueden funcionar fallan aquí, en el arranque, y no dentro de un login.
	authClient, err := iam.NewClient(iam.Options{
		System:          systemWappPlatform,
		IdentityBaseURL: cfg.IdentityBaseURL,
		PlatformBaseURL: cfg.PublicAPIBaseURL,
		Timeout:         cfg.UpstreamTimeout,
	})
	if err != nil {
		slog.Error("configuración del cliente de identidad inválida", "error", err)
		panic(err)
	}

	authH := NewAuthHandler(cfg, authClient)
	tenantsH := NewTenantsHandler(tenantsClient, installationsClient)
	provH := NewProvisioningHandler(tenantsClient, cfg)
	accessH := NewAccessRequestsHandler(accessRequestsClient, tenantsClient)

	// Rutas públicas
	router.GET("/login", authH.ShowLogin)
	router.POST("/login", authH.DoLogin)
	router.POST("/logout", authH.DoLogout)

	// Rutas protegidas (AuthMiddleware de plataforma)
	protected := router.Group("/")
	protected.Use(authH.AuthMiddleware())
	protected.Use(webgin.RequestDeadline(cfg.UpstreamTimeout))

	protected.GET("/", tenantsH.ShowTenants)
	protected.GET("/tenants/new", provH.ShowNewTenant)
	protected.POST("/tenants/new", provH.DoCreateTenant)
	protected.GET("/tenants/:id", tenantsH.ShowTenantDetail)
	protected.POST("/tenants/:id/revoke", tenantsH.DoRevokeTenant)
	protected.POST("/tenants/:id/restore", tenantsH.DoRestoreTenant)
	protected.POST("/tenants/:id/enrollment-codes", provH.DoIssueEnrollmentCode)
	// La pantalla del código es un GET (POST-Redirect-GET, M-10): recargarla no reenvía el POST y por
	// tanto no emite un código nuevo. El código llega en una cookie efímera, no en la URL.
	protected.GET("/tenants/:id/enrollment-code", provH.ShowEnrollmentCode)

	protected.GET("/access-requests", accessH.ShowAccessRequests)
	protected.POST("/access-requests/:id/approve", accessH.DoApproveAccessRequest)
	protected.POST("/access-requests/:id/reject", accessH.DoRejectAccessRequest)

	var cleanup func()
	if rateLimiter != nil {
		cleanup = rateLimiter.Close
	}

	return router, cleanup
}
