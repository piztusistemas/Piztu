package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"piztu/internal/config"
	"piztu/internal/modulos"
)

// fakeAxustes é un modulos.Axustes en memoria, para non precisar unha BD
// real nestes tests.
type fakeAxustes struct{ m map[string]string }

func newFakeAxustes() *fakeAxustes { return &fakeAxustes{m: map[string]string{}} }

func (f *fakeAxustes) LerAxuste(clave, defecto string) string {
	if v, ok := f.m[clave]; ok {
		return v
	}
	return defecto
}

func (f *fakeAxustes) GardarAxuste(clave, valor string) error {
	f.m[clave] = valor
	return nil
}

// escribirModuloFalso crea <dir>/<id>/modulo.json cun executable calquera,
// dabondo para que modulos.Externos o recoñeza coma módulo.
func escribirModuloFalso(t *testing.T, dir, id string) {
	t.Helper()
	sub := filepath.Join(dir, id)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	manifesto := `{"id":"` + id + `","nome":"` + id + `","icona":"🧩","executable":"bin"}`
	if err := os.WriteFile(filepath.Join(sub, "modulo.json"), []byte(manifesto), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleModulosDevolveSoOsActivos(t *testing.T) {
	dir := t.TempDir()
	escribirModuloFalso(t, dir, "tao")
	escribirModuloFalso(t, dir, "yang") // queda inactivo, sen entrada gardada

	ax := newFakeAxustes()
	ax.m[modulos.ClaveActivo("tao")] = "1"

	srv := Novo(&config.Config{ModulosDir: dir}, ax)
	token := srv.EmitirToken("consultor", []string{PermisoModulosLer})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modulos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtido %d: %s", w.Code, w.Body.String())
	}
	var res RespostaModulos
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("resposta non válida: %v", err)
	}
	if len(res.Modulos) != 1 || res.Modulos[0].ID != "tao" {
		t.Fatalf("esperaba só [tao] activo, obtido %+v", res.Modulos)
	}
	if res.Modulos[0].Icona != "🧩" {
		t.Fatalf("esperaba a icona do manifest, obtido %q", res.Modulos[0].Icona)
	}
}

func TestHandleModulosListaBaleiraNonNull(t *testing.T) {
	srv := Novo(&config.Config{ModulosDir: t.TempDir()}, newFakeAxustes())
	token := srv.EmitirToken("consultor", []string{PermisoModulosLer})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modulos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.mux().ServeHTTP(w, req)

	if got := w.Body.String(); !containsModulosBaleiro(got) {
		t.Fatalf(`sen módulos activos a resposta debe levar "modulos":[], obtido %s`, got)
	}
}

func containsModulosBaleiro(body string) bool {
	var res RespostaModulos
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		return false
	}
	return res.Modulos != nil && len(res.Modulos) == 0
}

func TestHandleModulosEsixePermiso(t *testing.T) {
	srv := Novo(&config.Config{ModulosDir: t.TempDir()}, newFakeAxustes())
	token := srv.EmitirToken("consultor", nil) // sen o permiso modulos.ler

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modulos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.mux().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("esperaba 403 sen o permiso modulos.ler, obtido %d", w.Code)
	}
}

func TestHandleModulosSenTokenE401(t *testing.T) {
	srv := Novo(&config.Config{ModulosDir: t.TempDir()}, newFakeAxustes())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/modulos", nil)
	w := httptest.NewRecorder()
	srv.mux().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401 sen token, obtido %d", w.Code)
	}
}

// TestHandleModulosSenAxustesUsaPorDefecto comproba que un Servidor sen BD
// (axustes nil, ex. db.Open fallou) non entra en pánico e simplemente usa o
// PorDefecto de cada módulo. En piztu/app.go isto require coidado ao
// construír o Servidor real: un *db.Store nil pasado directamente como
// modulos.Axustes deixaría de ser == nil (interface con tipo dinámico), por
// iso alí se pasa unha interface explicitamente nil se store == nil.
func TestHandleModulosSenAxustesUsaPorDefecto(t *testing.T) {
	dir := t.TempDir()
	escribirModuloFalso(t, dir, "tao") // externo, PorDefecto=false sempre

	srv := Novo(&config.Config{ModulosDir: dir}, nil)
	token := srv.EmitirToken("consultor", []string{PermisoModulosLer})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modulos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("esperaba 200 mesmo sen axustes, obtido %d: %s", w.Code, w.Body.String())
	}
	var res RespostaModulos
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("resposta non válida: %v", err)
	}
	if len(res.Modulos) != 0 {
		t.Fatalf("sen axustes, un externo (PorDefecto=false) debe contar coma inactivo, obtido %+v", res.Modulos)
	}
}
