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
