package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"piztu/internal/config"
	"piztu/internal/modulos"
)

// zipDeModulo devolve un .zip cun módulo mínimo dentro (modulo.json + binario).
func zipDeModulo(t *testing.T, id string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	manifest := `{"id":"` + id + `","nome":"` + id + `","icona":"🔧","executable":"` + id + `","version":"1.0"}`
	for nome, contido := range map[string]string{
		"modulo.json": manifest,
		id:            "#!/bin/sh\n",
	} {
		f, err := z.Create(nome)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(contido)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func servidorDeModulos(t *testing.T, id string) *httptest.Server {
	t.Helper()
	zipb := zipDeModulo(t, id)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipb)
	}))
}

// Instalación recén feita: o cartafol só ten o LEEME.md que deixa Preparar.
func TestSementarModulosBaixaCoCartafolBaleiro(t *testing.T) {
	dir := t.TempDir()
	if err := modulos.Preparar(dir); err != nil {
		t.Fatal(err)
	}
	srv := servidorDeModulos(t, "probamod")
	defer srv.Close()

	a := &App{
		cfg:      &config.Config{ModulosDir: dir},
		catalogo: map[string]modulos.CatalogoEntrada{"probamod": {URL: srv.URL + "/probamod_1.0.zip", Version: "1.0"}},
	}
	a.sementarModulos()

	if got := modulos.Externos(dir); len(got) != 1 || got[0].ID != "probamod" {
		t.Fatalf("esperaba o módulo baixado, obtido %+v", got)
	}
	// O LEEME.md non se toca.
	if _, err := os.Stat(filepath.Join(dir, "LEEME.md")); err != nil {
		t.Errorf("desapareceu o LEEME.md: %v", err)
	}
}

// Un módulo NOVO publicado no repo ten que chegar tamén aos equipos que xa
// teñen módulos instalados — non só a unha instalación recén feita. Antes
// sementarModulos paraba en seco se o cartafol tiña algo, e iso deixaba os
// equipos da aula sen os módulos publicados despois de instalar Piztu.
func TestSementarModulosEngadeOsQueFaltanAindaQueXaHaxaAlgun(t *testing.T) {
	dir := t.TempDir()
	srv := servidorDeModulos(t, "xa-instalado")
	defer srv.Close()

	a := &App{
		cfg:      &config.Config{ModulosDir: dir},
		catalogo: map[string]modulos.CatalogoEntrada{"xa-instalado": {URL: srv.URL + "/xa-instalado_1.0.zip", Version: "1.0"}},
	}
	a.sementarModulos() // primeira vez: baixa
	if len(modulos.Externos(dir)) != 1 {
		t.Fatal("a primeira sementeira debería ter baixado o módulo")
	}

	// Agora o catálogo ofrece outro módulo distinto: ten que baixalo tamén.
	srv2 := servidorDeModulos(t, "outro")
	defer srv2.Close()
	a.catalogo["outro"] = modulos.CatalogoEntrada{URL: srv2.URL + "/outro_1.0.zip", Version: "1.0"}
	a.sementarModulos()

	got := modulos.Externos(dir)
	if len(got) != 2 {
		t.Fatalf("esperaba os dous módulos, obtido %+v", got)
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["xa-instalado"] || !ids["outro"] {
		t.Errorf("esperaba xa-instalado e outro, obtido %+v", got)
	}
}

// O que NON debe facer é reinstalar por riba dun módulo que xa está: poñelo
// ao día é traballo de actualizarModulosAoDia (que compara versións), non
// desta función. Compróbase cun ficheiro centinela dentro do cartafol do
// módulo: se sementarModulos volvese descomprimir o .zip enriba, esvaecería.
func TestSementarModulosNonReinstalaUnModuloXaPresente(t *testing.T) {
	dir := t.TempDir()
	srv := servidorDeModulos(t, "xa-instalado")
	defer srv.Close()

	a := &App{
		cfg:      &config.Config{ModulosDir: dir},
		catalogo: map[string]modulos.CatalogoEntrada{"xa-instalado": {URL: srv.URL + "/xa-instalado_1.0.zip", Version: "1.0"}},
	}
	a.sementarModulos()

	centinela := filepath.Join(dir, "xa-instalado", "centinela.txt")
	if err := os.WriteFile(centinela, []byte("non me toques\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.sementarModulos() // segunda pasada: non debe tocar o que xa está

	if _, err := os.Stat(centinela); err != nil {
		t.Errorf("reinstalou por riba dun módulo xa presente: %v", err)
	}
}

// Sen rede o catálogo queda baleiro: non é un erro, simplemente non fai nada.
func TestSementarModulosSenCatalogoNonFallaNinCrea(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: &config.Config{ModulosDir: dir}}
	a.sementarModulos()
	if got := modulos.Externos(dir); len(got) != 0 {
		t.Errorf("non debía crear nada, obtido %+v", got)
	}
}
