package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/EduGoGroup/wapp-platform-console/internal/adminclient"
	"github.com/gin-gonic/gin"
)

// TenantsHandler gestiona las vistas y acciones sobre empresas.
type TenantsHandler struct {
	tenants       *adminclient.TenantsClient
	installations *adminclient.InstallationsClient
}

// NewTenantsHandler construye un TenantsHandler.
func NewTenantsHandler(tenants *adminclient.TenantsClient, installations *adminclient.InstallationsClient) *TenantsHandler {
	return &TenantsHandler{
		tenants:       tenants,
		installations: installations,
	}
}

// ShowTenants lista las empresas registradas.
func (h *TenantsHandler) ShowTenants(c *gin.Context) {
	token := c.GetString(ctxAccessToken)
	res, err := h.tenants.ListTenants(c.Request.Context(), token, 50, 0)
	if err != nil {
		slog.Error("error listando empresas", "error", err)
		c.HTML(http.StatusOK, "base.html", gin.H{
			"Title":           "Empresas",
			"ContentTemplate": "tenants.html",
			"Error":           "No se pudieron cargar las empresas.",
			"CurrentPath":     "/",
			"IsAuthenticated": true,
			"CSRFToken":       c.GetString("csrf_token"),
			"Nonce":           c.GetString("csp_nonce"),
		})
		return
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Empresas",
		"ContentTemplate": "tenants.html",
		"Tenants":         res.Items,
		"CurrentPath":     "/",
		"IsAuthenticated": true,
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
	})
}

// ShowTenantDetail muestra el detalle de una empresa y sus instalaciones.
func (h *TenantsHandler) ShowTenantDetail(c *gin.Context) {
	id := c.Param("id")
	token := c.GetString(ctxAccessToken)

	tenant, err := h.tenants.GetTenant(c.Request.Context(), token, id)
	if err != nil {
		slog.Error("error obteniendo detalle de empresa", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/")
		return
	}

	installations, err := h.installations.ListInstallations(c.Request.Context(), token, id)
	if err != nil {
		slog.Warn("error obteniendo instalaciones de empresa", "id", id, "error", err)
		installations = []adminclient.InstallationItem{}
	}

	c.HTML(http.StatusOK, "base.html", gin.H{
		"Title":           "Detalle de Empresa",
		"ContentTemplate": "tenant_detail.html",
		"Tenant":          tenant,
		"Installations":   installations,
		"CurrentPath":     "/",
		"IsAuthenticated": true,
		"CSRFToken":       c.GetString("csrf_token"),
		"Nonce":           c.GetString("csp_nonce"),
		"Error":           flashError(c.Query("error")),
		"Success":         flashSuccess(c.Query("success")),
	})
}

// DoRevokeTenant procesa el corte de una empresa previa confirmación del slug.
//
// La barrera humana (T4.5) exige que el operador escriba a mano el slug de la empresa objetivo. Por
// eso el slug "esperado" NUNCA viaja en el propio formulario (un campo oculto es indistinguible de un
// valor de ataque): se resuelve aquí, del lado servidor, contra el `id` de la URL. Un `slug_confirm`
// que coincida con el slug de OTRA empresa no basta: solo vale el slug real del tenant `id`.
func (h *TenantsHandler) DoRevokeTenant(c *gin.Context) {
	id := c.Param("id")
	token := c.GetString(ctxAccessToken)

	tenant, err := h.tenants.GetTenant(c.Request.Context(), token, id)
	if err != nil || tenant == nil {
		slog.Error("no se pudo resolver la empresa objetivo del corte", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?error=tenant_unreachable")
		return
	}

	slugConfirm := strings.TrimSpace(c.PostForm("slug_confirm"))
	reason := strings.TrimSpace(c.PostForm("reason"))

	if slugConfirm == "" || slugConfirm != tenant.Slug {
		c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?error=slug_mismatch")
		return
	}

	if reason == "" {
		reason = "Corte administrativo desde consola de plataforma"
	}

	if err := h.tenants.RevokeTenant(c.Request.Context(), token, id, reason); err != nil {
		slog.Error("error revocando empresa", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?error=revoke_failed")
		return
	}

	c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?success=revoked")
}

// DoRestoreTenant restaura el acceso a una empresa.
func (h *TenantsHandler) DoRestoreTenant(c *gin.Context) {
	id := c.Param("id")
	reason := strings.TrimSpace(c.PostForm("reason"))
	token := c.GetString(ctxAccessToken)

	if reason == "" {
		reason = "Restauración administrativa desde consola de plataforma"
	}

	if err := h.tenants.RestoreTenant(c.Request.Context(), token, id, reason); err != nil {
		slog.Error("error restaurando empresa", "id", id, "error", err)
		c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?error=restore_failed")
		return
	}

	c.Redirect(http.StatusSeeOther, "/tenants/"+id+"?success=restored")
}
