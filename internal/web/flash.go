package web

import sharedweb "github.com/EduGoGroup/wapp-shared/web"

// flash.go es el VOCABULARIO de esta consola: los códigos estables que sus handlers ponen en
// `?error=`/`?success=` al redirigir, y el texto en español que ve el operador (A-08, M-11).
//
// El MECANISMO —que el texto salga siempre de la tabla y nunca del query string ni del upstream, y
// que un código desconocido caiga al genérico— vive en `wapp-shared/web`.FlashCatalog. Los códigos
// no suben: dependen de las pantallas de cada consola.
var (
	// El fallback vacío cae a web.DefaultFlashFallback, que es literalmente el mismo mensaje
	// genérico que esta consola servía antes ("Ocurrió un error inesperado.").
	flashErrors = sharedweb.NewFlashCatalog("", map[string]string{
		"missing_fields":          "Selecciona empresa y rol antes de aprobar.",
		"missing_systems":         "Selecciona al menos un sistema (BFF o Edge) antes de aprobar.",
		"approve_failed":          "No se pudo aprobar la solicitud. Intenta de nuevo.",
		"approve_partial":         "La aprobación pudo quedar aplicada solo parcialmente en el servidor. Verifica el estado del usuario antes de repetir la acción.",
		"approve_partial_skipped": "La aprobación SÍ se aplicó: la empresa y el rol quedaron guardados. Los permisos de aplicaciones (BFF/Edge) NO se tocaron a propósito, para no revocarle sin querer un acceso que el usuario ya tenía. Reintentar no lo va a arreglar: revisa el estado de este usuario antes de continuar.",
		"missing_reason":          "Indica un motivo para rechazar la solicitud.",
		"reject_failed":           "No se pudo rechazar la solicitud. Intenta de nuevo.",
		"tenant_unreachable":      "No se pudo verificar la empresa objetivo. Intenta de nuevo.",
		"slug_mismatch":           "El slug escrito no coincide. El corte no se ejecutó.",
		"revoke_failed":           "No se pudo cortar el servicio. Verifica el estado actual antes de reintentar.",
		"restore_failed":          "No se pudo restaurar el servicio. Intenta de nuevo.",
		"code_failed":             "No se pudo generar el código de enrolamiento.",
		"code_lost":               "El código SÍ se emitió, pero no se pudo mostrar. Ese código queda inservible: emite uno nuevo.",
	})

	flashSuccesses = sharedweb.NewFlashCatalog("Acción completada.", map[string]string{
		"approved": "Solicitud aprobada. El acceso ya está activo.",
		"rejected": "Solicitud rechazada.",
		"revoked":  "Servicio cortado correctamente.",
		"restored": "Servicio restaurado correctamente.",
	})
)

// flashError traduce un código de error al mensaje que ve el operador.
func flashError(code string) string { return flashErrors.Message(code) }

// flashSuccess traduce un código de éxito al mensaje que ve el operador.
func flashSuccess(code string) string { return flashSuccesses.Message(code) }
