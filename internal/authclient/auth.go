package authclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sharedjwt "github.com/EduGoGroup/wapp-shared/auth/jwt"
	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultTimeout     = 15 * time.Second
	SystemWappPlatform = "wapp.platform"
)

// IdentityContext resume los datos del token de contexto.
type IdentityContext struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles"`
}

// AuthResult agrupa los tokens de sesión.
type AuthResult struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresAt    string          `json:"expires_at"`
	Context      IdentityContext `json:"context"`
}

// IdentityTokens es el resultado del login/refresh en identity-api.
type IdentityTokens struct {
	IdentityToken string `json:"identity_token"`
	RefreshToken  string `json:"refresh_token"`
	TokenType     string `json:"token_type"`
	ExpiresAt     string `json:"expires_at"`
}

// ExchangedToken es el resultado de POST /api/v1/auth/exchange en wapp-cloud-platform (:8103).
type ExchangedToken struct {
	ContextToken string `json:"context_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"`
}

// Client gestiona la autenticación delegada en identity y el canje en la plataforma.
type Client struct {
	identityURL string
	publicAPI   string
	httpClient  *http.Client
}

// NewClient construye el cliente de autenticación.
func NewClient(identityURL, publicAPIURL string) *Client {
	return &Client{
		identityURL: strings.TrimRight(identityURL, "/"),
		publicAPI:   strings.TrimRight(publicAPIURL, "/"),
		httpClient:  &http.Client{Timeout: defaultTimeout},
	}
}

// ErrUnauthorized señala credenciales inválidas.
var ErrUnauthorized = errors.New("authclient: credenciales inválidas o no autorizado")

// Login autentica contra identity con system="wapp.platform" y canjea por Context Token.
func (c *Client) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	tokens, err := c.identityLogin(ctx, email, password)
	if err != nil {
		return nil, err
	}
	return c.exchange(ctx, tokens)
}

// Refresh renueva la sesión mediante el refresh token en identity y canjea de nuevo.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	tokens, err := c.identityRefresh(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return c.exchange(ctx, tokens)
}

// Logout revoca la sesión en identity.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	payload := map[string]string{"refresh_token": refreshToken}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.identityURL+"/api/v1/auth/logout", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

func (c *Client) identityLogin(ctx context.Context, email, password string) (*IdentityTokens, error) {
	payload := map[string]string{
		"email":    email,
		"password": password,
		"system":   SystemWappPlatform,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.identityURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authclient: identity login: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authclient: identity login retornó status %d", resp.StatusCode)
	}

	var res IdentityTokens
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("authclient: decode identity tokens: %w", err)
	}
	return &res, nil
}

func (c *Client) identityRefresh(ctx context.Context, refreshToken string) (*IdentityTokens, error) {
	payload := map[string]string{
		"refresh_token": refreshToken,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.identityURL+"/api/v1/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authclient: identity refresh: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authclient: identity refresh retornó status %d", resp.StatusCode)
	}

	var res IdentityTokens
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("authclient: decode refresh tokens: %w", err)
	}
	return &res, nil
}

func (c *Client) exchange(ctx context.Context, tokens *IdentityTokens) (*AuthResult, error) {
	payload := map[string]string{
		"identity_token": tokens.IdentityToken,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.publicAPI+"/api/v1/auth/exchange", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authclient: exchange: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authclient: exchange retornó status %d", resp.StatusCode)
	}

	var ex ExchangedToken
	if err := json.NewDecoder(resp.Body).Decode(&ex); err != nil {
		return nil, fmt.Errorf("authclient: decode exchange response: %w", err)
	}

	return &AuthResult{
		AccessToken:  ex.ContextToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    ex.ExpiresAt,
		Context:      contextOf(ex.ContextToken),
	}, nil
}

func contextOf(contextToken string) IdentityContext {
	var claims sharedjwt.Claims
	if _, _, err := jwt.NewParser().ParseUnverified(contextToken, &claims); err != nil {
		return IdentityContext{}
	}
	return IdentityContext{TenantID: claims.TenantID, UserID: claims.UserID, Roles: claims.Roles}
}
