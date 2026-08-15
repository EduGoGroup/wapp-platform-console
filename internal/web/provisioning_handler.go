package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
	"github.com/gin-gonic/gin"
)

// ProvisioningHandler gestiona el alta de nuevas empresas y la emisión de códigos de enrolamiento.
type ProvisioningHandler struct {
	tenants *adminclient.TenantsClient
}

// NewProvisioningHandler construye un ProvisioningHandler.
func NewProvisioningHandler(tenants *adminclient.TenantsClient) *ProvisioningHandler {
	return &ProvisioningHandler{tenants: tenants}
}

// ShowNewTenant muestra el formulario de alta de empresa.
func (h *ProvisioningHandler) ShowNewTenant(c *gin.Context) {
	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Nueva Empresa",
		"ContentTemplate": "tenant_new.html",
		"CurrentPath":     "/tenants/new",
		"IsAuthenticated": true,
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
	})
}

// DoCreateTenant procesa el alta de la empresa.
func (h *ProvisioningHandler) DoCreateTenant(c *gin.Context) {
	slug := strings.TrimSpace(c.PostForm("slug"))
	displayName := strings.TrimSpace(c.PostForm("display_name"))
	planID := strings.TrimSpace(c.PostForm("plan_id"))
	token := c.GetString(ctxAccessToken)

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
			"CSRFToken":       c.GetString("csrf_token"),
			"Nonce":           c.GetString("csp_nonce"),
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
			"CSRFToken":       c.GetString("csrf_token"),
			"Nonce":           c.GetString("csp_nonce"),
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
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
	})
}

// DoIssueEnrollmentCode genera y muestra un código de activación de Edge.
func (h *ProvisioningHandler) DoIssueEnrollmentCode(c *gin.Context) {
	tenantID := c.Param("id")
	token := c.GetString(ctxAccessToken)

	res, err := h.tenants.IssueEnrollmentCode(c.Request.Context(), token, tenantID, 86400)
	if err != nil {
		slog.Error("error generando código de enrolamiento", "tenant_id", tenantID, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+tenantID+"?error=code_failed")
		return
	}

	// M-10 (parcial): el código YA se emitió y no se puede volver a mostrar (es de un solo uso), así que
	// perder la relectura del tenant es peor que mostrarlo con datos incompletos. Antes, un error aquí
	// se descartaba (`_`) y dejaba `tenant` en nil: html/template aborta al evaluar `.Tenant.DisplayName`
	// sobre un puntero nil, y como Gin ya escribió `200 OK`, la página queda truncada y el código,
	// perdido. Con este fallback la página siempre renderiza completa.
	//
	// (Queda pendiente la otra mitad de M-10: separar esta vista a su propia plantilla con
	// POST-Redirect-GET para que un F5 no reemita un código nuevo dejando el anterior huérfano.)
	tenant, terr := h.tenants.GetTenant(c.Request.Context(), token, tenantID)
	if terr != nil {
		slog.Warn("no se pudo releer la empresa tras emitir el código; se muestra igual", "tenant_id", tenantID, "error", terr)
		tenant = &adminclient.TenantDetail{ID: tenantID}
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Código de Activación",
		"ContentTemplate": "tenant_detail.html",
		"Tenant":          tenant,
		"EnrollmentCode":  res.Code,
		"CodeExpiresAt":   res.ExpiresAt,
		"CurrentPath":     "/",
		"IsAuthenticated": true,
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
	})
}
