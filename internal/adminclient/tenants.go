package adminclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// TenantSummary representa el resumen de una empresa en el listado.
type TenantSummary struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	DisplayName string  `json:"display_name"`
	PlanID      string  `json:"plan_id"`
	RevokedAt   *string `json:"revoked_at"`
}

// TenantListResult representa la respuesta paginada de listado de empresas.
type TenantListResult struct {
	Items  []TenantSummary `json:"items"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// TenantDetail representa los datos completos de una empresa.
type TenantDetail struct {
	ID                 string   `json:"id"`
	Slug               string   `json:"slug"`
	DisplayName        string   `json:"display_name"`
	PlanID             string   `json:"plan_id"`
	RevokedAt          *string  `json:"revoked_at"`
	CreatedAt          string   `json:"created_at"`
	InstallationsCount int      `json:"installations_count"`
	Features           []string `json:"features"`
}

// TenantCreateResult representa la respuesta de creación de empresa.
type TenantCreateResult struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// EnrollmentCodeResult representa un código de activación emitido.
type EnrollmentCodeResult struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

// TenantsClient habla con las rutas /admin/tenants* de la plataforma (:8100).
type TenantsClient struct {
	t *Transport
}

// NewTenantsClient construye un TenantsClient sobre el Transport dado.
func NewTenantsClient(t *Transport) *TenantsClient {
	return &TenantsClient{t: t}
}

// ListTenants lista las empresas registradas.
func (c *TenantsClient) ListTenants(ctx context.Context, accessToken string, limit, offset int) (*TenantListResult, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	path := "/admin/tenants"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	req, err := c.t.newAuthedRequest(ctx, http.MethodGet, path, nil, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: list tenants: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("list tenants", resp, http.StatusForbidden, http.StatusBadRequest)
	}

	var res TenantListResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("adminclient: decode tenants: %w", err)
	}
	return &res, nil
}

// GetTenant obtiene el detalle de una empresa.
func (c *TenantsClient) GetTenant(ctx context.Context, accessToken, id string) (*TenantDetail, error) {
	req, err := c.t.newAuthedRequest(ctx, http.MethodGet, "/admin/tenants/"+id, nil, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: get tenant: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("get tenant", resp, http.StatusNotFound, http.StatusForbidden)
	}

	var detail TenantDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("adminclient: decode tenant detail: %w", err)
	}
	return &detail, nil
}

// CreateTenant da de alta una nueva empresa en la plataforma.
func (c *TenantsClient) CreateTenant(ctx context.Context, accessToken, slug, displayName, planID string) (*TenantCreateResult, error) {
	payload := map[string]string{
		"slug":         slug,
		"display_name": displayName,
		"plan_id":      planID,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/tenants", payload, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: create tenant: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("create tenant", resp, http.StatusConflict, http.StatusBadRequest, http.StatusForbidden)
	}

	var res TenantCreateResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("adminclient: decode create tenant result: %w", err)
	}
	return &res, nil
}

// RevokeTenant corta el acceso a una empresa.
func (c *TenantsClient) RevokeTenant(ctx context.Context, accessToken, id, reason string) error {
	payload := map[string]string{
		"tenant_id": id,
		"reason":    reason,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/tenants/revoke", payload, accessToken)
	if err != nil {
		return err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: revoke tenant: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return reasonedStatusError("revoke tenant", resp, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest)
	}
	return nil
}

// RestoreTenant restaura el acceso a una empresa cortada.
func (c *TenantsClient) RestoreTenant(ctx context.Context, accessToken, id, reason string) error {
	payload := map[string]string{
		"tenant_id": id,
		"reason":    reason,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/tenants/restore", payload, accessToken)
	if err != nil {
		return err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: restore tenant: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return reasonedStatusError("restore tenant", resp, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest)
	}
	return nil
}

// IssueEnrollmentCode genera un código de enrolamiento de un solo uso para un Edge de la empresa.
//
// El servidor lee la clave "ttl" (platformadmin/handlers.go), no "ttl_seconds": encoding/json ignora
// claves desconocidas sin error, así que un desajuste aquí hace que el TTL elegido por el operador
// nunca llegue y el código viva siempre el default del servidor (24h) sin ningún aviso.
func (c *TenantsClient) IssueEnrollmentCode(ctx context.Context, accessToken, tenantID string, ttlSecs int) (*EnrollmentCodeResult, error) {
	payload := map[string]int{
		"ttl": ttlSecs,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/tenants/"+tenantID+"/enrollment-codes", payload, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: issue enrollment code: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("issue enrollment code", resp, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest)
	}

	var res EnrollmentCodeResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("adminclient: decode enrollment code result: %w", err)
	}
	return &res, nil
}
