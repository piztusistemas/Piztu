package motor

import (
	"os"
	"strings"
	"testing"

	"piztu/internal/config"
)

// TestInventarioTemporalUsaGrupoAula garante que os hosts se escriben baixo o
// grupo real da aula, non baixo un [all] solto: os playbooks apuntan a
// "hosts: aula" e un host que só estivese en [all] non casaría con ese patrón
// (bug detectado ao comprobar que durmir/bloqueo/apagar non facían nada).
func TestInventarioTemporalUsaGrupoAula(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		TmpDir:    dir,
		SSHUser:   "usuario",
		Ansible:   config.Ansible{GrupoAula: "aula"},
		HostsFile: dir + "/hosts_inexistente",
	}

	ruta, err := InventarioTemporal(cfg, []string{"bac01.local"})
	if err != nil {
		t.Fatalf("InventarioTemporal: %v", err)
	}
	defer os.Remove(ruta)

	data, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("lendo inventario xerado: %v", err)
	}
	contido := string(data)

	if !strings.Contains(contido, "[aula]") {
		t.Errorf("o inventario non declara o grupo [aula]:\n%s", contido)
	}
	if strings.Contains(contido, "[all]\n") {
		t.Errorf("o inventario non debería declarar [all] á parte (é implícito):\n%s", contido)
	}
	if !strings.Contains(contido, "bac01.local") {
		t.Errorf("o host non aparece no inventario xerado:\n%s", contido)
	}
}
