package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
	webgin "github.com/EduGoGroup/wapp-shared/web/gin"
	"github.com/gin-gonic/gin"
)

// AccessRequestsHandler gestiona la bandeja de solicitudes de acceso a la plataforma.
type AccessRequestsHandler struct {
	accessRequests *adminclient.AccessRequestsClient
	tenants        *adminclient.TenantsClient
}

// NewAccessRequestsHandler construye un AccessRequestsHandler.
func NewAccessRequestsHandler(accessRequests *adminclient.AccessRequestsClient, tenants *adminclient.TenantsClient) *AccessRequestsHandler {
	return &AccessRequestsHandler{
		accessRequests: accessRequests,
		tenants:        tenants,
	}
}

// ShowAccessRequests muestra las solicitudes pendientes en la bandeja.
func (h *AccessRequestsHandler) ShowAccessRequests(c *gin.Context) {
	token := webgin.AccessTokenFromContext(c)
	requests, err := h.accessRequests.ListAccessRequests(c.Request.Context(), token, "pending")
	if err != nil {
		slog.Error("error listando solicitudes de acceso", "error", err)
		requests = []adminclient.AccessRequestItem{}
	}

	tenantsRes, err := h.tenants.ListTenants(c.Request.Context(), token, 100, 0)
	var tenants []adminclient.TenantSummary
	if err == nil && tenantsRes != nil {
		tenants = tenantsRes.Items
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Bandeja de Acceso",
		"ContentTemplate": "access_requests.html",
		"Requests":        requests,
		"Tenants":         tenants,
		"CurrentPath":     "/access-requests",
		"IsAuthenticated": true,
		"CSRFToken":       webgin.CSRFTokenFromContext(c),
		"Nonce":           webgin.NonceFromContext(c),
		"Error":           flashError(c.Query("error")),
		"Success":         flashSuccess(c.Query("success")),
	})
}

// DoApproveAccessRequest procesa la aprobación de una solicitud.
func (h *AccessRequestsHandler) DoApproveAccessRequest(c *gin.Context) {
	id := c.Param("id")
	tenantID := strings.TrimSpace(c.PostForm("tenant_id"))
	role := strings.TrimSpace(c.PostForm("role"))
	systems := c.PostFormArray("systems")
	token := webgin.AccessTokenFromContext(c)

	if tenantID == "" || role == "" {
		c.Redirect(http.StatusSeeOther, "/access-requests?error=missing_fields")
		return
	}

	// Nunca se inventa un default que conceda sistemas: si el admin desmarcó todo a propósito (o por
	// error), se rechaza con un error explícito. PUT /users/{id}/systems es declarativo, así que un
	// default aquí concedería acceso que nadie pidió.
	if len(systems) == 0 {
		c.Redirect(http.StatusSeeOther, "/access-requests?error=missing_systems")
		return
	}

	err := h.accessRequests.ApproveAccessRequest(c.Request.Context(), token, id, tenantID, role, systems)
	if err != nil {
		// M-11: nunca se refleja el texto crudo del upstream en la URL — un "&" o "#" en el mensaje
		// rompería la query, y es una superficie de reflejo innecesaria. Se usa un código estable y el
		// detalle real va solo al log.
		errCode := "approve_failed"
		var partial *adminclient.PartialApprovalError
		switch {
		case errors.As(err, &partial) && partial.Identity == "skipped":
			// 409 a MEDIAS (Trabajo 2, code review 056 · T11): lo local (tenant + rol) quedó escrito, los
			// systems de identity NO se tocaron A PROPÓSITO (el usuario ya tenía una aprobación previa y
			// no hay lectura segura para unir sin arriesgar reemplazarle acceso). No es transitorio:
			// reintentar no lo arregla, hace falta reconciliar el estado del usuario a mano.
			errCode = "approve_partial_skipped"
		case errors.As(err, &partial):
			// "failed" (502, ErrSystemsSyncFailed): la propagación a identity falló de verdad. Mismo
			// mensaje que ya existía para este caso.
			errCode = "approve_partial"
		default:
			var rej *adminclient.RejectionError
			if errors.As(err, &rej) && rej.StatusCode == http.StatusBadGateway {
				// Defensivo: un 502 que llegara sin el cuerpo parcial esperado se sigue tratando como
				// parcial (comportamiento previo a este cambio).
				errCode = "approve_partial"
			}
		}
		slog.Error("error aprobando solicitud de acceso", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/access-requests?error="+errCode)
		return
	}

	c.Redirect(http.StatusSeeOther, "/access-requests?success=approved")
}

// DoRejectAccessRequest procesa el rechazo con motivo obligatorio.
func (h *AccessRequestsHandler) DoRejectAccessRequest(c *gin.Context) {
	id := c.Param("id")
	reason := strings.TrimSpace(c.PostForm("reason"))
	token := webgin.AccessTokenFromContext(c)

	if reason == "" {
		c.Redirect(http.StatusSeeOther, "/access-requests?error=missing_reason")
		return
	}

	err := h.accessRequests.RejectAccessRequest(c.Request.Context(), token, id, reason)
	if err != nil {
		slog.Error("error rechazando solicitud de acceso", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/access-requests?error=reject_failed")
		return
	}

	c.Redirect(http.StatusSeeOther, "/access-requests?success=rejected")
}
