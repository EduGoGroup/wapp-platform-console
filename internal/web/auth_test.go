package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sharedweb "github.com/EduGoGroup/wapp-shared/web"
)

func TestAuth_LoginSuccess(t *testing.T) {
	t.Parallel()

	// Simular identity-core (:8200)
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" && r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["email"] == "admin@wapp.local" && body["password"] == "test-only-password" && body["system"] == "wapp.platform" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{
					"identity_token": "id-token-123",
					"refresh_token":  "rt-token-123",
					"token_type":     "Bearer",
					"expires_at":     time.Now().Add(time.Hour).Format(time.RFC3339),
				})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer identitySrv.Close()

	// Simular cloud-platform (:8103) para exchange
	platformSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/exchange" && r.Method == http.MethodPost {
			exp := time.Now().Add(time.Hour)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"context_token": makeAdminToken(t, exp),
				"token_type":    "Bearer",
				"expires_at":    exp.Format(time.RFC3339),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer platformSrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", platformSrv.URL, identitySrv.URL)
	router := NewRouter(cfg)

	form := url.Values{
		"email":    {"admin@wapp.local"},
		"password": {"test-only-password"},
	}
	rec := postFormWithCSRF(router, "/login", form, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want 303. Body: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}

	foundCookie := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			foundCookie = true
			if !c.HttpOnly {
				t.Error("cookie de sesión debe ser HttpOnly")
			}
		}
	}
	if !foundCookie {
		t.Fatal("cookie de sesión no fue emitida")
	}
}

// TestAuth_LoginSetsSecureAndSameSiteCookie complementa TestAuth_LoginSuccess (que solo puede afirmar
// HttpOnly, porque testConfig fija CookieSecure:false). T4.4 exige los tres atributos: aquí se configura
// CookieSecure=true y CookieSameSite=strict para poder afirmar Secure y SameSite de verdad.
func TestAuth_LoginSetsSecureAndSameSiteCookie(t *testing.T) {
	t.Parallel()

	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"identity_token": "id-token-123",
				"refresh_token":  "rt-token-123",
				"token_type":     "Bearer",
				"expires_at":     time.Now().Add(time.Hour).Format(time.RFC3339),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer identitySrv.Close()

	platformSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/exchange" && r.Method == http.MethodPost {
			exp := time.Now().Add(time.Hour)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"context_token": makeAdminToken(t, exp),
				"token_type":    "Bearer",
				"expires_at":    exp.Format(time.RFC3339),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer platformSrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", platformSrv.URL, identitySrv.URL)
	cfg.CookieSecure = true
	cfg.CookieSameSite = "strict"
	router := NewRouter(cfg)

	form := url.Values{"email": {"admin@wapp.local"}, "password": {"test-only-password"}}
	rec := postFormWithCSRF(router, "/login", form, nil)

	var sessCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessCookie = c
		}
	}
	if sessCookie == nil {
		t.Fatal("cookie de sesión no fue emitida")
	}
	if !sessCookie.HttpOnly {
		t.Error("cookie de sesión debe ser HttpOnly")
	}
	if !sessCookie.Secure {
		t.Error("cookie de sesión debe ser Secure cuando CookieSecure=true")
	}
	if sessCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie de sesión SameSite = %v, want Strict (CookieSameSite=strict)", sessCookie.SameSite)
	}
}

func TestAuth_LoginInvalidCreds(t *testing.T) {
	t.Parallel()
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	form := url.Values{
		"email":    {"wrong@wapp.local"},
		"password": {"wrongpass"},
	}
	rec := postFormWithCSRF(router, "/login", form, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /login status = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Credenciales inválidas") {
		t.Fatalf("mensaje de error esperado en cuerpo: %s", body)
	}
}

// TestAuth_LogoutClearsCookie verifica el efecto local (la cookie caduca) Y el efecto remoto: T4.4 exige
// que "logout la invalida" en identity, no solo que el navegador la olvide. El mock antes respondía 200
// a todo sin que nadie comprobara que /auth/logout fue realmente invocado.
func TestAuth_LogoutClearsCookie(t *testing.T) {
	t.Parallel()
	var logoutCalled atomic.Bool
	var logoutRefreshToken atomic.Value

	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/logout" && r.Method == http.MethodPost {
			logoutCalled.Store(true)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			logoutRefreshToken.Store(body["refresh_token"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	sess := adminSessionCookie(t)
	rec := postFormWithCSRF(router, "/logout", url.Values{}, sess)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}

	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cookie de sesión no fue caducada")
	}

	if !logoutCalled.Load() {
		t.Fatal("logout no llamó a POST /api/v1/auth/logout en identity")
	}
	if rt, _ := logoutRefreshToken.Load().(string); rt != "rt-1" {
		t.Fatalf("logout envió refresh_token = %q, want %q (el de adminSessionCookie)", rt, "rt-1")
	}
}

// TestAuth_LogoutClearsCookieEvenIfIdentityFails complementa TestAuth_LogoutClearsCookie (que solo
// cubre el 200): T4.4/CODE-REVIEW-2026-08-15 #3 exige que la cookie local se borre SIEMPRE, aunque
// identity responda con error — el operador no debe quedarse con una sesión que él cree cerrada—,
// pero el fallo tiene que quedar en el log en vez de perderse en silencio.
func TestAuth_LogoutClearsCookieEvenIfIdentityFails(t *testing.T) {
	t.Parallel()
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	sess := adminSessionCookie(t)
	rec := postFormWithCSRF(router, "/logout", url.Values{}, sess)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout status = %d, want 303", rec.Code)
	}

	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cookie de sesión debe borrarse localmente aunque identity falle")
	}
}

// TestAuth_ExpiredSessionRedirectsToLogin ejercita sessionValid() de verdad: hasta ahora el único test
// sin sesión (TestAuth_UnauthenticatedRedirectsToLogin) no pasa por parseAccessClaims/sessionValid, así
// que un sessionValid() que siempre devolviera true no lo habría cazado. Aquí la cookie SÍ decodifica a
// un access token válido, pero expirado, y sin refresh_token (para no entrar en la rama de refresco y
// aislar exactamente la comprobación de expiración).
func TestAuth_ExpiredSessionRedirectsToLogin(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	expiredToken := makeAdminToken(t, time.Now().Add(-time.Hour))
	val, err := sharedweb.EncodeSession(sharedweb.SessionData{AccessToken: expiredToken})
	if err != nil {
		t.Fatalf("EncodeSession: %v", err)
	}
	sess := &http.Cookie{Name: sessionCookieName, Value: val}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / con sesión expirada status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}
}

func TestAuth_UnauthenticatedRedirectsToLogin(t *testing.T) {
	t.Parallel()
	router := NewRouter(testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", "http://127.0.0.1:8200"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / sin auth status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}
}

// TestAuth_ElCorreoSobreviveAlIntentoFallido — el operador no debe reescribir su correo en cada
// intento. Nació de campo (2026-08-28): tras un login fallido el formulario salía con el campo vacío
// y solo el placeholder, así que parecía que ni siquiera se había enviado.
//
// 🔴 La contraseña NO se repuebla, y el test lo exige: repoblar el correo es comodidad; repoblar la
// contraseña sería mandarla de vuelta al navegador en el HTML.
func TestAuth_ElCorreoSobreviveAlIntentoFallido(t *testing.T) {
	t.Parallel()
	identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer identitySrv.Close()

	cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
	router := NewRouter(cfg)

	const correo = "staff-plataforma@wapp.internal"
	const clave = "la-que-tecleo-y-no-cuela"
	form := url.Values{"email": {correo}, "password": {clave}}
	rec := postFormWithCSRF(router, "/login", form, nil)

	body := rec.Body.String()
	if !strings.Contains(body, `value="`+correo+`"`) {
		t.Fatalf("el correo tecleado tiene que volver en el formulario y no vuelve; cuerpo: %s", body)
	}
	if strings.Contains(body, clave) {
		t.Fatal("la contraseña NO puede volver al navegador dentro del HTML")
	}
}

// TestAuth_ElLogDISTINGUE401De403 — el mensaje de la pantalla funde credenciales y System Gate a
// propósito (no se le dice al visitante si el correo existe), así que el LOG es el único sitio donde
// queda la diferencia. Quien diagnostica un «no puedo entrar» decide con esa línea si buscar la
// contraseña o la fila de iam.user_systems.
//
// 🔴 Esto nació de un fallo real: el 2026-08-28 un operador no pudo entrar en UAT y el log solo tenía
// un 401 pelado del middleware. La causa hubo que deducirla por la AUSENCIA de la línea del System
// Gate, porque la rama de credenciales no escribía nada — mientras el comentario del handler
// prometía que «la diferencia sí queda en el log».
func TestAuth_ElLogDISTINGUE401De403(t *testing.T) {
	casos := []struct {
		nombre     string
		estado     int
		esperado   string
		noEsperado string
	}{
		{"credenciales", http.StatusUnauthorized, "credenciales inválidas", "System Gate"},
		{"system_gate", http.StatusForbidden, "System Gate", "credenciales inválidas"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			identitySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.estado)
			}))
			defer identitySrv.Close()

			var log bytes.Buffer
			anterior := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelWarn})))
			defer slog.SetDefault(anterior)

			cfg := testConfig("http://127.0.0.1:8100", "http://127.0.0.1:8103", identitySrv.URL)
			form := url.Values{"email": {"quien@wapp.internal"}, "password": {"lo-que-sea"}}
			postFormWithCSRF(NewRouter(cfg), "/login", form, nil)

			escrito := log.String()
			if !strings.Contains(escrito, c.esperado) {
				t.Fatalf("con identity devolviendo %d, el log tiene que decir %q y dice: %s", c.estado, c.esperado, escrito)
			}
			if strings.Contains(escrito, c.noEsperado) {
				t.Fatalf("con identity devolviendo %d, el log NO puede decir %q: %s", c.estado, c.noEsperado, escrito)
			}
			// CERO PII: el correo del operador no entra en el log de esta consola.
			if strings.Contains(escrito, "quien@wapp.internal") {
				t.Fatalf("el correo NO puede aparecer en el log: %s", escrito)
			}
		})
	}
}
