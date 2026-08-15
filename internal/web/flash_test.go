package web

import "testing"

// TestFlashError_KnownCodesMapToFixedMessages cubre A-08/M-11: cada código estable que un handler puede
// poner en ?error= debe traducirse a un mensaje fijo no vacío, y nunca al código crudo (que en el caso de
// M-11 podría, antes de este fix, ser texto arbitrario del upstream).
func TestFlashError_KnownCodesMapToFixedMessages(t *testing.T) {
	t.Parallel()

	codes := []string{
		"missing_fields", "missing_systems", "approve_failed", "approve_partial",
		"missing_reason", "reject_failed", "tenant_unreachable", "slug_mismatch",
		"revoke_failed", "restore_failed", "code_failed",
	}
	for _, code := range codes {
		msg := flashError(code)
		if msg == "" {
			t.Errorf("flashError(%q) = \"\", quiere un mensaje fijo no vacío", code)
		}
		if msg == code {
			t.Errorf("flashError(%q) devolvió el código crudo en vez de traducirlo", code)
		}
	}
}

// TestFlashError_UnknownCodeNeverReflectsRawInput es la defensa de M-11: un código no reconocido (por
// ejemplo, texto arbitrario inyectado en la query string) nunca debe reflejarse tal cual.
func TestFlashError_UnknownCodeNeverReflectsRawInput(t *testing.T) {
	t.Parallel()

	raw := "algo&arbitrario#inyectado"
	if got := flashError(raw); got == raw {
		t.Fatalf("flashError no debe reflejar texto crudo: got %q", got)
	}
	if got := flashError(""); got != "" {
		t.Fatalf("flashError(\"\") = %q, want \"\" (sin flash cuando no hay ?error=)", got)
	}
}

func TestFlashSuccess_KnownCodesMapToFixedMessages(t *testing.T) {
	t.Parallel()

	codes := []string{"approved", "rejected", "revoked", "restored"}
	for _, code := range codes {
		msg := flashSuccess(code)
		if msg == "" {
			t.Errorf("flashSuccess(%q) = \"\", quiere un mensaje fijo no vacío", code)
		}
		if msg == code {
			t.Errorf("flashSuccess(%q) devolvió el código crudo en vez de traducirlo", code)
		}
	}
	if got := flashSuccess(""); got != "" {
		t.Fatalf("flashSuccess(\"\") = %q, want \"\"", got)
	}
}
