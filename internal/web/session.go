package web

import (
	"time"

	"github.com/EduGoGroup/wapp-platform-console/internal/config"
	sharedjwt "github.com/EduGoGroup/wapp-shared/auth/jwt"
	sharedweb "github.com/EduGoGroup/wapp-shared/web"
	"github.com/golang-jwt/jwt/v5"
)

// Los nombres de cookie son de ESTA consola, no del módulo: `wapp-shared/web` los expone como
// PARÁMETRO (web.CSRFOptions.CookieName, web.SessionCookieOptions.Name) justo porque el BFF del
// cliente y esta consola conviven en el ecosistema y una constante compartida las haría pisarse la
// cookie entre ellas.
const (
	sessionCookieName = "wapp_platform_session"
	csrfCookieName    = "wapp_platform_csrf"
	// enrollmentCodeCookieName es la cookie EFÍMERA que lleva el código de enrolamiento del POST que
	// lo emite al GET que lo muestra (M-10). No es una tercera cookie de sesión: vive 60 s, está
	// acotada a la pantalla del código y el propio GET la borra.
	enrollmentCodeCookieName = "wapp_platform_enrollment_code"
)

// Vidas de las dos cookies. Se declaran aquí y no se dejan al valor por defecto del módulo (1 h la
// de sesión, 12 h la de CSRF) para conservar exactamente lo que la consola servía antes: 24 h.
const (
	sessionCookieMaxAge = 86400
	csrfCookieMaxAge    = 24 * time.Hour
	// La cookie del código no dura una sesión de trabajo: dura lo que tarda el navegador en seguir
	// el 303. Es un tope de seguridad — quien la retira de verdad es el GET que la consume.
	enrollmentCodeCookieMaxAge = 60 * time.Second
)

// sessionCookieOptions es la política de la cookie de sesión de la consola.
func sessionCookieOptions(cfg *config.Config) sharedweb.SessionCookieOptions {
	return sharedweb.SessionCookieOptions{
		Name:     sessionCookieName,
		Secure:   cfg.CookieSecure,
		SameSite: cfg.CookieSameSite,
	}
}

// enrollmentCodeCookieOptions es la política de la cookie efímera del código de enrolamiento. El
// Path se acota a la pantalla destino —y por eso depende del tenant—: fuera de ella el navegador no
// la manda, así que el código no viaja en peticiones que no tienen nada que ver con él. El HttpOnly
// lo fija el módulo SIEMPRE; Secure y SameSite siguen la misma config que la cookie de sesión.
//
// El valor NO se cifra ni se firma, y está razonado en el doc de web.OneTimeCookieOptions: el
// destinatario del código es justo quien tiene la cookie, y el código se le va a pintar en pantalla.
// Lo único que compra la cookie es que el código no pase por la URL.
func enrollmentCodeCookieOptions(cfg *config.Config, tenantID string) sharedweb.OneTimeCookieOptions {
	return sharedweb.OneTimeCookieOptions{
		Name:     enrollmentCodeCookieName,
		Path:     enrollmentCodePath(tenantID),
		MaxAge:   enrollmentCodeCookieMaxAge,
		Secure:   cfg.CookieSecure,
		SameSite: cfg.CookieSameSite,
	}
}

// csrfOptions es la política de la cookie CSRF de la consola. El SameSite=Lax y el HttpOnly no
// aparecen aquí porque el módulo los fija SIEMPRE: el fail-safe CSRF no se degrada aunque la cookie
// de sesión se configure de otra forma.
func csrfOptions(cfg *config.Config) sharedweb.CSRFOptions {
	return sharedweb.CSRFOptions{
		CookieName: csrfCookieName,
		MaxAge:     csrfCookieMaxAge,
		Secure:     cfg.CookieSecure,
	}
}

// unverifiedParser lee el Context Token SIN verificar la firma: quien la valida de verdad es la
// plataforma en cada llamada. Aquí solo se necesita el `exp` (para decidir si la sesión sigue viva o
// toca refrescarla) y el usuario/empresa que las pantallas muestran.
var unverifiedParser = jwt.NewParser()

// parseAccessClaims decodifica los claims del access token. Se queda en la consola a propósito:
// `wapp-shared/web` no importa ninguna librería de JWT —recibe el `exp` ya extraído— y esa frontera
// es lo que lo mantiene en el nivel 0.
func parseAccessClaims(accessToken string) (*sharedjwt.Claims, error) {
	var claims sharedjwt.Claims
	if _, _, err := unverifiedParser.ParseUnverified(accessToken, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// accessExpiry traduce el `exp` de los claims a lo que esperan web.SessionValid y web.RefreshDue.
// Un token sin `exp` devuelve nil, que el módulo trata como sesión inválida y refresco debido.
func accessExpiry(claims *sharedjwt.Claims) *time.Time {
	if claims == nil || claims.ExpiresAt == nil {
		return nil
	}
	exp := claims.ExpiresAt.Time
	return &exp
}
