package adminclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListTenants_BoundsSuccessBody fija el hallazgo #7 de CODE-REVIEW-2026-08-15: los cuerpos de
// ÉXITO se decodificaban sin io.LimitReader (solo el camino de error estaba acotado). Aquí el
// servidor devuelve un cuerpo bien formado pero deliberadamente mayor que maxSuccessBody: con el
// límite en vigor, el decode debe fallar por corte (EOF inesperado) en vez de leer el cuerpo entero
// sin tope.
func TestListTenants_BoundsSuccessBody(t *testing.T) {
	t.Parallel()
	padding := strings.Repeat("a", maxSuccessBody+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"limit":0,"offset":0,"padding":"` + padding + `"}`))
	}))
	defer srv.Close()

	client := NewTenantsClient(NewTransport(srv.URL, 0))
	_, err := client.ListTenants(context.Background(), "tok", 0, 0)
	if err == nil {
		t.Fatal("ListTenants con cuerpo mayor que maxSuccessBody = nil, want error de decode por corte")
	}
}
