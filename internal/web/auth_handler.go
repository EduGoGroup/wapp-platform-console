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
	h.renderLogin(c, http.StatusOK, "", "")
}

// DoLogin procesa las credenciales del operador de plataforma.
//
// El 401 de credenciales y el 403 del System Gate llegan como sentinelas distintos —`iam` no los
// colapsa— y aquí se muestran con el mismo texto a propósito: al que está en la pantalla de login no
// se le dice si el correo existe.
//
// 🔑 La distinción, que en la pantalla se oculta a propósito, en el LOG es lo único que hay: quien
// diagnostica un «no puedo entrar» necesita saber si buscar la contraseña o la fila de
// `iam.user_systems`. Por eso las dos ramas escriben, y por eso hay un test que lo vigila —hasta el
// 2026-08-28 esto estaba prometido en este mismo comentario y solo se cumplía para una de las dos.
func (h *AuthHandler) DoLogin(c *gin.Context) {
	email := strings.TrimSpace(c.PostForm("email"))
	password := c.PostForm("password")
	if email == "" || password == "" {
		h.renderLogin(c, http.StatusBadRequest, "Introduce tu correo y contraseña.", email)
		return
	}

	res, err := h.auth.Login(c.Request.Context(), email, password)
	if err != nil {
		// 🔴 LAS DOS RAMAS LOGUEAN, y la de credenciales no lo hacía.
		//
		// El 2026-08-28 un operador no pudo entrar en UAT y el log NO decía por qué: solo quedaba la
		// línea del middleware con un 401 pelado. La causa hubo que DEDUCIRLA por la AUSENCIA de la
		// línea del System Gate —«no hay log de 403, luego fue 401»—, que es un razonamiento que
		// funciona una vez y deja ciego a cualquiera la siguiente. El comentario de arriba prometía
		// que «la diferencia sí queda en el log» y solo era verdad para una de las dos.
		//
		// Se registra la CAUSA, nunca el correo: en el log de esta consola no entra PII.
		switch {
		case errors.Is(err, iam.ErrForbidden):
			slog.Warn("login de plataforma rechazado por el System Gate: falta la fila en iam.user_systems para wapp.platform", "error", err)
		case errors.Is(err, iam.ErrUnauthorized):
			slog.Warn("login de plataforma rechazado por identity: credenciales inválidas", "error", err)
		}
		if errors.Is(err, iam.ErrUnauthorized) || errors.Is(err, iam.ErrForbidden) {
			h.renderLogin(c, http.StatusUnauthorized, "Credenciales inválidas o sin acceso a la consola de plataforma.", email)
			return
		}
		slog.Warn("login de plataforma rechazado", "error", err)
		h.renderLogin(c, http.StatusUnauthorized, "No se pudo iniciar sesión. Verifica tus credenciales.", email)
		return
	}

	if err := h.startSession(c, res); err != nil {
		slog.Error("falló iniciar sesión", "error", err)
		h.renderLogin(c, http.StatusInternalServerError, "Error interno al iniciar sesión.", email)
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

// renderLogin pinta la pantalla de entrada.
//
// `email` es el correo que el operador acaba de teclear, y se devuelve al formulario para que no
// tenga que reescribirlo en cada intento. La contraseña NO se repuebla, por motivos evidentes.
// Va vacío en el GET inicial.
func (h *AuthHandler) renderLogin(c *gin.Context, status int, errMsg, email string) {
	c.HTML(status, "base.html", gin.H{
		"Title":           "Iniciar sesión",
		"Subtitle":        "Consola de Plataforma",
		"ContentTemplate": "login.html",
		"Error":           errMsg,
		"Email":           email,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
		"IsAuthenticated": false,
	})
}
