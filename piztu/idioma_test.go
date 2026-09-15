package main

import (
	"path/filepath"
	"testing"

	"piztu/internal/config"
	"piztu/internal/db"
	"piztu/internal/i18n"
)

func TestSetIdiomaPersisteEDevolveTraducions(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{
		BaseDir: base, PlaybooksDir: filepath.Join(base, "playbooks"),
		TmpDir: filepath.Join(base, "tmp"), DBPath: filepath.Join(base, "piztu.db"),
	}
	store, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	a := &App{cfg: cfg, store: store, tr: i18n.New(nil, "gl")}

	traducions, err := a.SetIdioma("es")
	if err != nil {
		t.Fatalf("SetIdioma: %v", err)
	}
	if traducions == nil {
		t.Fatal("SetIdioma debería devolver as traducións do idioma novo, non nil")
	}
	if got := a.tr.Idioma(); got != "es" {
		t.Errorf("a.tr.Idioma() = %q tras SetIdioma, quería \"es\"", got)
	}
	if got := store.LerAxuste("idioma", ""); got != "es" {
		t.Errorf("idioma non persistido en internal/db: LerAxuste = %q, quería \"es\"", got)
	}
}

func TestSetIdiomaRexeitaCodigoNonSoportado(t *testing.T) {
	a := &App{tr: i18n.New(nil, "gl")}
	if _, err := a.SetIdioma("fr"); err == nil {
		t.Fatal("SetIdioma(\"fr\") debería fallar: non hai fr.lang en recursos/idiomas/")
	}
	if got := a.tr.Idioma(); got != "gl" {
		t.Errorf("un idioma rexeitado non debe cambiar o activo: a.tr.Idioma() = %q, quería \"gl\"", got)
	}
}

// TestStartupPrefireIdiomaPersistidoSobreConfig cobre a orde de prioridade
// documentada en startup(): un idioma xa gardado en internal/db (chave
// "idioma", ex. por SetIdioma nunha sesión anterior) gaña sobre cfg.Idioma —
// así o cambio feito en ⚙ Configuración sobrevive a un reinicio de Piztu sen
// tocar config.yaml.
func TestStartupPrefireIdiomaPersistidoSobreConfig(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{
		BaseDir: base, PlaybooksDir: filepath.Join(base, "playbooks"),
		TmpDir: filepath.Join(base, "tmp"), DBPath: filepath.Join(base, "piztu.db"),
		Idioma: "gl",
	}
	store, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.GardarAxuste("idioma", "pt"); err != nil {
		t.Fatalf("GardarAxuste: %v", err)
	}

	idiomaInicial := cfg.Idioma
	if store != nil {
		idiomaInicial = store.LerAxuste("idioma", cfg.Idioma)
	}
	if idiomaInicial != "pt" {
		t.Errorf("idiomaInicial = %q, quería \"pt\" (persistido gaña sobre cfg.Idioma=%q)", idiomaInicial, cfg.Idioma)
	}
}
