package main

import (
	"path/filepath"
	"testing"

	"piztu/internal/config"
	"piztu/internal/db"
)

func appRuidoDeProba(t *testing.T) *App {
	t.Helper()
	base := t.TempDir()
	cfg := &config.Config{
		BaseDir: base, PlaybooksDir: filepath.Join(base, "playbooks"),
		TmpDir: filepath.Join(base, "tmp"), DBPath: filepath.Join(base, "piztu.db"),
		UmbralFile: filepath.Join(base, "umbral.txt"),
		Ruido:      config.Ruido{UmbralDefecto: 50, SegDuracionBloqueo: 20},
	}
	store, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return &App{cfg: cfg, store: store}
}

func TestRuidoConfigPorDefectoVenDoConfigYaml(t *testing.T) {
	a := appRuidoDeProba(t)
	c := a.GetRuidoConfig()
	if c.Umbral != 50 || c.Duracion != 20 {
		t.Fatalf("sen gardar nada debería valer config.yaml (50 %%, 20 s); deu %+v", c)
	}
}

func TestGardarRuidoConfigPersiste(t *testing.T) {
	a := appRuidoDeProba(t)
	a.GardarRuidoConfig(65, 45)

	// Unha App nova sobre o mesmo almacenamento le o gardado.
	b := &App{cfg: a.cfg, store: a.store}
	c := b.GetRuidoConfig()
	if c.Umbral != 65 || c.Duracion != 45 {
		t.Fatalf("GetRuidoConfig tras gardar 65/45 = %+v", c)
	}
}

func TestGardarRuidoConfigAcouta(t *testing.T) {
	a := appRuidoDeProba(t)
	c := a.GardarRuidoConfig(500, 1)
	if c.Umbral != db.UmbralMax || c.Duracion != db.DuracionBloqueoMin {
		t.Fatalf("valores fóra de rango deberían acoutarse a %d/%d; deu %+v",
			db.UmbralMax, db.DuracionBloqueoMin, c)
	}
	c = a.GardarRuidoConfig(0, 100000)
	if c.Umbral != db.UmbralMin || c.Duracion != db.DuracionBloqueoMax {
		t.Fatalf("valores fóra de rango deberían acoutarse a %d/%d; deu %+v",
			db.UmbralMin, db.DuracionBloqueoMax, c)
	}
}
