// Package actualizacion comproba, descarga e instala actualizacións do propio
// Piztu (non dun módulo coma Tao — ver internal/modulos para iso).
//
// A versión instalada lese do ficheiro VERSION embebido na raíz do módulo Go.
// A versión publicada consúltase na última "Release" de GitHub do
// repositorio piztusistemas/Piztu (https://github.com/piztusistemas/Piztu/releases) —
// a Release leva como tag o número de versión (ex.: "26.9") e, adxunto, un
// .zip que contén só o executable "piztu" novo (todo o resto — frontend,
// idiomas, playbooks, salt — vai embebido dentro del con go:embed, así que
// non fai falla máis nada no paquete).
//
// A instalación (AplicarEnCaliente) substitúe ese executable polo que está en
// execución neste intre, sen que o profesor teña que pechar Piztu nin
// descomprimir nada á man; Reiniciar arrinca a nova versión decontado.
//
// A Release TAMÉN ten que publicar un asset "<nome-do-zip>.sha256" (formato
// "sha256sum": o hash en hex, opcionalmente seguido do nome do ficheiro) —
// AplicarEnCaliente compróbao antes de instalar nada. Sen isto, quen puidese
// publicar unha Release (ex.: un token filtrado) podería facer executar
// calquera cousa, coas mesmas credenciais ca Piztu, en cada aula que
// actualizase — ver https://github.com/piztusistemas/Piztu/security.
package actualizacion

import (
	"archive/zip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"piztu/internal/modulos"
)

//go:embed VERSION
var versionFicheiro string

const repo = "piztusistemas/Piztu"
const urlUltimaRelease = "https://api.github.com/repos/" + repo + "/releases/latest"
const timeoutComprobar = 5 * time.Second
const timeoutDescarga = 60 * time.Second

// Estado é o resultado de comprobar unha actualización de Piztu.
type Estado struct {
	Disponible    bool   `json:"disponible"`
	VersionLocal  string `json:"version_local"`
	VersionRemota string `json:"version_remota"`
	URLDescarga   string `json:"url_descarga"` // "" se non hai asset .zip descargable
	URLChecksum   string `json:"url_checksum"` // "" se a Release non publica "<zip>.sha256"
	Notas         string `json:"notas"`        // descrición da Release (changelog)
}

type assetGitHub struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type releaseGitHub struct {
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []assetGitHub `json:"assets"`
}

// escollerAssets busca, entre os assets dunha Release, o .zip do propio
// Piztu e o seu .sha256 — ver a nota en Comprobar sobre por que "piztu_"
// vai cravado e non abonda con "calquera .zip".
func escollerAssets(assets []assetGitHub) (urlZip, nomeZip, urlChecksum string) {
	for _, a := range assets {
		nome := strings.ToLower(a.Name)
		if strings.HasPrefix(nome, "piztu_") && strings.HasSuffix(nome, ".zip") {
			urlZip = a.BrowserDownloadURL
			nomeZip = a.Name
			break
		}
	}
	if urlZip == "" {
		return "", "", ""
	}
	for _, a := range assets {
		if strings.EqualFold(a.Name, nomeZip+".sha256") {
			urlChecksum = a.BrowserDownloadURL
			break
		}
	}
	return urlZip, nomeZip, urlChecksum
}

// VersionLocal devolve a versión instalada de Piztu.
func VersionLocal() string {
	return strings.TrimSpace(versionFicheiro)
}

// Comprobar consulta a última Release de piztusistemas/Piztu en GitHub e compárea
// coa versión instalada. Nunca devolve erro cara ao chamador: se falla a rede,
// non hai releases publicadas, ou a resposta non é válida, devolve
// Disponible=false para non bloquear o arranque de piztu.
func Comprobar() Estado {
	estado := Estado{VersionLocal: VersionLocal()}

	req, err := http.NewRequest(http.MethodGet, urlUltimaRelease, nil)
	if err != nil {
		return estado
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	cliente := http.Client{Timeout: timeoutComprobar}
	resp, err := cliente.Do(req)
	if err != nil {
		return estado
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return estado
	}

	var rel releaseGitHub
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return estado
	}

	estado.VersionRemota = rel.TagName
	estado.Notas = rel.Body

	// escollerAssets busca especificamente "piztu_<versión>.zip", non
	// "calquera .zip": a Release pode levar outros (ex.
	// piztu-installer_<versión>.zip) e un "primeiro .zip que atope" ingenuo
	// collía o que non tocaba — GitHub non garante orde de subida nos
	// assets, e "piztu-installer_..." vai alfabeticamente ANTES ca
	// "piztu_..." (o guión "-" ordena antes có "_"). Visto en produción: o
	// botón "Actualizar" descargaba o instalador, non atopaba dentro un
	// ficheiro chamado "piztu" (ver nomeExecutable), e fallaba.
	urlZip, _, urlChecksum := escollerAssets(rel.Assets)
	if urlZip == "" {
		return estado // hai release, pero sen "piztu_*.zip" adxunto: nada que ofrecer
	}
	estado.URLDescarga = urlZip
	estado.URLChecksum = urlChecksum

	estado.Disponible = modulos.VersionMaior(estado.VersionRemota, estado.VersionLocal)
	return estado
}

// nomeExecutable é o nome do ficheiro que ten que haber dentro do .zip da
// Release: o executable de Piztu tal cal o produce "wails build".
func nomeExecutable() string {
	if runtime.GOOS == "windows" {
		return "piztu.exe"
	}
	return "piztu"
}

// descargar descarga `url` en destinoDir/nomeFicheiro (créao se non existe) e
// devolve a ruta final do ficheiro.
func descargar(url, destinoDir, nomeFicheiro string) (string, error) {
	if err := os.MkdirAll(destinoDir, 0o755); err != nil {
		return "", fmt.Errorf("non se puido crear %q: %w", destinoDir, err)
	}
	destino := filepath.Join(destinoDir, nomeFicheiro)

	cliente := http.Client{Timeout: timeoutDescarga}
	resp, err := cliente.Get(url)
	if err != nil {
		return "", fmt.Errorf("non se puido descargar a actualización: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non se puido descargar a actualización: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destino)
	if err != nil {
		return "", fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("non se puido gardar a actualización: %w", err)
	}
	return destino, nil
}

// extraerExecutable busca dentro do .zip descargado o ficheiro chamado coma
// nomeExecutable() e cópiao a destDir. Devolve a súa ruta.
func extraerExecutable(zipRuta, destDir string) (string, error) {
	zr, err := zip.OpenReader(zipRuta)
	if err != nil {
		return "", fmt.Errorf("o ficheiro descargado non é un zip válido: %w", err)
	}
	defer zr.Close()

	buscado := nomeExecutable()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != buscado {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("non se puido abrir %q dentro do zip: %w", f.Name, err)
		}
		defer rc.Close()

		destino := filepath.Join(destDir, buscado)
		out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("non se puido crear %q: %w", destino, err)
		}
		defer out.Close()
		if _, err := io.Copy(out, rc); err != nil {
			return "", fmt.Errorf("non se puido extraer %q: %w", f.Name, err)
		}
		return destino, nil
	}
	return "", fmt.Errorf("o zip da actualización non contén %q — revisa como o empaquetaches", buscado)
}

// lerHashEsperado extrae o hash hexadecimal do contido dun asset ".sha256":
// admite tanto unha liña só co hash coma o formato de "sha256sum" (hash +
// espazo + nome de ficheiro).
func lerHashEsperado(r io.Reader) (string, error) {
	datos, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return "", fmt.Errorf("non se puido ler o .sha256: %w", err)
	}
	campos := strings.Fields(string(datos))
	if len(campos) == 0 {
		return "", fmt.Errorf("o ficheiro .sha256 da Release está baleiro")
	}
	return strings.ToLower(campos[0]), nil
}

// comprobarChecksum descarga urlChecksum e verifica que coincide co SHA-256
// real de ficheiroRuta. Devolve erro (e NON deixa continuar a AplicarEnCaliente)
// se falta, non se pode descargar, ou non coincide — mellor cancelar a
// actualización ca instalar algo sen verificar.
func comprobarChecksum(ficheiroRuta, urlChecksum string) error {
	if urlChecksum == "" {
		return fmt.Errorf("esta Release non publica un .sha256 do .zip — non se pode verificar a súa integridade, así que se cancela a actualización por seguridade")
	}

	cliente := http.Client{Timeout: timeoutComprobar}
	resp, err := cliente.Get(urlChecksum)
	if err != nil {
		return fmt.Errorf("non se puido descargar o .sha256 da actualización: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non se puido descargar o .sha256 da actualización: HTTP %d", resp.StatusCode)
	}
	esperado, err := lerHashEsperado(resp.Body)
	if err != nil {
		return err
	}

	f, err := os.Open(ficheiroRuta)
	if err != nil {
		return fmt.Errorf("non se puido abrir %q para verificalo: %w", ficheiroRuta, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("non se puido calcular o SHA-256 de %q: %w", ficheiroRuta, err)
	}
	obtido := hex.EncodeToString(h.Sum(nil))

	if obtido != esperado {
		return fmt.Errorf("o SHA-256 do .zip descargado non coincide co publicado na Release — podería estar adulterado; cancélase a actualización")
	}
	return nil
}

// copiarFicheiro copia orixe a destino (sobrescribindo), conservando permisos
// de executable.
func copiarFicheiro(orixe, destino string) error {
	in, err := os.Open(orixe)
	if err != nil {
		return fmt.Errorf("non se puido abrir %q: %w", orixe, err)
	}
	defer in.Close()
	out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("non se puido crear %q: %w", destino, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("non se puido copiar a %q: %w", destino, err)
	}
	return nil
}

// AplicarEnCaliente descarga o .zip da Release en `urlZip`, verifica o seu
// SHA-256 contra o publicado en `urlChecksum`, extrae dentro del o executable
// de Piztu e substitúeo polo que está en execución agora mesmo — sen
// pechar Piztu nin descomprimir nada á man (← botón "Actualizar" no diálogo
// "Sobre Piztu"). Non reinicia por si mesma: quen chama decide cando (ver
// Reiniciar), para poder avisar antes ao profesor.
//
// Se falta o checksum, non se pode descargar, ou non coincide, non se toca
// nada — mellor cancelar a actualización ca instalar un binario sen verificar
// (ver comprobarChecksum).
//
// A substitución escribe primeiro o binario novo canda o vello (mesmo
// cartafol → mesmo sistema de ficheiros) e só ao final fai un os.Rename
// atómico enriba del: se algo falla a metade, o binario orixinal non se toca;
// e mentres tanto, o proceso que segue en execución non nota nada (Linux/
// macOS non bloquean un executable por estar a correr). Antes gárdase tamén
// unha copia coma "<executable>.anterior", por se hai que recuperala á man.
func AplicarEnCaliente(urlZip, urlChecksum string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("a actualización automática aínda non está soportada en Windows")
	}

	exeActual, err := os.Executable()
	if err != nil {
		return fmt.Errorf("non se puido localizar o executable actual: %w", err)
	}
	exeActual, err = filepath.EvalSymlinks(exeActual)
	if err != nil {
		return fmt.Errorf("non se puido resolver o executable actual: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "piztu-actualizacion-")
	if err != nil {
		return fmt.Errorf("non se puido crear un cartafol temporal: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipRuta, err := descargar(urlZip, tmpDir, "actualizacion.zip")
	if err != nil {
		return err
	}
	if err := comprobarChecksum(zipRuta, urlChecksum); err != nil {
		return err
	}
	novoExe, err := extraerExecutable(zipRuta, tmpDir)
	if err != nil {
		return err
	}

	swapTmp := exeActual + ".novo"
	defer os.Remove(swapTmp) // non-op se xa se renomeou
	if err := copiarFicheiro(novoExe, swapTmp); err != nil {
		return err
	}

	_ = copiarFicheiro(exeActual, exeActual+".anterior") // copia de reserva; non crítico se falla

	if err := os.Rename(swapTmp, exeActual); err != nil {
		return fmt.Errorf("non se puido instalar o binario novo: %w", err)
	}
	return nil
}

// Reiniciar arrinca unha nova copia de Piztu (xa a versión instalada por
// AplicarEnCaliente) coma proceso independente, para que quen chame poida
// pechar esta xanela decontado (← Quit() no frontend, mesmo camiño ca "Sair").
func Reiniciar() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("non se puido localizar o executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("non se puido resolver o executable: %w", err)
	}

	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("non se puido reiniciar Piztu: %w", err)
	}
	return nil
}
