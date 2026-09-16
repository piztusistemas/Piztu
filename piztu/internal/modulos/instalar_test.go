package modulos

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTodosOsZipsPorID cobre o descubrimento automático (idTodoORepo, "*"):
// dado un listado cru da raíz dun repo de GitHub (tal cal o devolve a súa
// API de contidos), agrupa por id e queda coa versión máis nova de cada un,
// sen que ningún id estivese declarado de antemán. Caso real: o repo
// piztusistemas/modulos ten varias versións de tao e xesta convivindo á vez.
func TestTodosOsZipsPorID(t *testing.T) {
	entradas := []ghContentEntry{
		{Name: "LICENSE", DownloadURL: "https://x/LICENSE"},
		{Name: "tao_26.2.zip", DownloadURL: "https://x/tao_26.2.zip"},
		{Name: "tao_26.7.zip", DownloadURL: "https://x/tao_26.7.zip"},
		{Name: "xesta_26.4.zip", DownloadURL: "https://x/xesta_26.4.zip"},
		{Name: "xesta_26.7.1.zip", DownloadURL: "https://x/xesta_26.7.1.zip"},
		{Name: "xesta_26.7.zip", DownloadURL: "https://x/xesta_26.7.zip"},
		{Name: "cisterna-1.0.0.zip", DownloadURL: "https://x/cisterna-1.0.0.zip"}, // separador "-", id novo
		{Name: "sen-version.zip", DownloadURL: "https://x/sen-version.zip"},       // sen número: ignórase
		{Name: "sen-url.zip"}, // sen DownloadURL: ignórase
	}

	got := todosOsZipsPorID(entradas)

	want := map[string]CatalogoEntrada{
		"tao":      {Version: "26.7", URL: "https://x/tao_26.7.zip"},
		"xesta":    {Version: "26.7.1", URL: "https://x/xesta_26.7.1.zip"},
		"cisterna": {Version: "1.0.0", URL: "https://x/cisterna-1.0.0.zip"},
	}
	if len(got) != len(want) {
		t.Fatalf("todosOsZipsPorID: %d ids atopados, quería %d: %+v", len(got), len(want), got)
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Errorf("todosOsZipsPorID: falta o id %q", id)
			continue
		}
		if g != w {
			t.Errorf("todosOsZipsPorID[%q] = %+v, quería %+v", id, g, w)
		}
	}
}

// TestMellorZipPrefixo cobre o camiño existente (un id xa COÑECIDO): entre
// varias versións do mesmo prefixo, escolle a máis nova; outros prefixos e
// ficheiros sen versión quedan fóra.
func TestMellorZipPrefixo(t *testing.T) {
	entradas := []ghContentEntry{
		{Name: "tao_26.2.zip", DownloadURL: "https://x/tao_26.2.zip"},
		{Name: "tao_26.7.zip", DownloadURL: "https://x/tao_26.7.zip"},
		{Name: "xesta_99.0.zip", DownloadURL: "https://x/xesta_99.0.zip"},
		{Name: "tao.zip", DownloadURL: "https://x/tao.zip"},
	}

	got, ok := mellorZipPrefixo(entradas, "tao")
	if !ok {
		t.Fatal("mellorZipPrefixo: esperaba achar unha coincidencia")
	}
	want := CatalogoEntrada{Version: "26.7", URL: "https://x/tao_26.7.zip"}
	if got != want {
		t.Errorf("mellorZipPrefixo(tao) = %+v, quería %+v", got, want)
	}

	if _, ok := mellorZipPrefixo(entradas, "descoñecido"); ok {
		t.Error("mellorZipPrefixo: non debería achar nada para un prefixo sen ficheiros")
	}
}

// TestEngadirEntradaComodinSenGitHubRepo comproba que "*" sen github_repo
// declarado non fai nada (nin sequera intenta unha chamada de rede) — non
// habería id ningún ao que asignarlle unha url fixa.
func TestEngadirEntradaComodinSenGitHubRepo(t *testing.T) {
	indice := map[string]CatalogoEntrada{}
	engadirEntrada(indice, idTodoORepo, CatalogoEntrada{URL: "https://exemplo.gal/algo.zip"})
	if len(indice) != 0 {
		t.Errorf("engadirEntrada(%q sen github_repo): esperaba índice baleiro, obtiven %+v", idTodoORepo, indice)
	}
}

// TestEngadirEntradaNormal comproba que un id normal (non "*") se resolve
// coma sempre, sen pasar polo camiño de descubrimento.
func TestEngadirEntradaNormal(t *testing.T) {
	indice := map[string]CatalogoEntrada{}
	engadirEntrada(indice, "cisterna", CatalogoEntrada{URL: "https://exemplo.gal/cisterna_1.0.0.zip"})
	got, ok := indice["cisterna"]
	if !ok {
		t.Fatal("engadirEntrada: esperaba atopar o id \"cisterna\"")
	}
	if got.Version != "1.0.0" {
		t.Errorf("engadirEntrada: version = %q, quería %q (sacada do nome do ficheiro)", got.Version, "1.0.0")
	}
}

// TestActualizarNonBorraSeFallaADescarga é a garantía que fai segura a
// actualización automática (App.vixiarActualizacionsModulos): se a rede cae
// ou a URL non serve, o módulo que xa había ten que seguir enteiro no seu
// sitio. A versión anterior borraba primeiro e descargaba despois, así que
// un fallo de rede deixaba o cartafol baleiro.
func TestActualizarNonBorraSeFallaADescarga(t *testing.T) {
	dir := t.TempDir()
	instalado := filepath.Join(dir, "tao")
	if err := os.MkdirAll(instalado, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"tao","nome":"Tao","executable":"tao","version":"26.2"}`)
	if err := os.WriteFile(filepath.Join(instalado, "modulo.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Actualizar(dir, "tao", CatalogoEntrada{
		Version: "26.9",
		URL:     "http://127.0.0.1:1/non-existe.zip", // porto pechado: falla seguro
	})
	if err == nil {
		t.Fatal("agardábase erro: a descarga non pode ter éxito contra un porto pechado")
	}

	// O módulo segue instalado, e na versión de antes.
	mods := Externos(dir)
	if len(mods) != 1 || mods[0].ID != "tao" || mods[0].Version != "26.2" {
		t.Fatalf("o módulo instalado non sobreviviu ao fallo: %+v", mods)
	}
	// E non queda lixo visible: os cartafoles de traballo van ocultos e
	// Externos ignóraos, pero o ".novo" tampouco debe quedar aí.
	if _, err := os.Stat(filepath.Join(dir, ".tao.novo")); !os.IsNotExist(err) {
		t.Error("quedou o cartafol temporal .tao.novo despois do fallo")
	}
}
