package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"piztu/internal/config"
	"piztu/internal/i18n"
	"piztu/internal/modulos"
)

// axustesMemoria imita o almacén de axustes sen base de datos, para poder probar
// as comprobacións de módulo sen montar unha App completa.
type axustesMemoria struct{ datos map[string]string }

func (a *axustesMemoria) LerAxuste(clave, defecto string) string {
	if v, hai := a.datos[clave]; hai {
		return v
	}
	return defecto
}
func (a *axustesMemoria) GardarAxuste(clave, valor string) error {
	a.datos[clave] = valor
	return nil
}

// TestAccionsRexeitadasSeOModuloEstaApagado é o que xustifica ter a comprobación
// no backend e non só ocultar os botóns: os métodos de App son bindings de Wails
// e seguen sendo chamables aínda que a barra non os amose (un frontend
// desactualizado, ou calquera que fale co binding directamente).
func TestAccionsRexeitadasSeOModuloEstaApagado(t *testing.T) {
	dir := t.TempDir()
	ax := &axustesMemoria{datos: map[string]string{}}
	if err := modulos.Set(ax, modulos.Ruido, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := modulos.Set(ax, modulos.Ficheiros, false); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if modulos.ActivoPorID(ax, dir, modulos.Ficheiros) {
		t.Fatal("o módulo debería estar apagado tras Set(false)")
	}
	// Co módulo apagado, o helper que protexe EnviarPracticas/Recoller/Limpar
	// ten que dicir que non. Apagar ficheiros non debe afectar a ruído.
	if modulos.ActivoPorID(ax, dir, modulos.Ruido) != true {
		t.Error("apagar ficheiros non debe afectar a ruído")
	}
}

// TestErroModuloInactivoNomeaOModulo: a mensaxe de erro ten que dicir cal é o
// módulo e onde se activa, ou o profesor non saberá que facer con ela.
func TestErroModuloInactivoNomeaOModulo(t *testing.T) {
	err := errModuloInactivo(modulos.Tao)
	if err == nil {
		t.Fatal("agardábase un erro")
	}
	msg := err.Error()
	if !strings.Contains(msg, modulos.Tao) {
		t.Errorf("o erro non nomea o módulo: %q", msg)
	}
	if !strings.Contains(msg, "Aula") {
		t.Errorf("o erro non di onde activalo: %q", msg)
	}
}

// TestModulosDirDerivadoDeBaseDir garante que o cartafol de módulos segue o
// mesmo patrón ca o resto de rutas de piztu.
func TestModulosDirDerivadoDeBaseDir(t *testing.T) {
	cfg := &config.Config{BaseDir: "/opt/piztu"}
	a := &App{cfg: cfg}

	// Sen store, moduloActivo cae no defecto do rexistro (todo apagado).
	if a.moduloActivo(modulos.Ruido) {
		t.Error("sen base de datos, os módulos non deberían vir activos por defecto")
	}
	if a.moduloActivo("apeiro-inventado") {
		t.Error("un módulo descoñecido non debe contar como activo")
	}
}

// TestTaoBuscaseNoCartafolDeModulos: Tao é o único módulo que trae Piztu cunha
// aplicación propia, así que o seu sitio é modulos/tao/. As rutas antigas
// séguense aceptando para non romper instalacións xa feitas, pero a do cartafol
// ten prioridade.
func TestTaoBuscaseNoCartafolDeModulos(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{BaseDir: base, ModulosDir: filepath.Join(base, "modulos")}
	a := &App{cfg: cfg}

	if _, ok := a.localizarTao(); ok {
		t.Fatal("sen instalar, Tao non debería atoparse")
	}

	nome := "tao"
	if runtime.GOOS == "darwin" {
		nome = "Tao.app"
	}
	destino := filepath.Join(cfg.ModulosDir, "tao", nome)
	if err := os.MkdirAll(destino, 0o755); err != nil {
		t.Fatal(err)
	}

	ruta, ok := a.localizarTao()
	if !ok {
		t.Fatal("Tao debería atoparse en modulos/tao/")
	}
	if ruta != destino {
		t.Errorf("atopouse en %q; agardábase %q", ruta, destino)
	}
}

// TestModulosDispoDiOndeViveCadaUn: é o que fai comprensible o cartafol. Os
// módulos sen ficheiros propios non teñen ruta, e iso ten que verse.
func TestModulosDispoDiOndeViveCadaUn(t *testing.T) {
	base := t.TempDir()
	a := &App{cfg: &config.Config{BaseDir: base, ModulosDir: filepath.Join(base, "modulos")}}

	// Tao xa non é interno: só aparece en ModulosDispo se ten manifest,
	// coma calquera módulo externo (ver tao/modulo.json).
	taoDir := filepath.Join(a.cfg.ModulosDir, "tao")
	if err := os.MkdirAll(taoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"tao","nome":"Tao","icona":"🧭","executable":"tao"}`
	if err := os.WriteFile(filepath.Join(taoDir, "modulo.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	viuTao := false
	for _, m := range a.ModulosDispo() {
		switch m.ID {
		case modulos.Ruido, modulos.Ficheiros:
			if m.Ruta != "" {
				t.Errorf("%s vai dentro de piztu: non debería ter ruta (%q)", m.ID, m.Ruta)
			}
		case modulos.Tao:
			viuTao = true
			if m.Instalado {
				t.Error("sen instalar, Tao non debería marcarse como instalado")
			}
			if !strings.Contains(m.Motivo, "modulos") {
				t.Errorf("o motivo debería dicir onde copiar Tao: %q", m.Motivo)
			}
		}
	}
	if !viuTao {
		t.Fatal("Tao debería aparecer en ModulosDispo tendo manifest en modulos/tao/")
	}
}

// TestConfigResolveAccionsDeModulo: o truco que fai que isto non precise tocar
// os motores. Rexistrada a acción, PlaybookPath/SaltStatePath devolven o
// ficheiro do módulo, e cada motor resolve por nome coma sempre.
func TestConfigResolveAccionsDeModulo(t *testing.T) {
	cfg := &config.Config{
		BaseDir: "/opt/piztu", PlaybooksDir: "/opt/piztu/playbooks", SaltDir: "/opt/piztu/salt",
		Ansible: config.Ansible{Playbooks: map[string]string{"acender": "acenderAula"}},
	}

	// Antes de rexistrar, resólvese nos directorios globais.
	if got := cfg.PlaybookPath("baleirar"); got != "/opt/piztu/playbooks/baleirar.yaml" {
		t.Errorf("sen rexistrar deu %q", got)
	}

	cfg.RexistrarAccionModulo("baleirar", config.AccionExterna{
		Playbook: "/mods/cisterna/baleirar.yaml",
		Estado:   "/mods/cisterna/baleirar.sls",
	})

	if got := cfg.PlaybookPath("baleirar"); got != "/mods/cisterna/baleirar.yaml" {
		t.Errorf("PlaybookPath = %q; agardábase a do módulo", got)
	}
	if got := cfg.SaltStatePath("baleirar"); got != "/mods/cisterna/baleirar.sls" {
		t.Errorf("SaltStatePath = %q; agardábase a do módulo", got)
	}
	// As accións de piztu non se ven afectadas.
	if got := cfg.PlaybookPath("acender"); got != "/opt/piztu/playbooks/acenderAula.yaml" {
		t.Errorf("unha acción interna cambiou de ruta: %q", got)
	}

	cfg.LimparAccionsModulo()
	if got := cfg.PlaybookPath("baleirar"); got != "/opt/piztu/playbooks/baleirar.yaml" {
		t.Errorf("tras limpar debería volver á ruta global, deu %q", got)
	}
}

// TestContextoTenOsCamposDoContrato: os nomes deste JSON son contrato público —
// as librerías cliente dos módulos dependen deles. Cambialos rompe módulos de
// terceiros sen aviso.
func TestContextoTenOsCamposDoContrato(t *testing.T) {
	base := t.TempDir()
	a := &App{cfg: &config.Config{
		BaseDir: base, ModulosDir: filepath.Join(base, "modulos"),
		TmpDir: base, HostsFile: filepath.Join(base, "hosts"),
	}}
	if err := os.WriteFile(a.cfg.HostsFile, []byte("[aula]\nbac01.local mac=aa:bb:cc:dd:ee:ff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.acendidos = map[string]bool{"bac01.local": true}
	a.tr = i18n.New(nil, "es")

	ruta, err := a.escribirContexto(modulos.Modulo{ID: "proba", Dir: "/mods/proba"})
	if err != nil {
		t.Fatalf("escribirContexto: %v", err)
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}

	var ctx ContextoModulo
	if err := json.Unmarshal(data, &ctx); err != nil {
		t.Fatalf("o contexto non é JSON válido: %v", err)
	}
	if ctx.Version != 1 {
		t.Errorf("version = %d; o contrato empeza na 1", ctx.Version)
	}
	if ctx.ModuloID != "proba" || ctx.ModuloDir != "/mods/proba" {
		t.Errorf("o módulo non se identifica ben: %+v", ctx)
	}
	if len(ctx.Equipos) != 1 {
		t.Fatalf("agardábase 1 equipo, hai %d", len(ctx.Equipos))
	}
	e := ctx.Equipos[0]
	if e.Nome != "bac01.local" || e.Mac != "aa:bb:cc:dd:ee:ff" || !e.Acendido {
		t.Errorf("equipo mal composto: %+v", e)
	}
	if ctx.Idioma != "es" {
		t.Errorf("idioma = %q; debería propagar o idioma activo de Piztu (\"es\")", ctx.Idioma)
	}
}

// TestContextoIdiomaBaleiroSenPanicSenTr cobre o caso dun App construído sen
// pasar por startup() (a.tr == nil, ex. outros tests desta suite): non debe
// entrar en pánico, e o contexto queda co idioma baleiro para que o módulo
// caia no seu propio fallback.
func TestContextoIdiomaBaleiroSenPanicSenTr(t *testing.T) {
	base := t.TempDir()
	a := &App{cfg: &config.Config{
		BaseDir: base, ModulosDir: filepath.Join(base, "modulos"),
		TmpDir: base, HostsFile: filepath.Join(base, "hosts"),
	}}
	if err := os.WriteFile(a.cfg.HostsFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	ruta, err := a.escribirContexto(modulos.Modulo{ID: "proba", Dir: "/mods/proba"})
	if err != nil {
		t.Fatalf("escribirContexto: %v", err)
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	var ctx ContextoModulo
	if err := json.Unmarshal(data, &ctx); err != nil {
		t.Fatalf("o contexto non é JSON válido: %v", err)
	}
	if ctx.Idioma != "" {
		t.Errorf("idioma = %q; sen a.tr debería quedar baleiro", ctx.Idioma)
	}
}
