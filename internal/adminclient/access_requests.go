package adminclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// AccessRequestItem representa una solicitud en la bandeja de acceso.
//
// Systems refleja los sistemas que el usuario YA tiene concedidos hoy (no el origen de la solicitud):
// PUT /users/{id}/systems es declarativo, así que la bandeja debe precargar las casillas con este valor
// para no perder acceso que el usuario ya tenía al aprobar. SystemsKnown distingue "el usuario no tiene
// ningún sistema" (true, Systems vacío) de "el servidor no pudo leer el estado actual" (false): si el
// upstream aún no expone el campo, el JSON lo deja en su cero (false), y la plantilla debe avisar en
// vez de precargar casillas "por si acaso".
type AccessRequestItem struct {
	ID           string   `json:"id"`
	UserID       string   `json:"user_id"`
	Email        string   `json:"email"`
	Origin       string   `json:"origin"`
	CreatedAt    string   `json:"created_at"`
	Systems      []string `json:"systems"`
	SystemsKnown bool     `json:"systems_known"`
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSuccessBody)).Decode(&res); err != nil {
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
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/access-requests/"+url.PathEscape(id)+"/approve", payload, accessToken)
	if err != nil {
		return err
	}
	resp, err := c.t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: approve access request: %w", err)
	}
	defer drainClose(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return approveAccessRequestError("approve access request", resp)
	}
	return nil
}

// approvePartialBody espeja platformadmin.ApprovePartialResult (cloud/wapp-cloud-platform): NO se
// importa ese paquete — repos independientes (CLAUDE.md raíz §2) — solo se replica su forma JSON para
// poder RECONOCERLA.
type approvePartialBody struct {
	Local    string `json:"local"`
	Identity string `json:"identity"`
	Reason   string `json:"reason"`
}

// approveAccessRequestError interpreta el cuerpo de un status no-OK de ApproveAccessRequest. A
// diferencia de reasonedStatusError (que asume siempre el envelope {"error":"..."}), aquí hace falta
// mirar la FORMA del cuerpo antes de decidir: un 409 puede ser "la solicitud ya fue resuelta" (texto
// plano, ErrConflict) o una aprobación a MEDIAS ({"local","identity","reason"},
// ErrSystemsUnionUnavailable) — el status por sí solo NO los distingue (Trabajo 2, code review 056 ·
// T11). Un 502 (ErrSystemsSyncFailed) trae siempre la misma forma parcial, así que entra por el mismo
// camino; si alguna vez llegara sin ese cuerpo (defensivo), cae al RejectionError genérico igual que
// antes de este cambio.
func approveAccessRequestError(op string, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusConflict, http.StatusBadGateway:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxRejectionBody))
		var partial approvePartialBody
		if err := json.Unmarshal(raw, &partial); err == nil && partial.Local != "" && partial.Identity != "" {
			return &PartialApprovalError{Op: op, StatusCode: resp.StatusCode, Identity: partial.Identity, Reason: partial.Reason}
		}
		var rej struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &rej)
		return &RejectionError{Op: op, StatusCode: resp.StatusCode, Message: rej.Error}
	default:
		return reasonedStatusError(op, resp, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest)
	}
}

// RejectAccessRequest rechaza la solicitud guardando el motivo.
func (c *AccessRequestsClient) RejectAccessRequest(ctx context.Context, accessToken, id, reason string) error {
	payload := map[string]string{
		"reason": reason,
	}
	req, err := c.t.newAuthedJSONRequest(ctx, http.MethodPost, "/admin/access-requests/"+url.PathEscape(id)+"/reject", payload, accessToken)
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
