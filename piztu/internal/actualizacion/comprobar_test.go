package actualizacion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// releaseFalsa serve unha "última Release" coma a API de GitHub e apunta
// Comprobar a ela durante o test.
func releaseFalsa(t *testing.T, tag string, assets ...string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := releaseGitHub{TagName: tag, HTMLURL: "https://example.org/releases/" + tag}
		for _, a := range assets {
			rel.Assets = append(rel.Assets, assetGitHub{Name: a, BrowserDownloadURL: "https://example.org/" + a})
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(srv.Close)
	orixinal := urlUltimaRelease
	urlUltimaRelease = srv.URL
	t.Cleanup(func() { urlUltimaRelease = orixinal })
}

// Unha Release só co instalador (como 26.8–26.10.1) ten que avisar igual:
// antes Comprobar devolvía Disponible=false e ningunha aula se enteraba.
func TestComprobarAvisaAindaSenZip(t *testing.T) {
	releaseFalsa(t, "999.1", "Piztu_Debian_installer")
	e := Comprobar()
	if !e.Disponible {
		t.Fatal("hai unha versión máis nova: Disponible debería ser certo aínda que a Release non traia o .zip")
	}
	if e.Automatica || e.URLDescarga != "" {
		t.Fatalf("sen piztu_*.zip non se pode actualizar en quente; estado = %+v", e)
	}
	if e.URLRelease == "" {
		t.Fatal("sen .zip, a UI precisa a páxina da Release para ofrecer o instalador")
	}
}

func TestComprobarConZipEAutomatica(t *testing.T) {
	releaseFalsa(t, "999.1", "Piztu_Debian_installer", "piztu_999.1.zip", "piztu_999.1.zip.sha256")
	e := Comprobar()
	if !e.Disponible || !e.Automatica {
		t.Fatalf("con piztu_*.zip a actualización debería ser automática; estado = %+v", e)
	}
	if e.URLChecksum == "" {
		t.Fatal("o .sha256 da Release debería recollerse")
	}
}

func TestComprobarMesmaVersionNonAvisa(t *testing.T) {
	releaseFalsa(t, VersionLocal(), "piztu_x.zip")
	if Comprobar().Disponible {
		t.Fatal("a mesma versión ca instalada non é unha actualización")
	}
}
