package web

// flash.go traduce los códigos estables que los handlers ponen en `?error=`/`?success=` a mensajes fijos
// en español para el operador (A-08, M-11). Nunca se refleja texto crudo del upstream en la URL ni en la
// pantalla: el detalle real de un fallo va al log (slog), identificado por el mismo código.

// flashError traduce un código de error a un mensaje fijo. Un código no reconocido (o vacío) devuelve un
// mensaje genérico — nunca el texto original, que podría venir de un query string manipulado.
func flashError(code string) string {
	switch code {
	case "":
		return ""
	case "missing_fields":
		return "Selecciona empresa y rol antes de aprobar."
	case "missing_systems":
		return "Selecciona al menos un sistema (BFF o Edge) antes de aprobar."
	case "approve_failed":
		return "No se pudo aprobar la solicitud. Intenta de nuevo."
	case "approve_partial":
		return "La aprobación pudo quedar aplicada solo parcialmente en el servidor. Verifica el estado del usuario antes de repetir la acción."
	case "approve_partial_skipped":
		return "La aprobación SÍ se aplicó: la empresa y el rol quedaron guardados. Los permisos de aplicaciones (BFF/Edge) NO se tocaron a propósito, para no revocarle sin querer un acceso que el usuario ya tenía. Reintentar no lo va a arreglar: revisa el estado de este usuario antes de continuar."
	case "missing_reason":
		return "Indica un motivo para rechazar la solicitud."
	case "reject_failed":
		return "No se pudo rechazar la solicitud. Intenta de nuevo."
	case "tenant_unreachable":
		return "No se pudo verificar la empresa objetivo. Intenta de nuevo."
	case "slug_mismatch":
		return "El slug escrito no coincide. El corte no se ejecutó."
	case "revoke_failed":
		return "No se pudo cortar el servicio. Verifica el estado actual antes de reintentar."
	case "restore_failed":
		return "No se pudo restaurar el servicio. Intenta de nuevo."
	case "code_failed":
		return "No se pudo generar el código de enrolamiento."
	default:
		return "Ocurrió un error inesperado."
	}
}

// flashSuccess traduce un código de éxito a un mensaje fijo.
func flashSuccess(code string) string {
	switch code {
	case "":
		return ""
	case "approved":
		return "Solicitud aprobada. El acceso ya está activo."
	case "rejected":
		return "Solicitud rechazada."
	case "revoked":
		return "Servicio cortado correctamente."
	case "restored":
		return "Servicio restaurado correctamente."
	default:
		return "Acción completada."
	}
}
