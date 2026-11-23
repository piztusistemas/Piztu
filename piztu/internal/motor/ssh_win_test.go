package motor

import (
	"strings"
	"testing"
)

// Os win_*.ps1 teñen que ir embebidos (recursos/salt): se faltasen, o motor
// SSH mandaría un script baleiro aos equipos Windows.
func TestScriptsWindowsEmbebidos(t *testing.T) {
	for _, n := range []string{"win_bloqueo.ps1", "win_desbloqueo.ps1", "win_aviso_ruido.ps1"} {
		if strings.TrimSpace(psScriptSalt(n)) == "" {
			t.Errorf("%s non está embebido en recursos/salt", n)
		}
	}
}

func TestPsBloqueoOpcions(t *testing.T) {
	so := psBloqueo(map[string]bool{"internet": true})
	if !strings.Contains(so, "name=PIZTU_BLOQUEO dir=out") || strings.Contains(so, "PiztuBloqueo") {
		t.Errorf("só internet: debería haber regra de firewall e non overlay:\n%s", so)
	}
	todo := psBloqueo(map[string]bool{"rato": true, "ssh": true})
	if !strings.Contains(todo, "PIZTU_BLOQUEO_SSH dir=out") || !strings.Contains(todo, "PiztuBloqueo") {
		t.Errorf("rato+ssh: faltan a regra SSH ou o overlay")
	}
}

// Desde a 26.12.2 a conta da aula en Windows é estándar: os scripts que
// precisan administrador teñen que levar a comprobación diante (senón fallan
// en silencio) e o de comprobar o bloqueo, que só le, non.
func TestSoAdmin(t *testing.T) {
	if soAdmin("") != "" || soAdmin("  \n") != "" {
		t.Error("un script baleiro (sen equivalente Windows) ten que seguir baleiro")
	}
	if !strings.HasPrefix(soAdmin(psApagar), psSoAdmin) {
		t.Error("apagar non leva a comprobación de administrador")
	}
	if !strings.HasPrefix(psBloqueo(map[string]bool{"internet": true}), psSoAdmin) {
		t.Error("o bloqueo non leva a comprobación de administrador")
	}
	if strings.Contains(psComprobarBloqueo, marcaSenAdmin) {
		t.Error("comprobar o bloqueo non precisa administrador")
	}
	if m := erroWindows("usuario", "Process exited with status 3: "+marcaSenAdmin); !strings.Contains(m, "motor Salt") {
		t.Errorf("a mensaxe sen administrador non indica o motor Salt: %q", m)
	}
	if m := erroWindows("usuario", "conexión: timeout"); m != "conexión: timeout" {
		t.Errorf("os outros erros teñen que pasar tal cal: %q", m)
	}
}
