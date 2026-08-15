package web

import (
	"testing"
	"time"
)

// TestRefreshGroup_DoSurvivesPanicInFn fija el hallazgo #1 de CODE-REVIEW-2026-08-15: sin el defer de
// limpieza en refreshGroup.do, un pánico dentro de fn() dejaba c.wg.Done() y el delete(g.m, key) sin
// ejecutar, así que toda petición POSTERIOR con el MISMO refresh token se quedaba colgada para siempre
// en c.wg.Wait() (sin timeout, ocupando una goroutine). Aquí se provoca el pánico a propósito y se
// verifica que una segunda llamada con la misma key no cuelga, usando un timeout para que el test
// falle en vez de colgar toda la suite si la regresión vuelve.
func TestRefreshGroup_DoSurvivesPanicInFn(t *testing.T) {
	t.Parallel()
	g := newRefreshGroup()

	func() {
		defer func() { _ = recover() }()
		_, _ = g.do("rt-panic", func() (any, error) {
			panic("fn entra en pánico a propósito")
		})
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		val, err := g.do("rt-panic", func() (any, error) {
			return "ok-tras-el-panico", nil
		})
		if err != nil {
			t.Errorf("segunda llamada con la misma key devolvió error = %v, want nil", err)
		}
		if s, _ := val.(string); s != "ok-tras-el-panico" {
			t.Errorf("segunda llamada con la misma key devolvió val = %v, want %q", val, "ok-tras-el-panico")
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("la segunda llamada con la misma key se quedó colgada: el pánico en fn() dejó " +
			"c.wg.Done()/delete(g.m, key) sin ejecutar")
	}

	g.mu.Lock()
	_, stillTracked := g.m["rt-panic"]
	g.mu.Unlock()
	if stillTracked {
		t.Error("la entrada de la key debía borrarse del mapa tras el pánico")
	}
}
