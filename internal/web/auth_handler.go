package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/authclient"
	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	"github.com/gin-gonic/gin"
)

// AuthHandler gestiona el login/logout y el AuthMiddleware de la consola de plataforma.
type AuthHandler struct {
	cfg     *config.Config
	auth    *authclient.Client
	refresh *refreshGroup
}

// NewAuthHandler construye el handler de autenticación.
func NewAuthHandler(cfg *config.Config, auth *authclient.Client) *AuthHandler {
	return &AuthHandler{
		cfg:     cfg,
		auth:    auth,
		refresh: newRefreshGroup(),
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
func (h *AuthHandler) DoLogin(c *gin.Context) {
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	if email == "" || password == "" {
		h.renderLogin(c, http.StatusBadRequest, "Introduce tu correo y contraseña.")
		return
	}

	res, err := h.auth.Login(c.Request.Context(), email, password)
	if err != nil {
		if errors.Is(err, authclient.ErrUnauthorized) {
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
// con una sesión que él cree cerrada. Pero el fallo no se traga en silencio (antes Logout devolvía
// nil pasara lo que pasara): si identity responde con error, el refresh token sigue vivo allí, y eso
// tiene que quedar en el log para que alguien lo note.
func (h *AuthHandler) DoLogout(c *gin.Context) {
	if raw, err := c.Cookie(sessionCookieName); err == nil && raw != "" {
		if sess, derr := decodeSession(raw); derr == nil && sess.RefreshToken != "" {
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
		raw, err := c.Cookie(sessionCookieName)
		if err != nil || raw == "" {
			h.redirectToLogin(c)
			return
		}

		sess, err := decodeSession(raw)
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

		accessToken := sess.AccessToken
		refreshToken := sess.RefreshToken

		if refreshDue(claims) && refreshToken != "" {
			res, rerr := h.refreshSession(c, refreshToken)
			if rerr == nil && res != nil {
				accessToken = res.AccessToken
				refreshToken = res.RefreshToken
			} else if !sessionValid(claims) {
				h.clearSession(c)
				h.redirectToLogin(c)
				return
			}
		} else if !sessionValid(claims) {
			h.clearSession(c)
			h.redirectToLogin(c)
			return
		}

		c.Set(ctxAccessToken, accessToken)
		c.Set(ctxRefreshToken, refreshToken)
		c.Set(ctxUserID, claims.UserID)
		c.Set(ctxTenantID, claims.TenantID)
		c.Next()
	}
}

func (h *AuthHandler) hasValidSession(c *gin.Context) bool {
	raw, err := c.Cookie(sessionCookieName)
	if err != nil || raw == "" {
		return false
	}
	sess, err := decodeSession(raw)
	if err != nil || sess.AccessToken == "" {
		return false
	}
	claims, err := parseAccessClaims(sess.AccessToken)
	if err != nil {
		return false
	}
	return sessionValid(claims)
}

func (h *AuthHandler) startSession(c *gin.Context, res *authclient.AuthResult) error {
	raw, err := encodeSession(sessionData{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		ExpiresAt:    res.ExpiresAt,
	})
	if err != nil {
		return err
	}
	sameSite := http.SameSiteLaxMode
	if h.cfg.CookieSameSite == "strict" {
		sameSite = http.SameSiteStrictMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie(sessionCookieName, raw, 86400, "/", "", h.cfg.CookieSecure, true)
	return nil
}

func (h *AuthHandler) clearSession(c *gin.Context) {
	c.SetCookie(sessionCookieName, "", -1, "/", "", h.cfg.CookieSecure, true)
}

func (h *AuthHandler) redirectToLogin(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, "/login")
	c.Abort()
}

func (h *AuthHandler) refreshSession(c *gin.Context, refreshToken string) (*authclient.AuthResult, error) {
	val, err := h.refresh.do(refreshToken, func() (any, error) {
		return h.auth.Refresh(c.Request.Context(), refreshToken)
	})
	if err != nil {
		return nil, err
	}
	res, ok := val.(*authclient.AuthResult)
	if !ok || res == nil {
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
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
		"IsAuthenticated": false,
	})
}
