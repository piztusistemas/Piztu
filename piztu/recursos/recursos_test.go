package recursos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesplegarEstadosSalt cobre o fallo real "Estado Salt non atopado:
// bloqueoTotal.sls": en macOS base_dir arranca baleiro, e se os .sls non se
// despregan alí, calquera acción dos motores Salt/Salt-SSH falla aínda que os
// ficheiros existan no repositorio.
func TestDesplegarEstadosSalt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "salt")

	n, err := DesplegarEstadosSalt(dir)
	if err != nil {
		t.Fatalf("DesplegarEstadosSalt: %v", err)
	}
	if n == 0 {
		t.Fatal("non se despregou ningún estado")
	}

	// Os estados que os botóns da barra chaman por nome teñen que estar todos.
	for _, sls := range []string{
		"bloqueoTotal.sls", "desbloquearAula.sls", "durmirAula.sls",
		"apagar_aula.sls", "avisoRuido.sls", "comprobar_bloqueo.sls",
	} {
		if _, err := os.Stat(filepath.Join(dir, sls)); err != nil {
			t.Errorf("falta %s en %s", sls, dir)
		}
	}
}

// TestDesplegarNonPisaOEditado: o profesor pode editar os estados, así que un
// segundo arranque non debe sobrescribirlle os cambios.
func TestDesplegarNonPisaOEditado(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "salt")
	if _, err := DesplegarEstadosSalt(dir); err != nil {
		t.Fatalf("primeiro despregue: %v", err)
	}

	editado := filepath.Join(dir, "bloqueoTotal.sls")
	if err := os.WriteFile(editado, []byte("# editado polo profesor\n"), 0o644); err != nil {
		t.Fatalf("editando: %v", err)
	}

	n, err := DesplegarEstadosSalt(dir)
	if err != nil {
		t.Fatalf("segundo despregue: %v", err)
	}
	if n != 0 {
		t.Errorf("o segundo despregue escribiu %d ficheiros; debería respectar o existente", n)
	}
	data, _ := os.ReadFile(editado)
	if string(data) != "# editado polo profesor\n" {
		t.Error("sobrescribiuse un estado editado polo profesor")
	}
}

// TestDesplegarIdiomas cobre o fallo "a pantalla Sobre Piztu amosa
// sobre.version, sobre.autor…": en macOS a app lánzase co directorio de
// traballo en "/" e base_dir arranca baleiro, así que sen despregar os idiomas
// non se atopaba ningunha tradución.
func TestDesplegarIdiomas(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "idiomas")

	n, err := DesplegarIdiomas(dir)
	if err != nil {
		t.Fatalf("DesplegarIdiomas: %v", err)
	}
	if n == 0 {
		t.Fatal("non se despregou ningún ficheiro de idioma")
	}

	for _, lang := range []string{"gl.lang", "es.lang", "en.lang", "pt.lang"} {
		if _, err := os.Stat(filepath.Join(dir, lang)); err != nil {
			t.Errorf("falta %s en %s", lang, dir)
		}
	}

	// As claves que amosaba en cru teñen que estar de verdade no ficheiro.
	data, err := os.ReadFile(filepath.Join(dir, "gl.lang"))
	if err != nil {
		t.Fatal(err)
	}
	for _, clave := range []string{"sobre.version", "sobre.autor", "sobre.licdesc"} {
		if !strings.Contains(string(data), clave+" =") {
			t.Errorf("gl.lang non define %q", clave)
		}
	}
}
