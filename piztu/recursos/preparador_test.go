package recursos

import (
	"strings"
	"testing"
)

// App.PrepararClientesWindows substitúe estas marcas: se o .ps1 deixase de
// levalas, o preparador xerado sairía sen servidor ou sen clave.
func TestPreparadorWindowsMarcas(t *testing.T) {
	ps1, bat, err := PreparadorWindows()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"@@SERVIDOR@@", "@@CLAVE_PUBLICA@@", "@@USUARIO@@"} {
		if !strings.Contains(string(ps1), m) {
			t.Errorf("falta a marca %s no preparador", m)
		}
	}
	if !strings.Contains(string(bat), "preparar_cliente_windows.ps1") {
		t.Error("o .bat non lanza o .ps1")
	}
}
