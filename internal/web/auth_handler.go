package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/EduGoGroup/wapp-shared/iam"
	sharedweb "github.com/EduGoGroup/wapp-shared/web"
	webgin "github.com/EduGoGroup/wapp-shared/web/gin"
	"github.com/gin-gonic/gin"
)

// AuthHandler gestiona el login/logout y el AuthMiddleware de la consola de plataforma.
//
// El AuthMiddleware NO sube al módulo compartido: depende del upstream y del perímetro de cada
// consola, y son dos perímetros de autorización distintos que no se tocan.
type AuthHandler struct {
	cfg     *config.Config
	auth    *iam.Client
	refresh *sharedweb.RefreshGroup[*iam.AuthResult]
}

// NewAuthHandler construye el handler de autenticación.
func NewAuthHandler(cfg *config.Config, auth *iam.Client) *AuthHandler {
	return &AuthHandler{
		cfg:     cfg,
		auth:    auth,
		refresh: sharedweb.NewRefreshGroup[*iam.AuthResult](),
	}
}

// ShowLogin muestra el formulario de login.
func (h *AuthHandler) ShowLogin(c *gin.Context) {
	if h.hasValidSession(c) {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	h.renderLogin(c, http.StatusOK, "")
}

// DoLogin procesa las credenciales del operador de plataforma.
//
// El 401 de credenciales y el 403 del System Gate llegan como sentinelas distintos —`iam` no los
// colapsa— y aquí se muestran con el mismo texto a propósito: al que está en la pantalla de login no
// se le dice si el correo existe. La diferencia sí queda en el log.
func (h *AuthHandler) DoLogin(c *gin.Context) {
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	if email == "" || password == "" {
		h.renderLogin(c, http.StatusBadRequest, "Introduce tu correo y contraseña.")
		return
	}

	res, err := h.auth.Login(c.Request.Context(), email, password)
	if err != nil {
		if errors.Is(err, iam.ErrForbidden) {
			slog.Warn("login de plataforma rechazado por el System Gate", "error", err)
		}
		if errors.Is(err, iam.ErrUnauthorized) || errors.Is(err, iam.ErrForbidden) {
			h.renderLogin(c, http.StatusUnauthorized, "Credenciales inválidas o sin acceso a la consola de plataforma.")
			return
		}
		slog.Warn("login de plataforma rechazado", "error", err)
		h.renderLogin(c, http.StatusUnauthorized, "No se pudo iniciar sesión. Verifica tus credenciales.")
		return
	}

	if err := h.startSession(c, res); err != nil {
		slog.Error("falló iniciar sesión", "error", err)
		h.renderLogin(c, http.StatusInternalServerError, "Error interno al iniciar sesión.")
		return
	}

	c.Redirect(http.StatusSeeOther, "/")
}

// DoLogout finaliza la sesión.
//
// La cookie local se borra SIEMPRE, aunque falle la revocación remota: el operador no debe quedarse
// con una sesión que él cree cerrada. Pero el fallo no se traga en silencio: si identity responde
// con error, el refresh token sigue vivo allí, y eso tiene que quedar en el log para que se note.
func (h *AuthHandler) DoLogout(c *gin.Context) {
	if raw := webgin.SessionCookieValue(c, sessionCookieOptions(h.cfg)); raw != "" {
		if sess, derr := sharedweb.DecodeSession(raw); derr == nil && sess.RefreshToken != "" {
			if lerr := h.auth.Logout(c.Request.Context(), sess.RefreshToken); lerr != nil {
				slog.Warn("logout en identity falló; la sesión se cierra localmente igualmente", "error", lerr)
			}
		}
	}
	h.clearSession(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

// AuthMiddleware valida la cookie de sesión y renueva el token proactivamente.
func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := webgin.SessionCookieValue(c, sessionCookieOptions(h.cfg))
		if raw == "" {
			h.redirectToLogin(c)
			return
		}

		sess, err := sharedweb.DecodeSession(raw)
		if err != nil || sess.AccessToken == "" {
			h.clearSession(c)
			h.redirectToLogin(c)
			return
		}

		claims, err := parseAccessClaims(sess.AccessToken)
		if err != nil {
			h.clearSession(c)
			h.redirectToLogin(c)
			return
		}
		exp := accessExpiry(claims)

		accessToken := sess.AccessToken
		refreshToken := sess.RefreshToken

		if sharedweb.RefreshDue(exp, 0) && refreshToken != "" {
			res, rerr := h.refreshSession(c, refreshToken)
			if rerr == nil && res != nil {
				accessToken = res.AccessToken
				refreshToken = res.RefreshToken
			} else if !sharedweb.SessionValid(exp) {
				h.clearSession(c)
				h.redirectToLogin(c)
				return
			}
		} else if !sharedweb.SessionValid(exp) {
			h.clearSession(c)
			h.redirectToLogin(c)
			return
		}

		c.Set(webgin.ContextAccessToken, accessToken)
		c.Set(webgin.ContextRefreshToken, refreshToken)
		c.Set(webgin.ContextUserID, claims.UserID)
		c.Set(webgin.ContextTenantID, claims.TenantID)
		c.Next()
	}
}

func (h *AuthHandler) hasValidSession(c *gin.Context) bool {
	raw := webgin.SessionCookieValue(c, sessionCookieOptions(h.cfg))
	if raw == "" {
		return false
	}
	sess, err := sharedweb.DecodeSession(raw)
	if err != nil || sess.AccessToken == "" {
		return false
	}
	claims, err := parseAccessClaims(sess.AccessToken)
	if err != nil {
		return false
	}
	return sharedweb.SessionValid(accessExpiry(claims))
}

func (h *AuthHandler) startSession(c *gin.Context, res *iam.AuthResult) error {
	raw, err := sharedweb.EncodeSession(sharedweb.SessionData{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		ExpiresAt:    res.ExpiresAt,
	})
	if err != nil {
		return err
	}
	webgin.SetSessionCookie(c, sessionCookieOptions(h.cfg), raw, sessionCookieMaxAge)
	return nil
}

func (h *AuthHandler) clearSession(c *gin.Context) {
	webgin.ClearSessionCookie(c, sessionCookieOptions(h.cfg))
}

func (h *AuthHandler) redirectToLogin(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, "/login")
	c.Abort()
}

// refreshSession serializa por refresh token los refrescos concurrentes: N peticiones del mismo
// operador que llegan a la vez hacen UN solo viaje a identity, no N.
func (h *AuthHandler) refreshSession(c *gin.Context, refreshToken string) (*iam.AuthResult, error) {
	res, err := h.refresh.Do(refreshToken, func() (*iam.AuthResult, error) {
		return h.auth.Refresh(c.Request.Context(), refreshToken)
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, errors.New("refresh falló")
	}
	_ = h.startSession(c, res)
	return res, nil
}

func (h *AuthHandler) renderLogin(c *gin.Context, status int, errMsg string) {
	c.HTML(status, "base.html", gin.H{
		"Title":           "Iniciar sesión",
		"Subtitle":        "Consola de Plataforma",
		"ContentTemplate": "login.html",
		"Error":           errMsg,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
		"IsAuthenticated": false,
	})
}
