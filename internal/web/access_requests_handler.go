package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
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
	token := c.GetString(ctxAccessToken)
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
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
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
	token := c.GetString(ctxAccessToken)

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
		var rej *adminclient.RejectionError
		if errors.As(err, &rej) && rej.StatusCode == http.StatusBadGateway {
			// 502: la plataforma pudo escribir localmente antes de fallar en la propagación. Distinto
			// del resto de fallos, para que el operador no reintente a ciegas.
			errCode = "approve_partial"
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
	token := c.GetString(ctxAccessToken)

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
