package adminclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// AccessRequestItem representa una solicitud en la bandeja de acceso.
type AccessRequestItem struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Origin    string `json:"origin"`
	CreatedAt string `json:"created_at"`
}

type accessRequestsResponse struct {
	Items []AccessRequestItem `json:"items"`
}

// AccessRequestsClient consulta y gestiona la bandeja de solicitudes (:8100).
type AccessRequestsClient struct {
	t *Transport
}

// NewAccessRequestsClient construye un AccessRequestsClient sobre el Transport dado.
func NewAccessRequestsClient(t *Transport) *AccessRequestsClient {
	return &AccessRequestsClient{t: t}
}

// ListAccessRequests obtiene las solicitudes según el estado indicado.
func (c *AccessRequestsClient) ListAccessRequests(ctx context.Context, accessToken, status string) ([]AccessRequestItem, error) {
	path := "/admin/access-requests"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}

	req, err := c.t.newAuthedRequest(ctx, http.MethodGet, path, nil, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: list access requests: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("list access requests", resp, http.StatusForbidden, http.StatusBadRequest)
	}

	var res accessRequestsResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("adminclient: decode access requests: %w", err)
	}
	return res.Items, nil
}

// ApproveAccessRequest aprueba la solicitud asignando empresa, rol y aplicaciones.
func (c *AccessRequestsClient) ApproveAccessRequest(ctx context.Context, accessToken, id, tenantID, role string, systems []string) error {
	payload := map[string]any{
		"tenant_id": tenantID,
		"role":      role,
		"systems":   systems,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/access-requests/"+id+"/approve", payload, accessToken)
	if err != nil {
		return err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: approve access request: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return reasonedStatusError("approve access request", resp, http.StatusNotFound, http.StatusConflict, http.StatusForbidden, http.StatusBadRequest, http.StatusBadGateway)
	}
	return nil
}

// RejectAccessRequest rechaza la solicitud guardando el motivo.
func (c *AccessRequestsClient) RejectAccessRequest(ctx context.Context, accessToken, id, reason string) error {
	payload := map[string]string{
		"reason": reason,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/access-requests/"+id+"/reject", payload, accessToken)
	if err != nil {
		return err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: reject access request: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return reasonedStatusError("reject access request", resp, http.StatusNotFound, http.StatusConflict, http.StatusForbidden, http.StatusBadRequest)
	}
	return nil
}
