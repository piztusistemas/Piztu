package actualizacion

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// zipConFicheiro crea en memoria un .zip cun único ficheiro, co nome dado
// (nomeExecutable() se se quere que extraerExecutable o atope, calquera
// outro para probar o camiño de erro).
func zipConFicheiro(nome string, contido []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(nome)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(contido); err != nil {
		panic(err)
	}
	zw.Close()
	return buf.Bytes()
}

// servidorActualizacion serve o .zip en "/zip" e (se se lle pasa un hash) o
// .sha256 correspondente en "/zip.sha256"; devolve as dúas URLs.
func servidorActualizacion(t *testing.T, zipBytes []byte, hashPublicado string) (urlZip, urlChecksum string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/zip", func(w http.ResponseWriter, r *http.Request) { w.Write(zipBytes) })
	if hashPublicado != "" {
		mux.HandleFunc("/zip.sha256", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(hashPublicado + "  actualizacion.zip\n"))
		})
	}
	servidor := httptest.NewServer(mux)
	t.Cleanup(servidor.Close)
	return servidor.URL + "/zip", servidor.URL + "/zip.sha256"
}

func sha256Hex(datos []byte) string {
	h := sha256.Sum256(datos)
	return hex.EncodeToString(h[:])
}

// TestAplicarEnCaliente comproba que AplicarEnCaliente, con checksum correcto,
// substitúe de verdade o executable en execución (aquí, o propio binario de
// "go test") e que deixa unha copia ".anterior" co contido orixinal, por se
// hai que recuperala á man.
func TestAplicarEnCaliente(t *testing.T) {
	exeOrixinal, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contidoOrixinal, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.WriteFile(exeOrixinal, contidoOrixinal, 0o755)
		os.Remove(exeOrixinal + ".anterior")
		os.Remove(exeOrixinal + ".novo")
	})

	contidoNovo := []byte("binario-falso-de-proba")
	zipBytes := zipConFicheiro(nomeExecutable(), contidoNovo)
	urlZip, urlChecksum := servidorActualizacion(t, zipBytes, sha256Hex(zipBytes))

	if err := AplicarEnCaliente(urlZip, urlChecksum); err != nil {
		t.Fatalf("AplicarEnCaliente: %v", err)
	}

	novo, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(novo, contidoNovo) {
		t.Fatalf("o executable non se substituíu (len=%d)", len(novo))
	}

	anterior, err := os.ReadFile(exeOrixinal + ".anterior")
	if err != nil {
		t.Fatalf("non se gardou a copia .anterior: %v", err)
	}
	if !bytes.Equal(anterior, contidoOrixinal) {
		t.Fatal("a copia .anterior non coincide co binario orixinal")
	}
}

// TestAplicarEnCalienteSenExecutable comproba que, se o .zip publicado non
// contén un ficheiro co nome esperado (erro típico ao empaquetar a release),
// AplicarEnCaliente falla cun erro claro e NON toca o executable actual.
func TestAplicarEnCalienteSenExecutable(t *testing.T) {
	exeOrixinal, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contidoOrixinal, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}

	zipBytes := zipConFicheiro("outra-cousa.txt", []byte("non son o executable"))
	urlZip, urlChecksum := servidorActualizacion(t, zipBytes, sha256Hex(zipBytes))

	if err := AplicarEnCaliente(urlZip, urlChecksum); err == nil {
		t.Fatal("esperábase erro por non haber executable no zip")
	}

	tras, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tras, contidoOrixinal) {
		t.Fatal("o executable orixinal non debía tocarse cando falla a extracción")
	}
}

// TestAplicarEnCalienteSenChecksum comproba que, se a Release non publica
// ".sha256" (urlChecksum baleiro), AplicarEnCaliente cancela a actualización
// sen tocar o executable — este é o caso que arranxa a vulnerabilidade de
// "instalar sen verificar".
func TestAplicarEnCalienteSenChecksum(t *testing.T) {
	exeOrixinal, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contidoOrixinal, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}

	zipBytes := zipConFicheiro(nomeExecutable(), []byte("binario-falso"))
	urlZip, _ := servidorActualizacion(t, zipBytes, "") // sen .sha256 publicado

	err = AplicarEnCaliente(urlZip, "")
	if err == nil {
		t.Fatal("esperábase erro por non haber .sha256 que verificar")
	}

	tras, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tras, contidoOrixinal) {
		t.Fatal("o executable orixinal non debía tocarse sen checksum que verificar")
	}
}

// TestAplicarEnCalienteChecksumIncorrecto comproba que, se o .sha256
// publicado non coincide co .zip real (adulterado ou erro ao publicar),
// AplicarEnCaliente cancela a actualización sen tocar o executable.
func TestAplicarEnCalienteChecksumIncorrecto(t *testing.T) {
	exeOrixinal, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contidoOrixinal, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}

	zipBytes := zipConFicheiro(nomeExecutable(), []byte("binario-falso"))
	hashIncorrecto := sha256Hex([]byte("isto non é o zip"))
	urlZip, urlChecksum := servidorActualizacion(t, zipBytes, hashIncorrecto)

	err = AplicarEnCaliente(urlZip, urlChecksum)
	if err == nil {
		t.Fatal("esperábase erro por checksum incorrecto")
	}

	tras, err := os.ReadFile(exeOrixinal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tras, contidoOrixinal) {
		t.Fatal("o executable orixinal non debía tocarse cun checksum incorrecto")
	}
}

// TestEscollerAssetsIgnoraOutrosZip é a regresión do bug real visto en
// produción: unha Release con máis dun .zip (ex. tamén
// piztu-installer_26.9.zip, publicado xunto ao de piztu) tiña que seguir
// escollendo o .zip de piztu, non "o primeiro .zip que atope" — GitHub
// devolve os assets en orde alfabética, e "piztu-installer_..." vai antes ca
// "piztu_..." (o guión ordena antes có guión baixo).
func TestEscollerAssetsIgnoraOutrosZip(t *testing.T) {
	assets := []assetGitHub{
		{Name: "piztu-installer_26.9.zip", BrowserDownloadURL: "https://exemplo/piztu-installer_26.9.zip"},
		{Name: "piztu-installer_26.9.zip.sha256", BrowserDownloadURL: "https://exemplo/piztu-installer_26.9.zip.sha256"},
		{Name: "piztu_26.9.zip", BrowserDownloadURL: "https://exemplo/piztu_26.9.zip"},
		{Name: "piztu_26.9.zip.sha256", BrowserDownloadURL: "https://exemplo/piztu_26.9.zip.sha256"},
	}

	urlZip, nomeZip, urlChecksum := escollerAssets(assets)

	if nomeZip != "piztu_26.9.zip" {
		t.Fatalf("escollerAssets colleu %q, quería \"piztu_26.9.zip\"", nomeZip)
	}
	if urlZip != "https://exemplo/piztu_26.9.zip" {
		t.Fatalf("URL do zip incorrecta: %q", urlZip)
	}
	if urlChecksum != "https://exemplo/piztu_26.9.zip.sha256" {
		t.Fatalf("URL do checksum incorrecta: %q", urlChecksum)
	}
}

// TestEscollerAssetsSenPiztuZip: sen ningún "piztu_*.zip" (só o instalador,
// por exemplo), non hai que ofrecer nada — non calquera .zip vale.
func TestEscollerAssetsSenPiztuZip(t *testing.T) {
	assets := []assetGitHub{
		{Name: "piztu-installer_26.9.zip", BrowserDownloadURL: "https://exemplo/piztu-installer_26.9.zip"},
	}
	urlZip, nomeZip, _ := escollerAssets(assets)
	if urlZip != "" || nomeZip != "" {
		t.Fatalf("esperábase nada, obtívose urlZip=%q nomeZip=%q", urlZip, nomeZip)
	}
}
