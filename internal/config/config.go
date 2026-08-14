// Package config centraliza la configuración de la consola de plataforma (wapp-platform-console).
//
// La consola es una superficie web server-side endurecida para operadores de plataforma.
// Habla con el listener admin de la plataforma (:8100) para operaciones administrativas y
// con la API pública (:8103) exclusivamente para el canje de tokens en el login.
package config

import (
	"time"

	sharedconfig "github.com/EduGoGroup/wapp-shared/config"
)

// Config agrupa la configuración efectiva de la consola de plataforma.
type Config struct {
	Environment      string
	HTTPAddr         string
	AdminAPIBaseURL  string
	PublicAPIBaseURL string
	IdentityBaseURL  string

	CookieSecure   bool
	CookieSameSite string
	AllowedOrigins string
	TrustedProxies string
	HSTSEnabled    bool

	RateLimitEnabled bool
	RateLimitRPS     float64
	RateLimitBurst   float64

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	UpstreamTimeout   time.Duration
}

// Load resuelve la configuración desde variables de entorno con prefijo WAPP_.
func Load() Config {
	l := sharedconfig.New(sharedconfig.WithEnvPrefix("WAPP_"))

	env := l.GetString("CONSOLE_ENV", l.GetString("PLATFORM_CONSOLE_ENV", "local"))
	secureDefault := env != "local"

	return Config{
		Environment:      env,
		HTTPAddr:         l.GetString("PLATFORM_CONSOLE_HTTP_ADDR", l.GetString("CONSOLE_HTTP_ADDR", "127.0.0.1:8106")),
		AdminAPIBaseURL:  l.GetString("ADMIN_API_BASE", "http://127.0.0.1:8100"),
		PublicAPIBaseURL: l.GetString("PUBLIC_API_BASE", "http://127.0.0.1:8103"),
		IdentityBaseURL:  l.GetString("IDENTITY_URL", "http://127.0.0.1:8200"),

		CookieSecure:   l.GetBool("CONSOLE_COOKIE_SECURE", secureDefault),
		CookieSameSite: l.GetString("CONSOLE_COOKIE_SAMESITE", "lax"),
		AllowedOrigins: l.GetString("CONSOLE_ALLOWED_ORIGINS", ""),
		TrustedProxies: l.GetString("PLATFORM_CONSOLE_TRUSTED_PROXIES", l.GetString("CONSOLE_TRUSTED_PROXIES", "")),
		HSTSEnabled:    l.GetBool("CONSOLE_HSTS_ENABLED", secureDefault),

		RateLimitEnabled: l.GetBool("CONSOLE_RATE_ENABLED", true),
		RateLimitRPS:     float64(l.GetInt("CONSOLE_RATE_RPS", 5)),
		RateLimitBurst:   float64(l.GetInt("CONSOLE_RATE_BURST", 10)),

		ReadHeaderTimeout: time.Duration(l.GetInt("CONSOLE_READ_HEADER_TIMEOUT_SECS", 5)) * time.Second,
		ReadTimeout:       time.Duration(l.GetInt("CONSOLE_READ_TIMEOUT_SECS", 15)) * time.Second,
		WriteTimeout:      time.Duration(l.GetInt("CONSOLE_WRITE_TIMEOUT_SECS", 30)) * time.Second,
		IdleTimeout:       time.Duration(l.GetInt("CONSOLE_IDLE_TIMEOUT_SECS", 60)) * time.Second,

		ShutdownTimeout: time.Duration(l.GetInt("CONSOLE_SHUTDOWN_TIMEOUT_SECS", 10)) * time.Second,
		UpstreamTimeout: time.Duration(l.GetInt("CONSOLE_UPSTREAM_TIMEOUT_SECS", 20)) * time.Second,
	}
}
