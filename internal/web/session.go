package web

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	sharedjwt "github.com/EduGoGroup/wapp-shared/auth/jwt"
	"github.com/golang-jwt/jwt/v5"
)

const (
	sessionCookieName = "wapp_platform_session"
	csrfCookieName    = "wapp_platform_csrf"
	csrfFieldName     = "csrf_token"
	ctxAccessToken    = "access_token"
	ctxRefreshToken   = "refresh_token"
	ctxUserID         = "user_id"
	ctxTenantID       = "tenant_id"
)

type sessionData struct {
	AccessToken  string `json:"a"`
	RefreshToken string `json:"r"`
	ExpiresAt    string `json:"e,omitempty"`
}

func encodeSession(s sessionData) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSession(value string) (sessionData, error) {
	var s sessionData
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	return s, nil
}

var unverifiedParser = jwt.NewParser()

func parseAccessClaims(accessToken string) (*sharedjwt.Claims, error) {
	var claims sharedjwt.Claims
	if _, _, err := unverifiedParser.ParseUnverified(accessToken, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func sessionValid(claims *sharedjwt.Claims) bool {
	if claims.ExpiresAt == nil {
		return false
	}
	return claims.ExpiresAt.After(time.Now())
}

const refreshMargin = 2 * time.Minute

func refreshDue(claims *sharedjwt.Claims) bool {
	if claims.ExpiresAt == nil {
		return true
	}
	return time.Until(claims.ExpiresAt.Time) < refreshMargin
}

type refreshFlightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

type refreshGroup struct {
	mu sync.Mutex
	m  map[string]*refreshFlightCall
}

func newRefreshGroup() *refreshGroup {
	return &refreshGroup{m: make(map[string]*refreshFlightCall)}
}

func (g *refreshGroup) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := new(refreshFlightCall)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	// defer, no al final de la función: si fn() entra en pánico, gin.Recovery() lo atrapa aguas
	// arriba, pero sin este defer c.wg.Done() y el delete() nunca correrían, y toda petición
	// posterior con la misma key se quedaría colgada para siempre en c.wg.Wait() (sin timeout).
	defer func() {
		c.wg.Done()
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
	}()

	c.val, c.err = fn()
	return c.val, c.err
}
