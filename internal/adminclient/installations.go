package adminclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// InstallationItem representa un Edge instalado de la empresa.
type InstallationItem struct {
	EdgeID       string  `json:"edge_id"`
	Sessions     int     `json:"sessions"`
	LastSeenAt   *string `json:"last_seen_at"`
	LeaseRevoked bool    `json:"lease_revoked"`
}

type installationsResponse struct {
	Items []InstallationItem `json:"items"`
}

// InstallationsClient maneja las consultas de instalaciones (:8100).
type InstallationsClient struct {
	t *Transport
}

// NewInstallationsClient construye un InstallationsClient sobre el Transport dado.
func NewInstallationsClient(t *Transport) *InstallationsClient {
	return &InstallationsClient{t: t}
}

// ListInstallations consulta los Edges de una empresa.
func (c *InstallationsClient) ListInstallations(ctx context.Context, accessToken, tenantID string) ([]InstallationItem, error) {
	req, err := c.t.newAuthedRequest(ctx, http.MethodGet, "/admin/tenants/"+tenantID+"/installations", nil, accessToken)
	if err != nil {
		return nil, err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adminclient: list installations: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, reasonedStatusError("list installations", resp, http.StatusNotFound, http.StatusForbidden)
	}

	var res installationsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSuccessBody)).Decode(&res); err != nil {
		return nil, fmt.Errorf("adminclient: decode installations: %w", err)
	}
	return res.Items, nil
}
