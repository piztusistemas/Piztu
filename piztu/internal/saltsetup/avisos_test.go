package saltsetup

import (
	"strings"
	"testing"
)

func TestAvisosMinions(t *testing.T) {
	hosts := []string{"bac01.local", "bac02.local", "bac03.local", "bac04.local"}
	acendidos := map[string]bool{"bac01.local": true, "bac02.local": true, "bac03.local": true}
	// bac01 contesta; bac02 contesta co id curto; bac03 acendido e mudo; bac04 apagado.
	av := avisosMinions(hosts, acendidos, []string{"bac01.local", "bac02"})

	if av["bac01.local"] != "" {
		t.Errorf("bac01 contesta, non debería ter aviso: %q", av["bac01.local"])
	}
	if !strings.Contains(av["bac02.local"], `"bac02"`) {
		t.Errorf("bac02 debería avisar do id %q: %q", "bac02", av["bac02.local"])
	}
	if !strings.Contains(av["bac03.local"], "non contesta") {
		t.Errorf("bac03 acendido e mudo debería ter aviso: %q", av["bac03.local"])
	}
	if av["bac04.local"] != "" {
		t.Errorf("bac04 apagado non debería ter aviso: %q", av["bac04.local"])
	}
	if len(av) != len(hosts) {
		t.Errorf("debería devolver unha entrada por equipo (para limpar avisos vellos): %d", len(av))
	}
}
