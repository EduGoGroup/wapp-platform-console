package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	sharedweb "github.com/EduGoGroup/wapp-shared/web"
	webgin "github.com/EduGoGroup/wapp-shared/web/gin"
	"github.com/gin-gonic/gin"
)

// ProvisioningHandler gestiona el alta de nuevas empresas y la emisión de códigos de enrolamiento.
type ProvisioningHandler struct {
	tenants *adminclient.TenantsClient
	cfg     *config.Config
}

// NewProvisioningHandler construye un ProvisioningHandler. Recibe la config porque la cookie
// efímera del código de enrolamiento hereda la MISMA política de despliegue (Secure, SameSite) que
// la cookie de sesión: dos juegos distintos en la misma consola serían dos verdades que mantener.
func NewProvisioningHandler(tenants *adminclient.TenantsClient, cfg *config.Config) *ProvisioningHandler {
	return &ProvisioningHandler{tenants: tenants, cfg: cfg}
}

// enrollmentCodePath es la pantalla del código y, a la vez, el Path EXACTO de la cookie efímera. Es
// UNA sola función a propósito: si el redirect y la cookie se construyeran por separado, bastaría
// tocar uno para que el navegador dejara de mandar la cookie (o de borrarla) sin que nada fallara al
// compilar — la página saldría vacía y solo se vería en producción.
func enrollmentCodePath(tenantID string) string {
	return "/tenants/" + tenantID + "/enrollment-code"
}

// enrollmentCodeCookiePayload es lo que viaja dentro de la cookie efímera. Las claves son cortas
// porque el valor va en una cabecera, no porque se pretenda ofuscar nada.
type enrollmentCodeCookiePayload struct {
	Code      string `json:"c"`
	ExpiresAt string `json:"e,omitempty"`
}

// ShowNewTenant muestra el formulario de alta de empresa.
func (h *ProvisioningHandler) ShowNewTenant(c *gin.Context) {
	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Nueva Empresa",
		"ContentTemplate": "tenant_new.html",
		"CurrentPath":     "/tenants/new",
		"IsAuthenticated": true,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
	})
}

// DoCreateTenant procesa el alta de la empresa.
func (h *ProvisioningHandler) DoCreateTenant(c *gin.Context) {
	slug := strings.TrimSpace(c.PostForm("slug"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	planID := strings.TrimSpace(c.PostForm("plan_id"))
	token := webgin.AccessTokenFromContext(c)

	if slug == "" || displayName == "" {
		c.HTML(http.StatusBadRequest, "base.html", gin.H{
			"Title":           "Nueva Empresa",
			"ContentTemplate": "tenant_new.html",
			"Error":           "Slug y Nombre de la empresa son requeridos.",
			"Slug":            slug,
			"DisplayName":     displayName,
			"PlanID":          planID,
			"CurrentPath":     "/tenants/new",
			"IsAuthenticated": true,
			"CSRFToken":       webgin.CSRFTokenFromContext(c),
			"Nonce":           webgin.NonceFromContext(c),
		})
		return
	}

	res, err := h.tenants.CreateTenant(c.Request.Context(), token, slug, displayName, planID)
	if err != nil {
		var rej *adminclient.RejectionError
		msg := "No se pudo crear la empresa."
		if errors.As(err, &rej) && rej.Message != "" {
			msg = rej.Message
		}
		slog.Error("error creando empresa", "slug", slug, "error", err)
		c.HTML(http.StatusBadRequest, "base.html", gin.H{
			"Title":           "Nueva Empresa",
			"ContentTemplate": "tenant_new.html",
			"Error":           msg,
			"Slug":            slug,
			"DisplayName":     displayName,
			"PlanID":          planID,
			"CurrentPath":     "/tenants/new",
			"IsAuthenticated": true,
			"CSRFToken":       webgin.CSRFTokenFromContext(c),
			"Nonce":           webgin.NonceFromContext(c),
		})
		return
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Empresa Creada",
		"ContentTemplate": "tenant_created.html",
		"TenantID":        res.ID,
		"Slug":            res.Slug,
		"DisplayName":     displayName,
		"CurrentPath":     "/tenants/new",
		"IsAuthenticated": true,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
	})
}

// DoIssueEnrollmentCode emite el código y REDIRIGE a la pantalla que lo muestra (POST-Redirect-GET).
//
// M-10 (`CODE-REVIEW-2026-08-15`): antes se renderizaba SOBRE el POST, así que un F5 reenviaba el
// formulario y emitía un código NUEVO, dejando el anterior huérfano y vivo 24 h. Se cumplía la letra
// de T4.6 («recargar no vuelve a mostrar el código») y se incumplía su espíritu. Con el 303, la
// pantalla del código es un GET: recargarla no reenvía nada.
//
// El código viaja al GET en una cookie efímera de un solo uso, NO en la URL. Es la razón de ser de
// web.OneTimeCookie: una URL acaba en el log de acceso del proxy, en la cabecera Referer y en el
// historial del navegador, y este código autoriza a enrolar un Edge durante 24 h.
func (h *ProvisioningHandler) DoIssueEnrollmentCode(c *gin.Context) {
	tenantID := c.Param("id")
	token := webgin.AccessTokenFromContext(c)

	res, err := h.tenants.IssueEnrollmentCode(c.Request.Context(), token, tenantID, 86400)
	if err != nil {
		slog.Error("error generando código de enrolamiento", "tenant_id", tenantID, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+tenantID+"?error=code_failed")
		return
	}

	value, encErr := sharedweb.EncodeCookiePayload(enrollmentCodeCookiePayload{
		Code:      res.Code,
		ExpiresAt: res.ExpiresAt,
	})
	if encErr != nil {
		// El código YA se emitió y es de un solo uso: aquí ya no se puede recuperar. Se dice
		// exactamente eso, sin el código en el log (es material que autoriza un enrolamiento).
		slog.Error("no se pudo empaquetar el código de enrolamiento emitido", "tenant_id", tenantID, "error", encErr)
		c.Redirect(http.StatusSeeOther, "/tenants/"+tenantID+"?error=code_lost")
		return
	}

	webgin.SetOneTimeCookie(c, enrollmentCodeCookieOptions(h.cfg, tenantID), value)
	c.Redirect(http.StatusSeeOther, enrollmentCodePath(tenantID))
}

// ShowEnrollmentCode muestra —UNA vez— el código que acaba de emitir el POST.
//
// La cookie se lee y se borra en el mismo gesto (web/gin.TakeOneTimeCookie), así que un F5 sobre
// esta pantalla ya no encuentra nada: redirige al detalle de la empresa y NO emite ningún código.
func (h *ProvisioningHandler) ShowEnrollmentCode(c *gin.Context) {
	tenantID := c.Param("id")
	token := webgin.AccessTokenFromContext(c)

	raw := webgin.TakeOneTimeCookie(c, enrollmentCodeCookieOptions(h.cfg, tenantID))
	if raw == "" {
		// Entrar aquí a pelo, o recargar, no es un error: simplemente no hay nada que enseñar.
		c.Redirect(http.StatusSeeOther, "/tenants/"+tenantID)
		return
	}

	var payload enrollmentCodeCookiePayload
	if err := sharedweb.DecodeCookiePayload(raw, &payload); err != nil || payload.Code == "" {
		slog.Warn("la cookie del código de enrolamiento llegó ilegible", "tenant_id", tenantID, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+tenantID+"?error=code_lost")
		return
	}

	// M-10 (la otra mitad): el código ya se emitió y no se puede volver a mostrar, así que perder la
	// relectura del tenant es peor que mostrarlo con datos incompletos. Antes, un error aquí se
	// descartaba (`_`) y dejaba `tenant` en nil: html/template abortaba al evaluar `.Tenant.DisplayName`
	// sobre un puntero nil y, como Gin ya había escrito `200 OK`, la página quedaba truncada y el
	// código, perdido. Hoy la plantilla ya no exige `.Tenant` —solo adorna con el nombre si lo hay—,
	// y el fallback se conserva para que ninguna reescritura futura vuelva a atar las dos cosas.
	nombre := ""
	if tenant, terr := h.tenants.GetTenant(c.Request.Context(), token, tenantID); terr != nil {
		slog.Warn("no se pudo releer la empresa tras emitir el código; se muestra igual", "tenant_id", tenantID, "error", terr)
	} else if tenant != nil {
		nombre = tenant.DisplayName
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Código de Activación",
		"ContentTemplate": "enrollment_code.html",
		"TenantID":        tenantID,
		"TenantName":      nombre,
		"EnrollmentCode":  payload.Code,
		"CodeExpiresAt":   payload.ExpiresAt,
		"CurrentPath":     "/",
		"IsAuthenticated": true,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
	})
}
