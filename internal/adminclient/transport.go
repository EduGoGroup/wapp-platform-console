package adminclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	defaultTimeout   = 15 * time.Second
	maxRejectionBody = 4096
)

// Transport maneja la configuración base HTTP contra :8100.
type Transport struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewTransport construye un Transport acoplado al listener admin (:8100).
func NewTransport(baseURL string) *Transport {
	return &Transport{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: defaultTimeout},
	}
}

// ErrUnauthorized señala un 401 del upstream.
var ErrUnauthorized = errors.New("adminclient: no autorizado")

// APIError es un fallo de transporte con status HTTP del upstream.
type APIError struct {
	Op         string
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("adminclient: %s devolvió status %d", e.Op, e.StatusCode)
}

// RejectionError representa una respuesta de error con mensaje explicativo.
type RejectionError struct {
	Op         string
	StatusCode int
	Message    string
}

func (e *RejectionError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("adminclient: %s rechazada (%d): %s", e.Op, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("adminclient: %s rechazada (%d)", e.Op, e.StatusCode)
}

func statusError(op string, status int) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("%s: %w", op, ErrUnauthorized)
	}
	return &APIError{Op: op, StatusCode: status}
}

func reasonedStatusError(op string, resp *http.Response, reasoned ...int) error {
	if !slices.Contains(reasoned, resp.StatusCode) {
		return statusError(op, resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxRejectionBody)).Decode(&body)
	return &RejectionError{Op: op, StatusCode: resp.StatusCode, Message: body.Error}
}

func (t *Transport) newAuthedRequest(ctx context.Context, method, path string, body []byte, accessToken string) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.BaseURL+path, r)
	if err != nil {
		return nil, fmt.Errorf("adminclient: construir petición %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return req, nil
}

func (t *Transport) newAuthedJSONRequest(ctx context.Context, method, path string, payload any, accessToken string) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("adminclient: serializar %s: %w", path, err)
	}
	return t.newAuthedRequest(ctx, method, path, body, accessToken)
}

func drainClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
