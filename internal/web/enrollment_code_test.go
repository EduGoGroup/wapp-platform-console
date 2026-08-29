package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	sharedweb "github.com/EduGoGroup/wapp-shared/web"
)

// Este fichero cierra M-10 (`docs/completed/056-consola-de-plataforma/CODE-REVIEW-2026-08-15.md`):
// `DoIssueEnrollmentCode` renderizaba SOBRE el POST, así que un F5 reenviaba el formulario y emitía
// un código de enrolamiento NUEVO, dejando el anterior huérfano y vivo 24 h.
//
// El criterio no se comprueba "a ojo" mirando si la página se ve igual: se CUENTAN las emisiones
// contra el doble de :8100. Y se comprueba con un navegador de verdad —cliente con `cookiejar`—
// porque la cookie efímera del código depende de cosas que un `httptest.NewRecorder` no simula: que
// el tarro honre el borrado (`MaxAge=-1`) y que el `Path` acote a qué peticiones se envía.

// enrollmentAdminStub levanta el doble de :8100 y devuelve el contador de emisiones. Cada POST a
// /admin/tenants/<id>/enrollment-codes suma uno y devuelve un código distinto, para que un segundo
// código no se pueda confundir con el primero.
func enrollmentAdminStub(t *testing.T, tenantID string, tenantOK bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var emisiones atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/tenants/"+tenantID+"/enrollment-codes" && r.Method == http.MethodPost:
			n := emisiones.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":       codigoEmitido(int(n)),
				"expires_at": "2026-08-29T00:00:00Z",
			})
		case r.URL.Path == "/admin/tenants/"+tenantID && r.Method == http.MethodGet:
			if !tenantOK {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": tenantID, "slug": "empresa-alfa", "display_name": "Empresa Alfa",
				"plan_id": "basic", "created_at": "2026-08-14T00:00:00Z",
			})
		case r.URL.Path == "/admin/tenants/"+tenantID+"/installations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &emisiones
}

// codigoEmitido nombra la n-ésima emisión. Si un F5 reemitiera, en pantalla aparecería la 2.ª.
func codigoEmitido(n int) string {
	return "ACT-EMISION-" + string(rune('0'+n))
}

// consola arranca la consola completa contra el doble y devuelve un cliente con TARRO DE COOKIES,
// ya con la sesión de operador dentro y sin seguir redirects (para poder mirar cada 303 por dentro).
func consola(t *testing.T, adminURL string) (*httptest.Server, *http.Client) {
	t.Helper()

	cfg := testConfig(adminURL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	srv := httptest.NewServer(NewRouter(cfg))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url del servidor: %v", err)
	}
	jar.SetCookies(base, []*http.Cookie{adminSessionCookie(t)})

	cliente := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// El middleware CSRF siembra su cookie en el primer GET; el tarro la recoge sola.
	resp, err := cliente.Get(srv.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	_ = leer(t, resp)

	return srv, cliente
}

// tokenCSRF saca del tarro el token que hay que devolver en el formulario.
func tokenCSRF(t *testing.T, cliente *http.Client, srvURL string) string {
	t.Helper()
	u, _ := url.Parse(srvURL)
	for _, ck := range cliente.Jar.Cookies(u) {
		if ck.Name == csrfCookieName {
			return ck.Value
		}
	}
	t.Fatal("el tarro no tiene la cookie CSRF: el POST se rechazaría con 403 y el test no probaría nada")
	return ""
}

func leer(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("leer cuerpo: %v", err)
	}
	return string(b)
}

// emitir hace el POST del formulario y devuelve la respuesta SIN seguir el redirect.
func emitir(t *testing.T, cliente *http.Client, srvURL, tenantID string) *http.Response {
	t.Helper()
	form := url.Values{"csrf_token": {tokenCSRF(t, cliente, srvURL)}}
	resp, err := cliente.PostForm(srvURL+"/tenants/"+tenantID+"/enrollment-codes", form)
	if err != nil {
		t.Fatalf("POST enrollment-codes: %v", err)
	}
	return resp
}

// TestEnrollmentCode_F5NoReemite es el criterio de T-A10, contado y no mirado: tras ver la pantalla
// del código, recargarla NO produce una segunda emisión.
func TestEnrollmentCode_F5NoReemite(t *testing.T) {
	t.Parallel()

	adminSrv, emisiones := enrollmentAdminStub(t, "t-1", true)
	srv, cliente := consola(t, adminSrv.URL)

	// 1. El POST emite y REDIRIGE. Si volviera a renderizar (200), el F5 reenviaría el formulario.
	post := emitir(t, cliente, srv.URL, "t-1")
	_ = leer(t, post)
	if post.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303: sin redirect, un F5 reenvía el POST y emite otro código", post.StatusCode)
	}
	destino := post.Header.Get("Location")
	if destino != "/tenants/t-1/enrollment-code" {
		t.Fatalf("Location = %q, want /tenants/t-1/enrollment-code", destino)
	}
	// 🔴 El código NO puede ir en la URL: acabaría en el log de acceso, en el Referer y en el
	// historial, y autoriza a enrolar un Edge durante 24 h.
	if strings.Contains(destino, codigoEmitido(1)) || strings.Contains(destino, "code=") {
		t.Fatalf("el código viaja en la URL del redirect: %q", destino)
	}
	if emisiones.Load() != 1 {
		t.Fatalf("emisiones tras el POST = %d, want 1", emisiones.Load())
	}

	// 2. La pantalla del código: aquí, y solo aquí, se ve.
	primero, err := cliente.Get(srv.URL + destino)
	if err != nil {
		t.Fatalf("GET %s: %v", destino, err)
	}
	cuerpo := leer(t, primero)
	if primero.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200. Body: %s", destino, primero.StatusCode, cuerpo)
	}
	if !strings.Contains(cuerpo, codigoEmitido(1)) {
		t.Fatalf("la pantalla no muestra el código emitido. Body: %s", cuerpo)
	}
	if !strings.Contains(cuerpo, "</html>") {
		t.Fatal("la página del código quedó truncada")
	}

	// 3. F5. El navegador repite el GET —no hay POST que reenviar— y la cookie ya no está.
	segundo, err := cliente.Get(srv.URL + destino)
	if err != nil {
		t.Fatalf("F5 sobre %s: %v", destino, err)
	}
	cuerpo2 := leer(t, segundo)
	if segundo.StatusCode != http.StatusSeeOther {
		t.Fatalf("F5 = %d, want 303 al detalle de la empresa. Body: %s", segundo.StatusCode, cuerpo2)
	}
	if strings.Contains(cuerpo2, codigoEmitido(1)) {
		t.Fatal("el código reaparece en la recarga: la cookie no se consumió")
	}

	// 4. EL CRITERIO: una sola emisión en todo el recorrido.
	if n := emisiones.Load(); n != 1 {
		t.Fatalf("emisiones = %d, want 1: el F5 volvió a emitir un código de enrolamiento (M-10)", n)
	}
}

// TestEnrollmentCode_SinCookieNoHayPantalla: entrar a la URL de la pantalla a pelo —o compartirla—
// no enseña ningún código ni emite ninguno. El secreto está en la cookie, no en la dirección.
func TestEnrollmentCode_SinCookieNoHayPantalla(t *testing.T) {
	t.Parallel()

	adminSrv, emisiones := enrollmentAdminStub(t, "t-1", true)
	cfg := testConfig(adminSrv.URL, "http://127.0.0.1:8103", "http://127.0.0.1:8200")
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/tenants/t-1/enrollment-code", nil)
	req.AddCookie(adminSessionCookie(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET sin cookie = %d, want 303 al detalle", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/tenants/t-1" {
		t.Fatalf("Location = %q, want /tenants/t-1", loc)
	}
	if n := emisiones.Load(); n != 0 {
		t.Fatalf("emisiones = %d: abrir la pantalla NO puede emitir nada", n)
	}
}

// TestEnrollmentCode_LaCookieEsHttpOnlyYAcotada fija las dos propiedades del transporte que ningún
// otro test de esta consola vigila: si dejara de ser HttpOnly, un XSS leería el código sin raspar el
// DOM; si el Path se ensanchara, el código viajaría en todas las peticiones de la consola.
func TestEnrollmentCode_LaCookieEsHttpOnlyYAcotada(t *testing.T) {
	t.Parallel()

	adminSrv, _ := enrollmentAdminStub(t, "t-1", true)
	srv, cliente := consola(t, adminSrv.URL)

	post := emitir(t, cliente, srv.URL, "t-1")
	_ = leer(t, post)

	var puesta *http.Cookie
	for _, ck := range post.Cookies() {
		if ck.Name == enrollmentCodeCookieName {
			puesta = ck
		}
	}
	if puesta == nil {
		t.Fatal("el 303 no trae la cookie del código: la pantalla saldría vacía")
	}
	if !puesta.HttpOnly {
		t.Error("la cookie del código debe ser HttpOnly")
	}
	if puesta.Path != "/tenants/t-1/enrollment-code" {
		t.Errorf("Path = %q, want /tenants/t-1/enrollment-code", puesta.Path)
	}
	if puesta.MaxAge <= 0 || puesta.MaxAge > 300 {
		t.Errorf("MaxAge = %d: la cookie del código es efímera (~60 s), no una sesión", puesta.MaxAge)
	}
	// El valor viaja EMPAQUETADO (JSON + base64 URL-safe), no cifrado: lo que se comprueba aquí es
	// que llega entero por el cable, porque el alfabeto equivocado lo corrompería en silencio.
	var carga enrollmentCodeCookiePayload
	if err := sharedweb.DecodeCookiePayload(puesta.Value, &carga); err != nil {
		t.Fatalf("el valor de la cookie no se puede releer: %v", err)
	}
	if carga.Code != codigoEmitido(1) {
		t.Errorf("el código que viaja en la cookie = %q, want %q", carga.Code, codigoEmitido(1))
	}
}
