package modulos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// axustesFalsos imita o almacén chave-valor de internal/db sen base de datos.
type axustesFalsos struct{ datos map[string]string }

func novosAxustes() *axustesFalsos { return &axustesFalsos{datos: map[string]string{}} }

func (a *axustesFalsos) LerAxuste(clave, defecto string) string {
	if v, hai := a.datos[clave]; hai {
		return v
	}
	return defecto
}

func (a *axustesFalsos) GardarAxuste(clave, valor string) error {
	a.datos[clave] = valor
	return nil
}

// TestTodoApagadoSenEstadoGardado: unha instalación nova non debe activar
// micrófono nin transferencia de ficheiros sen que o profesor o decida. Sen
// nada gardado, todo ten que seguir apagado.
func TestTodoApagadoSenEstadoGardado(t *testing.T) {
	ax := novosAxustes()
	for _, m := range Internos() {
		if Activo(ax, m) {
			t.Errorf("o módulo %q non debería vir activo por defecto", m.ID)
		}
	}
}

func TestPersistenciaIdaEVolta(t *testing.T) {
	ax := novosAxustes()
	dir := t.TempDir()

	if err := Set(ax, Ruido, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := Set(ax, Ficheiros, false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ActivoPorID(ax, dir, Ficheiros) {
		t.Error("tras apagalo, o módulo debería estar inactivo")
	}
	// Os demais non se ven afectados.
	if !ActivoPorID(ax, dir, Ruido) {
		t.Error("apagar ficheiros non debe apagar ruído")
	}

	if err := Set(ax, Ficheiros, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !ActivoPorID(ax, dir, Ficheiros) {
		t.Error("tras reactivalo, o módulo debería estar activo")
	}
}

// TestIDDescoñecidoContaComoInactivo: se piztu non sabe que é un módulo, non
// debe deixalo actuar.
func TestIDDescoñecidoContaComoInactivo(t *testing.T) {
	if ActivoPorID(novosAxustes(), t.TempDir(), "apeiro-inventado") {
		t.Error("un id descoñecido non debería contar como activo")
	}
}

func escribirModulo(t *testing.T, raiz, nome, contido string) {
	t.Helper()
	dir := filepath.Join(raiz, nome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modulo.json"), []byte(contido), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExternosLeOManifest(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "arado", `{"id":"arado","nome":"Arado","icona":"🪏","executable":"Arado.app"}`)

	ext := Externos(raiz)
	if len(ext) != 1 {
		t.Fatalf("agardábase 1 módulo externo, obtivéronse %d", len(ext))
	}
	m := ext[0]
	if m.ID != "arado" || m.Nome != "Arado" || m.Executable != "Arado.app" {
		t.Errorf("manifest mal lido: %+v", m)
	}
	if m.Interno {
		t.Error("un módulo do cartafol non pode marcarse como interno")
	}
	if m.Dir != filepath.Join(raiz, "arado") {
		t.Errorf("Dir = %q; agardábase o cartafol do módulo", m.Dir)
	}
}

// TestExternosIgnoraOsRotos é o punto importante deste paquete: un módulo de
// terceiros mal empaquetado NON pode deixar ao profesor sen poder acender a
// aula. Todo o que non se entenda descártase en silencio.
func TestExternosIgnoraOsRotos(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "corrupto", `{esto non é json`)
	escribirModulo(t, raiz, "sen-executable", `{"id":"x","nome":"X"}`)
	escribirModulo(t, raiz, "bo", `{"id":"bo","nome":"Bo","executable":"Bo.app"}`)
	// Un cartafol sen manifest ningún.
	if err := os.MkdirAll(filepath.Join(raiz, "baleiro"), 0o755); err != nil {
		t.Fatal(err)
	}

	ext := Externos(raiz)
	if len(ext) != 1 || ext[0].ID != "bo" {
		t.Errorf("só debería sobrevivir o módulo válido; obtívose %+v", ext)
	}
}

func TestExternosSenCartafolNonFalla(t *testing.T) {
	if ext := Externos(filepath.Join(t.TempDir(), "non-existe")); ext != nil {
		t.Errorf("sen cartafol de módulos debería devolver nada, non %+v", ext)
	}
}

// TestExternoNonPodeSuplantarUnInterno: se un módulo do cartafol reutiliza o id
// de "ruido", podería substituír unha función propia de piztu.
func TestExternoNonPodeSuplantarUnInterno(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "falso", `{"id":"ruido","nome":"Ruído falso","executable":"Falso.app"}`)

	for _, m := range Todos(raiz) {
		if m.ID == Ruido && !m.Interno {
			t.Fatal("un módulo externo suplantou o módulo interno de ruído")
		}
	}
}

// TestPrepararCreaOCartafol: sen isto o cartafol non existía ata que alguén o
// creaba á man, e o profesor non tiña onde soltar un apeiro novo nin de onde
// deducir o formato do manifest.
func TestPrepararCreaOCartafol(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "modulos")

	if err := Preparar(dir); err != nil {
		t.Fatalf("Preparar: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("non se creou o cartafol: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "LEEME.md"))
	if err != nil {
		t.Fatalf("non se escribiu o LEEME.md: %v", err)
	}
	// Ten que explicar o mínimo para poder escribir un manifest.
	for _, agardado := range []string{"modulo.json", "executable", "id"} {
		if !strings.Contains(string(data), agardado) {
			t.Errorf("o LEEME.md non menciona %q", agardado)
		}
	}
}

// TestPrepararNonPisaOLeemeEditado: o profesor pode anotar cousas aí.
func TestPrepararNonPisaOLeemeEditado(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "modulos")
	if err := Preparar(dir); err != nil {
		t.Fatalf("Preparar: %v", err)
	}
	leeme := filepath.Join(dir, "LEEME.md")
	if err := os.WriteFile(leeme, []byte("as miñas notas\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Preparar(dir); err != nil {
		t.Fatalf("segundo Preparar: %v", err)
	}
	data, _ := os.ReadFile(leeme)
	if string(data) != "as miñas notas\n" {
		t.Error("sobrescribiuse o LEEME.md editado polo profesor")
	}
}

// TestExternoNonSeAcendeSo: un apeiro recén soltado no cartafol detéctase e
// ofrécese, pero non aparece na barra ata que o profesor o activa. Un módulo de
// terceiros non debe colarse na interface só por aparecer nun cartafol.
func TestExternoNonSeAcendeSo(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "cisterna", `{"id":"cisterna","nome":"Cisterna","executable":"c.sh"}`)
	ax := novosAxustes()

	if ActivoPorID(ax, raiz, "cisterna") {
		t.Error("un módulo externo recén detectado non debería estar activo")
	}
	// Os internos tampouco veñen acendidos por defecto: hai que activalos.
	if ActivoPorID(ax, raiz, Ruido) {
		t.Error("os módulos internos non deben vir activos por defecto")
	}

	if err := Set(ax, "cisterna", true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !ActivoPorID(ax, raiz, "cisterna") {
		t.Error("tras activalo debería quedar activo")
	}
}

// TestAccionsNonPodenPisarAsDePiztu é a garantía importante da API: un apeiro de
// terceiros non pode redefinir "apagar" ou "bloqueo" e cambiar o que fan os
// botóns de sempre.
func TestAccionsNonPodenPisarAsDePiztu(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "malo", `{"id":"malo","executable":"m.sh","accions":[
		{"id":"apagar","etiqueta":"Apagar (secuestrada)","script":"meu.sh"},
		{"id":"bloqueo","etiqueta":"Bloqueo (secuestrado)","script":"meu.sh"},
		{"id":"baleirar","etiqueta":"Baleirar","script":"baleirar.sh"}
	]}`)

	ext := Externos(raiz)
	if len(ext) != 1 {
		t.Fatalf("agardábase 1 módulo, obtivéronse %d", len(ext))
	}
	if len(ext[0].Accions) != 1 || ext[0].Accions[0].ID != "baleirar" {
		t.Fatalf("só debería sobrevivir a acción propia; obtívose %+v", ext[0].Accions)
	}
}

// TestAccionsIgnoraAsIncompletas: unha acción sen implementación en ningún motor
// non se pode executar, así que non se declara.
func TestAccionsIgnoraAsIncompletas(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "m", `{"id":"m","executable":"m.sh","accions":[
		{"id":"sen-nada","etiqueta":"Sen implementación"},
		{"etiqueta":"Sen id","script":"x.sh"},
		{"id":"boa","script":"boa.sh"},
		{"id":"boa","script":"repetida.sh"}
	]}`)

	acc := Externos(raiz)[0].Accions
	if len(acc) != 1 || acc[0].ID != "boa" {
		t.Fatalf("esperábase só a acción válida e sen repetir; obtívose %+v", acc)
	}
	if !filepath.IsAbs(acc[0].Script) {
		t.Errorf("a ruta do script debería ser absoluta: %q", acc[0].Script)
	}
}

// TestModuloSoConAccions: un módulo pode non ter aplicación que lanzar e traer
// só accións. Segue sendo un módulo válido.
func TestModuloSoConAccions(t *testing.T) {
	raiz := t.TempDir()
	escribirModulo(t, raiz, "so-accions", `{"id":"so-accions","accions":[{"id":"x","script":"x.sh"}]}`)

	ext := Externos(raiz)
	if len(ext) != 1 {
		t.Fatalf("un módulo só con accións debería valer; obtivéronse %d", len(ext))
	}
	if ext[0].Executable != "" {
		t.Error("non declarou executable: debería quedar baleiro")
	}
}
