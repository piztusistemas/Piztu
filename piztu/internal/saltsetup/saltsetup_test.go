package saltsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O drop-in de file_roots ten que quedar en YAML válido e coas raíces na orde
// correcta: cfg.SaltDir primeiro (prioridade sobre un módulo que repita nome).
func TestRaicesValidas(t *testing.T) {
	dir := t.TempDir()
	modulos := filepath.Join(dir, "modulos")
	if err := os.MkdirAll(modulos, 0o755); err != nil {
		t.Fatal(err)
	}
	saltDir := filepath.Join(dir, "salt")

	got := raicesValidas(saltDir, []string{
		modulos,
		modulos,                          // repetido
		"",                               // baleiro
		"relativa/salt",                  // non absoluta
		filepath.Join(dir, "non-existe"), // non existe
	})

	want := []string{saltDir, modulos}
	if len(got) != len(want) {
		t.Fatalf("raíces = %v, quería %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("raíz %d = %q, quería %q", i, got[i], want[i])
		}
	}
}

// Unha ruta con espazos ten que saír entrecomiñada, senón o YAML do drop-in
// rompe e o salt-master non arranca.
func TestFileRootsScriptEntrecomiña(t *testing.T) {
	dir := t.TempDir()
	conEspazos := filepath.Join(dir, "cartafol con espazos")
	if err := os.MkdirAll(conEspazos, 0o755); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	for _, r := range raicesValidas(filepath.Join(dir, "salt"), []string{conEspazos}) {
		b.WriteString("    - \"" + r + "\"\n")
	}
	if !strings.Contains(b.String(), "\""+conEspazos+"\"") {
		t.Errorf("a ruta con espazos non quedou entrecomiñada: %s", b.String())
	}
}

// O patrón de autosign decide que máquinas entran soas na aula: unha liña
// colada nel abriría a porta a calquera minion.
func TestPatronsValidos(t *testing.T) {
	got := patronsValidos([]string{
		"tux*", "tux*", // repetido
		"", "  ", // baleiros
		"*",            // aceptaría calquera minion
		"tux01\n*",     // inxección dunha liña nova
		"tux;rm -rf /", // caracteres non válidos
		"aula-2.local", // válido
	})
	want := []string{"tux*", "aula-2.local"}
	if len(got) != len(want) {
		t.Fatalf("patróns = %v, quería %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("patrón %d = %q, quería %q", i, got[i], want[i])
		}
	}
}

// O bug que motivou todo isto: o equipo responde, pero co nome longo.
func TestDesaxusteDeNomesDetectaSufixo(t *testing.T) {
	r := desaxusteDeNomes([]string{"tux01"}, []string{"tux01.local"})
	if len(r) == 0 {
		t.Fatal("non detectou o desaxuste tux01 / tux01.local")
	}
	if !strings.Contains(r[0].Detalle, "tux01.local") || r[0].Ok {
		t.Errorf("comprobación inesperada: %+v", r[0])
	}
}

// A saída de `salt` trae texto antes do JSON cando algo falla.
func TestIdsQueRespondenConRuidoDiante(t *testing.T) {
	saida := "ERROR: No return received\nNo minions matched the target.\n" +
		`{"tux01.local": true, "tux02.local": false}`
	got := idsQueResponden(saida)
	if len(got) != 1 || got[0] != "tux01.local" {
		t.Errorf("ids = %v, quería [tux01.local]", got)
	}
}

// Un enderezo gardado que segue resolvendo respéctase; un que xa non, non.
func TestEnderezoMasterValidado(t *testing.T) {
	if got := EnderezoMasterValidado("localhost", ".local"); got != "localhost" {
		t.Errorf("un enderezo que resolve debe respectarse: got %q", got)
	}
	// .invalid está reservado precisamente para non resolver nunca (RFC 2606).
	if got := EnderezoMasterValidado("non-existe.invalid", ".local"); got == "non-existe.invalid" {
		t.Error("un enderezo que non resolve non debe manterse")
	}
	if EnderezoMasterValidado("", ".local") == "" {
		t.Error("sen valor gardado ten que deducir algo")
	}
}
