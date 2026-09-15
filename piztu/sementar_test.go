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

// Con algo dentro, a lista xa é decisión do profesor: non se engade nada.
func TestSementarModulosNonTocaUnCartafolConModulos(t *testing.T) {
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

	// Agora o catálogo ofrece outro módulo distinto: xa non debe tocar nada.
	srv2 := servidorDeModulos(t, "outro")
	defer srv2.Close()
	a.catalogo["outro"] = modulos.CatalogoEntrada{URL: srv2.URL + "/outro_1.0.zip", Version: "1.0"}
	a.sementarModulos()

	if got := modulos.Externos(dir); len(got) != 1 {
		t.Errorf("non debía engadir nada cun módulo xa presente, obtido %+v", got)
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
