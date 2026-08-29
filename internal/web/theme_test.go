package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/EduGoGroup/wapp-shared/ui"
)

// El invariante que estos tests blindan es el que falló en campo: un color LITERAL fijo pintado
// sobre una superficie que SÍ se mueve con `prefers-color-scheme`. El par tiene que viajar entero
// —o los dos literales, o los dos tokenizados—, y aquí se decidió que fueran los dos literales:
// esta consola es MONOTEMA CLARA (ver la cabecera de `static/css/app.css`).
//
// Enunciarlo tal cual —"ningún par mixto"— no es comprobable desde Go: el par se forma en el
// NAVEGADOR, cuando la cascada de cuatro hojas decide qué `color` hereda un elemento y de qué
// ancestro saca el fondo. Haría falta un motor de render. Lo que sí es comprobable, y es
// ESTRICTAMENTE MÁS FUERTE, es la condición que lo hace imposible: que ningún token se mueva.
// Si en modo oscuro ninguna superficie ni ningún texto compartido cambia de valor, no puede
// existir un par mixto, se escriba la plantilla que se escriba.
//
// La lista de tokens a neutralizar NO está copiada aquí: se deriva de la hoja compartida del
// módulo PINADO (`ui.GetCSS`), que es exactamente el byte que el binario sirve. Por eso el día
// que un release de `wapp-shared/ui` añada un token sensible al tema —hay un `--wapp-color-link`
// en camino— este test se pone rojo al subir la versión, en vez de dejar el bloque envejecer en
// silencio y que el token se mueva solo.

var (
	reMediaOscura = regexp.MustCompile(`@media\s*\(\s*prefers-color-scheme\s*:\s*dark\s*\)\s*\{`)
	reRootClaro   = regexp.MustCompile(`(^|\})\s*:root\s*\{`)
	reComentario  = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// sinComentarios quita los bloques /* … */ antes de cualquier análisis: las hojas de wApp llevan
// prosa larga dentro de los comentarios y un `{` suelto ahí dentro descuadraría el conteo de llaves.
func sinComentarios(css string) string {
	return reComentario.ReplaceAllString(css, "")
}

// cuerpoTrasLlave devuelve el texto entre la llave que abre en `desde` y su llave de cierre pareja.
func cuerpoTrasLlave(css string, desde int) (string, bool) {
	prof := 1
	for i := desde; i < len(css); i++ {
		switch css[i] {
		case '{':
			prof++
		case '}':
			prof--
			if prof == 0 {
				return css[desde:i], true
			}
		}
	}
	return "", false
}

// declaraciones parsea las parejas `propiedad: valor;` de un cuerpo de regla, normalizando el valor
// a mayúsculas para que `#ffffff` y `#FFFFFF` no cuenten como distintos.
func declaraciones(cuerpo string) map[string]string {
	out := map[string]string{}
	for _, decl := range strings.Split(cuerpo, ";") {
		i := strings.Index(decl, ":")
		if i < 0 {
			continue
		}
		nombre := strings.TrimSpace(decl[:i])
		valor := strings.TrimSpace(decl[i+1:])
		if nombre == "" || valor == "" || strings.ContainsAny(nombre, "{}") {
			continue
		}
		out[nombre] = strings.ToUpper(valor)
	}
	return out
}

// tokensDelBloqueOscuro devuelve las custom properties declaradas dentro del `@media
// (prefers-color-scheme: dark)` de una hoja.
func tokensDelBloqueOscuro(t *testing.T, hoja, nombre string) map[string]string {
	t.Helper()

	css := sinComentarios(hoja)
	m := reMediaOscura.FindStringIndex(css)
	if m == nil {
		return nil
	}
	cuerpoMedia, ok := cuerpoTrasLlave(css, m[1])
	if !ok {
		t.Fatalf("%s: el @media de modo oscuro no cierra su llave", nombre)
	}
	// Dentro de la media hay una regla (`:root…{ … }`); nos quedamos con su cuerpo.
	j := strings.Index(cuerpoMedia, "{")
	if j < 0 {
		t.Fatalf("%s: el @media de modo oscuro no contiene ninguna regla", nombre)
	}
	cuerpoRegla, ok := cuerpoTrasLlave(cuerpoMedia, j+1)
	if !ok {
		t.Fatalf("%s: la regla dentro del @media oscuro no cierra su llave", nombre)
	}
	return declaraciones(cuerpoRegla)
}

// tokensClaros devuelve las custom properties del `:root` de nivel superior (el tema CLARO).
func tokensClaros(t *testing.T, hoja, nombre string) map[string]string {
	t.Helper()

	css := sinComentarios(hoja)
	m := reRootClaro.FindStringIndex(css)
	if m == nil {
		t.Fatalf("%s: no se encontró el bloque `:root` del tema claro", nombre)
	}
	cuerpo, ok := cuerpoTrasLlave(css, m[1])
	if !ok {
		t.Fatalf("%s: el bloque `:root` no cierra su llave", nombre)
	}
	return declaraciones(cuerpo)
}

// TestCSS_TokensSensiblesAlTemaNeutralizados falla si `app.css` deja SIN neutralizar —o neutraliza
// con el valor equivocado— cualquier token que la hoja compartida mueva en modo oscuro.
//
// Sin este lazo, el modo oscuro de la consola quedaba sostenido por un accidente de orden de carga:
// las reglas duplicadas de `app.css` (`.wapp-card`, `.wapp-title`, `.wapp-field__label`…) tapaban
// los tokens oscuros solo porque la hoja se carga la última. Borrar seis líneas duplicadas —cosa que
// la propia hoja compartida invita a hacer— destapaba 21 fallos AA medidos en Chrome, entre ellos un
// `#1C1B1F` sobre `#161D1B` = 1,00:1: texto invisible.
func TestCSS_TokensSensiblesAlTemaNeutralizados(t *testing.T) {
	t.Parallel()

	compartida, err := ui.GetCSS("wapp-tokens.css")
	if err != nil {
		t.Fatalf("leer wapp-tokens.css del módulo compartido: %v", err)
	}

	claros := tokensClaros(t, string(compartida), "wapp-tokens.css")
	oscuros := tokensDelBloqueOscuro(t, string(compartida), "wapp-tokens.css")
	if len(oscuros) == 0 {
		t.Fatal("wapp-tokens.css ya no trae bloque de modo oscuro: si de verdad desapareció, este " +
			"test y el bloque de neutralización de app.css sobran; mientras tanto, asume que el " +
			"parseo se rompió y NO lo des por bueno")
	}

	neutralizados := tokensDelBloqueOscuro(t, string(appCSS), "app.css")
	if len(neutralizados) == 0 {
		t.Fatal("app.css no declara ningún token dentro de su `@media (prefers-color-scheme: dark)`: " +
			"la consola volvió a heredar el tema oscuro de la hoja compartida y sus 48 colores " +
			"literales se pintarán sobre superficies que se mueven")
	}

	var sensibles int
	for token, valorOscuro := range oscuros {
		valorClaro, existe := claros[token]
		if !existe {
			t.Errorf("`%s` se define en el bloque OSCURO de wapp-tokens.css y no en el claro: no hay "+
				"valor claro con el que neutralizarlo (es el defecto del token que existe en un tema "+
				"y no en el otro)", token)
			continue
		}
		if valorClaro == valorOscuro {
			continue // declarado en los dos bloques con el mismo valor: no se mueve, nada que hacer
		}
		sensibles++

		puesto, ok := neutralizados[token]
		if !ok {
			t.Errorf("`%s` se MUEVE con el tema (claro %s → oscuro %s) y app.css no lo neutraliza: "+
				"en modo oscuro esa superficie/texto cambiará bajo los literales de la consola. "+
				"Añádelo al bloque `@media (prefers-color-scheme: dark)` de app.css con su valor CLARO",
				token, valorClaro, valorOscuro)
			continue
		}
		if puesto != valorClaro {
			t.Errorf("app.css neutraliza `%s` con %s, pero el valor CLARO de wapp-tokens.css es %s: "+
				"la consola es monotema clara, así que el valor tiene que ser el del tema claro",
				token, puesto, valorClaro)
		}
	}

	if sensibles == 0 {
		t.Fatal("no se detectó NINGÚN token sensible al tema en wapp-tokens.css: el parseo está roto " +
			"y este test estaría pasando sin comprobar nada")
	}

	// El bloque de app.css es SOLO para neutralizar. Si alguien mete ahí una regla de verdad de modo
	// oscuro, la consola deja de ser monotema por la puerta de atrás y el par mixto vuelve.
	for token, valor := range neutralizados {
		if !strings.HasPrefix(token, "--") {
			t.Errorf("el `@media (prefers-color-scheme: dark)` de app.css declara `%s: %s`, que no es "+
				"una custom property: ese bloque solo puede NEUTRALIZAR tokens compartidos. Si la "+
				"consola va a tener modo oscuro de verdad, es una decisión de diseño y hay que "+
				"tokenizar sus 48 literales, no colar una regla suelta aquí", token, valor)
			continue
		}
		if _, esCompartido := oscuros[token]; !esCompartido {
			t.Errorf("app.css neutraliza `%s`, que la hoja compartida NO mueve en oscuro: sobra, y "+
				"un día tapará un valor que sí importa", token)
		}
	}
}

// TestCSS_ColorSchemeDeclaradoLight fija la mitad que la neutralización de tokens no cubre: los
// widgets que pinta el NAVEGADOR (controles de formulario, barras de scroll, autofill). No sustituye
// al bloque de tokens —`color-scheme: light` no impide que `prefers-color-scheme` case, y por eso los
// tokens compartidos se movían igual—, pero sin él los `<select>` y las barras salen oscuros dentro
// de una consola clara.
func TestCSS_ColorSchemeDeclaradoLight(t *testing.T) {
	t.Parallel()

	css := sinComentarios(string(appCSS))
	m := reRootClaro.FindStringIndex(css)
	if m == nil {
		t.Fatal("app.css no declara ningún bloque `:root`")
	}
	cuerpo, ok := cuerpoTrasLlave(css, m[1])
	if !ok {
		t.Fatal("app.css: el bloque `:root` no cierra su llave")
	}
	if got := declaraciones(cuerpo)["color-scheme"]; got != "LIGHT" {
		t.Errorf("app.css declara `color-scheme: %q` en :root, se esperaba `light`: esta consola es "+
			"monotema clara y sin esa declaración el navegador pinta sus propios widgets en oscuro", got)
	}
}
