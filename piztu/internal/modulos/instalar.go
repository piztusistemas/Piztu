// instalar.go completa o paquete modulos coa descarga de módulos externos
// publicados en piztu.org (equivalente Go de core/actualizacions.py).
//
// Un módulo "coñecido pero non instalado" (ex.: Tao antes de descargalo) non
// aparece en Externos() porque aínda non ten cartafol nin modulo.json; por
// iso o catálogo (índice remoto + catalogo_local de config.yaml) e a
// instalación viven á parte, e é app.go quen os xunta coa lista de sempre en
// ModulosDispo().
package modulos

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CatalogoEntrada é unha entrada do índice de módulos descargables: a versión
// publicada e a URL do seu .zip. Version pode deixarse baleira: normalizarEntrada
// tira daquela a versión do propio nome do ficheiro en URL (ver
// versionDoNomeFicheiro) — é o único xeito de que quen publica un módulo (que
// pode non ser quen o programou) non teña que editar un manifest á parte nin
// coordinarse con ninguén: nomea ben o .zip que sobe e xa está.
//
// Alternativa a URL/Version fixas: GitHubRepo. Con el, nin sequera fai falla
// tocar o catálogo cando sae unha versión nova — Piztu busca soa o .zip máis
// recente na raíz dese repositorio (ver resolverGitHubUltimaVersion).
type CatalogoEntrada struct {
	Version string `yaml:"version" json:"version"`
	URL     string `yaml:"url" json:"url"`
	// GitHubRepo, formato "usuario/repo": se está posta, ignórase URL/Version
	// explícitas e descóbrese o .zip máis novo que siga o patrón
	// "<prefixo>_versión.zip" (ou "-versión") na raíz dese repositorio.
	GitHubRepo string `yaml:"github_repo" json:"github_repo"`
	// GitHubPrefixo é o prefixo do nome de ficheiro a buscar; baleiro = o id
	// do módulo (a clave no catálogo).
	GitHubPrefixo string `yaml:"github_prefixo" json:"github_prefixo"`
}

const timeoutIndice = 5 * time.Second

// Descarga dun módulo: un .zip pode ser de decenas de MB (leva o binario
// Wails dentro) e nunha conexión de centro tardar minutos en baixar sen que
// iso sexa un erro. Por iso NON se usa http.Client.Timeout (é un tope total
// que inclúe a lectura do corpo: mataría unha descarga lenta pero viva —
// era a causa do "context deadline exceeded ... while reading body"). Os
// límites van só nas fases de conexión/cabeceiras, e hai un teito xeneroso
// por contexto para o conxunto.
const timeoutDescargaConexion = 20 * time.Second
const timeoutDescargaCabeceiras = 45 * time.Second
const timeoutDescargaTotal = 15 * time.Minute

// clienteDescarga devolve un http.Client apto para ficheiros grandes: sen
// Client.Timeout, con límites só na conexión, o TLS e a espera de
// cabeceiras. O tope global aplícase co contexto da petición.
func clienteDescarga() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: timeoutDescargaConexion}).DialContext,
			TLSHandshakeTimeout:   timeoutDescargaConexion,
			ResponseHeaderTimeout: timeoutDescargaCabeceiras,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// idTodoORepo é un id especial en catalogo_local/indice_url: "*" xunto con
// github_repo significa "descobre TODOS os módulos que haxa nese repo", non
// un único id xa coñecido de antemán (ver resolverGitHubRepoCompleto). É o
// que fai posible engadir un módulo novo subindo soamente o seu .zip á raíz
// do repo — "<id calquera>_<versión>.zip" — sen tocar config.yaml para
// nada: aparece só en ⚙ Aula → Módulos co botón "Descargar" a próxima vez
// que se refresque o catálogo. Unha entrada "*" sen github_repo (só con url
// fixa) ignórase — non habería id ningún ao que asignarlle esa URL.
const idTodoORepo = "*"

// engadirEntrada resolve unha entrada do catálogo (local ou remoto) e
// engádea a `indice`. Illado de IndiceRemoto para poder tratar "*" á parte:
// unha entrada normal engade UN id; "*" pode engadir CALQUERA número deles
// dun só golpe.
func engadirEntrada(indice map[string]CatalogoEntrada, id string, e CatalogoEntrada) {
	if id == idTodoORepo {
		if e.GitHubRepo == "" {
			return
		}
		if descubertos, ok := resolverGitHubRepoCompleto(e.GitHubRepo); ok {
			for idDescuberto, entrada := range descubertos {
				indice[idDescuberto] = entrada
			}
		}
		return
	}
	indice[id] = resolverEntrada(id, e)
}

// IndiceRemoto devolve o catálogo de módulos descargables: o publicado en
// `indiceURL` (piztu.org), completado con `catalogoLocal` (config.yaml →
// modulos.catalogo_local) para os nomes que aínda non estean alí — útil en
// desenvolvemento, antes de que piztu.org exista de verdade. Se un módulo
// aparece nos dous sitios, gaña o remoto. Sen conexión, devolve só o local.
func IndiceRemoto(indiceURL string, catalogoLocal map[string]CatalogoEntrada) map[string]CatalogoEntrada {
	indice := make(map[string]CatalogoEntrada, len(catalogoLocal))
	for id, e := range catalogoLocal {
		engadirEntrada(indice, id, e)
	}
	if indiceURL == "" {
		return indice
	}

	client := http.Client{Timeout: timeoutIndice}
	resp, err := client.Get(indiceURL)
	if err != nil {
		return indice
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return indice
	}

	var remoto map[string]CatalogoEntrada
	if err := json.NewDecoder(resp.Body).Decode(&remoto); err != nil {
		return indice
	}
	for id, e := range remoto {
		engadirEntrada(indice, id, e)
	}
	return indice
}

// resolverEntrada completa unha entrada do catálogo para UN id xa coñecido.
// Se declara GitHubRepo, consulta GitHub para atopar o .zip máis novo dese
// id (ver resolverGitHubUltimaVersion); se iso falla (sen rede, sen
// coincidencias…) cae ao URL/Version que houbese postos á man. Se non
// declara GitHubRepo, é o de sempre: normalizarEntrada.
func resolverEntrada(id string, e CatalogoEntrada) CatalogoEntrada {
	if e.GitHubRepo != "" {
		prefixo := e.GitHubPrefixo
		if prefixo == "" {
			prefixo = id
		}
		if achada, ok := resolverGitHubUltimaVersion(e.GitHubRepo, prefixo); ok {
			return achada
		}
	}
	return normalizarEntrada(e)
}

// ghContentEntry é unha entrada da resposta da API de contidos de GitHub
// (GET /repos/{repo}/contents/{path}).
type ghContentEntry struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

// listarContidoGitHubRepo lista a raíz de `repo` (formato "usuario/repo")
// pola API pública de GitHub — chamada HTTP común a
// resolverGitHubUltimaVersion (busca UN id coñecido) e
// resolverGitHubRepoCompleto (descobre TODOS os ids que haxa). Sen rede, sen
// repo, ou se a API falla (ex.: límite de peticións sen autenticar),
// devolve ok=false.
func listarContidoGitHubRepo(repo string) ([]ghContentEntry, bool) {
	api := "https://api.github.com/repos/" + repo + "/contents/"
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := http.Client{Timeout: timeoutIndice}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var entradas []ghContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entradas); err != nil {
		return nil, false
	}
	return entradas, true
}

// mellorZipPrefixo escolle, entre `entradas`, o .zip máis novo que siga o
// patrón "<prefixo>_versión.zip" (ou "-versión"). Sen coincidencias, devolve
// ok=false — quen chama cae ao URL/Version fixas.
func mellorZipPrefixo(entradas []ghContentEntry, prefixo string) (CatalogoEntrada, bool) {
	patron := regexp.MustCompile(`^` + regexp.QuoteMeta(prefixo) + `[_-]([0-9]+(?:\.[0-9]+)+)\.zip$`)
	var mellor CatalogoEntrada
	achada := false
	for _, e := range entradas {
		m := patron.FindStringSubmatch(e.Name)
		if m == nil || e.DownloadURL == "" {
			continue
		}
		if !achada || VersionMaior(m[1], mellor.Version) {
			mellor = CatalogoEntrada{Version: m[1], URL: e.DownloadURL}
			achada = true
		}
	}
	return mellor, achada
}

// resolverGitHubUltimaVersion busca en `repo` o .zip máis novo dun id XA
// COÑECIDO de antemán (ver mellorZipPrefixo). Sen rede ou sen coincidencias,
// devolve ok=false.
func resolverGitHubUltimaVersion(repo, prefixo string) (CatalogoEntrada, bool) {
	entradas, ok := listarContidoGitHubRepo(repo)
	if !ok {
		return CatalogoEntrada{}, false
	}
	return mellorZipPrefixo(entradas, prefixo)
}

// reZipVersionado recoñece calquera "<id>_versión.zip" (ou "-versión") na
// raíz dun repo SEN esixir un id xa coñecido de antemán — a diferenza do
// patrón en mellorZipPrefixo (que leva o prefixo cravado), aquí o id é
// xustamente o que se quere DESCUBRIR, así que vai el mesmo capturado como
// grupo. É o que fai posible resolverGitHubRepoCompleto.
var reZipVersionado = regexp.MustCompile(`^(.+?)[_-]([0-9]+(?:\.[0-9]+)+)\.zip$`)

// todosOsZipsPorID agrupa `entradas` por id (ver reZipVersionado) e queda,
// para cada id, coa versión máis nova atopada — un repo pode ter varias
// versións convivindo (ex.: "tao_26.2.zip" e "tao_26.7.zip") mentres se
// publica a seguinte.
func todosOsZipsPorID(entradas []ghContentEntry) map[string]CatalogoEntrada {
	res := map[string]CatalogoEntrada{}
	for _, e := range entradas {
		m := reZipVersionado.FindStringSubmatch(e.Name)
		if m == nil || e.DownloadURL == "" {
			continue
		}
		id, version := m[1], m[2]
		actual, existe := res[id]
		if !existe || VersionMaior(version, actual.Version) {
			res[id] = CatalogoEntrada{Version: version, URL: e.DownloadURL}
		}
	}
	return res
}

// resolverGitHubRepoCompleto lista TODA a raíz de `repo` e devolve unha
// entrada de catálogo por cada id distinto que atope (ver
// listarContidoGitHubRepo + todosOsZipsPorID) — a diferenza de
// resolverGitHubUltimaVersion (que busca un id xa coñecido), isto DESCUBRE
// ids novos sen tocar config.yaml (ver idTodoORepo). Sen rede ou se a API
// falla, devolve ok=false.
func resolverGitHubRepoCompleto(repo string) (map[string]CatalogoEntrada, bool) {
	entradas, ok := listarContidoGitHubRepo(repo)
	if !ok {
		return nil, false
	}
	return todosOsZipsPorID(entradas), true
}

// reVersionNomeFicheiro recoñece un número de versión ao final do nome dun
// .zip, separado por "_" ou "-": "tao_26.3.zip" → "26.3", "tao-1.2.0.zip" →
// "1.2.0". "tao.zip" a secas non ten versión: non hai nada que extraer.
var reVersionNomeFicheiro = regexp.MustCompile(`[_-]([0-9]+(?:\.[0-9]+)+)\.zip$`)

// versionDoNomeFicheiro extrae a versión do nome de ficheiro dunha URL de
// descarga, ou "" se non a segue ese patrón.
func versionDoNomeFicheiro(url string) string {
	nome := url
	if i := strings.LastIndex(nome, "/"); i >= 0 {
		nome = nome[i+1:]
	}
	m := reVersionNomeFicheiro.FindStringSubmatch(nome)
	if m == nil {
		return ""
	}
	return m[1]
}

// normalizarEntrada completa entrada.Version a partir do nome do ficheiro en
// entrada.URL cando non se declarou explícita no catálogo. Unha versión posta
// á man no catálogo sempre gaña (por se o nome do ficheiro non abonda, ex.:
// varios módulos servidos coa mesma URL "latest.zip").
func normalizarEntrada(e CatalogoEntrada) CatalogoEntrada {
	if e.Version == "" {
		e.Version = versionDoNomeFicheiro(e.URL)
	}
	return e
}

// urlDescargaDirecta admite unha URL de GitHub tal cal se copia do navegador
// (https://github.com/<usuario>/<repo>/blob/<rama>/ficheiro.zip) e devolve a
// súa descarga directa en raw.githubusercontent.com. Calquera outra URL
// (piztu.org, releases/download/...) devólvese sen tocar.
func urlDescargaDirecta(url string) string {
	const prefixo = "https://github.com/"
	if strings.HasPrefix(url, prefixo) && strings.Contains(url, "/blob/") {
		resto := strings.Replace(strings.TrimPrefix(url, prefixo), "/blob/", "/", 1)
		return "https://raw.githubusercontent.com/" + resto
	}
	return url
}

// Instalado di se xa hai un módulo (cun modulo.json) en dir/id.
func Instalado(dir, id string) bool {
	_, err := os.Stat(filepath.Join(dir, id, "modulo.json"))
	return err == nil
}

// Instalar descarga e descomprime un módulo en dir/<id> se aínda non o está.
// Se xa está instalado, non fai nada — para substituílo por unha versión
// nova, usa Actualizar.
func Instalar(dir, id string, entrada CatalogoEntrada) error {
	if Instalado(dir, id) {
		return nil
	}
	return baixarEDescomprimir(filepath.Join(dir, id), id, entrada)
}

// Actualizar substitúe un módulo xa instalado pola versión de `entrada`. O
// estado activo/inactivo non se toca: gárdase á parte, no almacén de axustes
// (ver ClaveActivo en modulos.go), non no modulo.json. Se non estaba
// instalado, compórtase coma Instalar.
//
// Descarga PRIMEIRO nun cartafol á parte e só troca ao final, con dous
// renomeados. Antes borraba o módulo e descargaba enriba: se a rede caía no
// medio (ou o .zip viña corrupto) o profesor quedaba sen o módulo que tiña.
// Iso xa era malo cun botón que se preme a man; agora que a actualización é
// automática e periódica (ver App.vixiarActualizacionsModulos) sería un
// módulo desaparecendo só, sen que ninguén tocara nada. Se algo falla antes
// do troco, o instalado nin se entera.
//
// Os cartafoles de traballo van con punto diante para que Externos os ignore
// mentres existen — senón, o "%s.novo" tería un modulo.json co mesmo id e
// aparecería coma un módulo duplicado no intre entre a descarga e o troco.
func Actualizar(dir, id string, entrada CatalogoEntrada) error {
	destino := filepath.Join(dir, id)
	if _, err := os.Stat(destino); err != nil {
		return baixarEDescomprimir(destino, id, entrada) // non estaba: instalación normal
	}

	novo := filepath.Join(dir, "."+id+".novo")
	os.RemoveAll(novo) // restos dun intento anterior interrompido
	defer os.RemoveAll(novo)
	if err := baixarEDescomprimir(novo, id, entrada); err != nil {
		return err // o módulo instalado segue intacto
	}

	// O troco. Apartar o vello en vez de borralo: se o segundo renomeado
	// falla (disco cheo, permisos) aínda se pode devolver ao seu sitio.
	vello := filepath.Join(dir, "."+id+".vello")
	os.RemoveAll(vello)
	if err := os.Rename(destino, vello); err != nil {
		return fmt.Errorf("non se puido apartar a versión anterior de %q: %w", id, err)
	}
	if err := os.Rename(novo, destino); err != nil {
		os.Rename(vello, destino) // desfacer: mellor a versión vella ca ningunha
		return fmt.Errorf("non se puido poñer a versión nova de %q: %w", id, err)
	}
	os.RemoveAll(vello)
	return nil
}

// baixarEDescomprimir é o núcleo común de Instalar e Actualizar: descarga o
// .zip de `entrada`, descomprímeo en `destino` e, se non trae o seu propio
// modulo.json, xérao coa versión do catálogo para que quede rexistrado.
// `destino` chega resolto de fóra (non se compón aquí con dir+id) porque
// Actualizar descarga nun cartafol temporal antes de trocar — ver alí.
func baixarEDescomprimir(destino, id string, entrada CatalogoEntrada) error {
	if entrada.URL == "" {
		return fmt.Errorf("o módulo %q non ten URL de descarga coñecida", id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDescargaTotal)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlDescargaDirecta(entrada.URL), nil)
	if err != nil {
		return fmt.Errorf("non se puido descargar %q: %w", id, err)
	}
	resp, err := clienteDescarga().Do(req)
	if err != nil {
		return fmt.Errorf("non se puido descargar %q: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non se puido descargar %q: HTTP %d", id, resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "piztu-modulo-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpZip.Name())
	if _, err := io.Copy(tmpZip, resp.Body); err != nil {
		tmpZip.Close()
		return fmt.Errorf("erro gardando a descarga de %q: %w", id, err)
	}
	tmpZip.Close()

	if err := os.MkdirAll(destino, 0o755); err != nil {
		return err
	}
	if err := extraerZip(tmpZip.Name(), destino); err != nil {
		return fmt.Errorf("o ficheiro descargado de %q non se puido descomprimir: %w", id, err)
	}
	if err := aplanarSeEnvoltoNunCartafol(destino); err != nil {
		return fmt.Errorf("non se puido preparar %q despois de descomprimir: %w", id, err)
	}

	return completarVersionSeFalta(destino, id, entrada.Version)
}

// completarVersionSeFalta engade "version" ao modulo.json xa extraído, se non
// a trae declarada. É o caso normal: quen escribe o módulo (Tao, por exemplo)
// non ten por que saber nada de piztu.org, e chégalle a quen o publica con
// nomear ben o .zip (ver versionDoNomeFicheiro) — ninguén ten que editar o
// modulo.json á man. Se o zip non trae modulo.json ningún, créase un mínimo.
// Non toca "activo": iso vive no almacén de axustes (ClaveActivo), non aquí.
func completarVersionSeFalta(destino, id, version string) error {
	ficheiro := filepath.Join(destino, "modulo.json")
	datos, err := os.ReadFile(ficheiro)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	m := map[string]any{}
	if len(datos) > 0 {
		if err := json.Unmarshal(datos, &m); err != nil {
			m = map[string]any{}
		}
	}
	if v, ok := m["version"].(string); ok && strings.TrimSpace(v) != "" {
		return nil // xa a declara: non se toca
	}
	if version == "" {
		return nil // non hai versión que poñer (nin declarada nin no nome do ficheiro)
	}
	if _, ok := m["id"]; !ok {
		m["id"] = id
	}
	m["version"] = version

	saida, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ficheiro, saida, 0o644)
}

// VersionMaior compara dúas versións "semver-lite" (números separados por
// puntos, coma "1.2.10"; un "v" inicial ignórase).
//
// Se `local` está baleira ou non se pode interpretar (módulo sen "version"
// declarada no seu modulo.json), calquera `remota` válida cóntase coma máis
// nova: mellor amosar "hai actualización" de máis e animar a declarala ca
// deixar un módulo sen versión eternamente "ao día". Se `remota` non se pode
// interpretar, non hai nada que ofrecer.
func VersionMaior(remota, local string) bool {
	r, okR := partesVersion(remota)
	if !okR {
		return false
	}
	l, okL := partesVersion(local)
	if !okL {
		l = []int{0}
	}
	for i := 0; i < len(r) || i < len(l); i++ {
		var rv, lv int
		if i < len(r) {
			rv = r[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if rv != lv {
			return rv > lv
		}
	}
	return false
}

func partesVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	partes := strings.Split(v, ".")
	res := make([]int, 0, len(partes))
	for _, p := range partes {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, false
		}
		res = append(res, n)
	}
	return res, true
}

// aplanarSeEnvoltoNunCartafol corrixe o caso típico de GitHub: un .zip cuxo
// contido non está na raíz, senón dentro dun único cartafol contedor (ex.:
// "tao/Tao" en vez de "Tao"). Sen isto, un módulo chamado igual có seu propio
// cartafol contedor (como "tao") quedaría instalado un nivel de máis
// (modulos/tao/tao/Tao) e localizarTao non o atoparía.
//
// Só actúa se destino ten exactamente UNHA entrada e é un cartafol: se hai
// varios ficheiros na raíz do zip, xa está "plano" e non se toca nada.
func aplanarSeEnvoltoNunCartafol(destino string) error {
	entradas, err := os.ReadDir(destino)
	if err != nil || len(entradas) != 1 || !entradas[0].IsDir() {
		return nil
	}
	envoltorio := filepath.Join(destino, entradas[0].Name())

	// O contedor múdase primeiro a un nome temporal: se dentro hai un ficheiro
	// co MESMO nome có contedor (ex.: contedor "tao/" cun ficheiro "tao" dentro,
	// coma no caso real que motivou isto), mover eses ficheiros directamente a
	// destino/<nome> chocaría coa propia ruta do contedor a medio mover.
	temporal := envoltorio + ".aplanando"
	if err := os.Rename(envoltorio, temporal); err != nil {
		return err
	}
	internas, err := os.ReadDir(temporal)
	if err != nil {
		return err
	}
	for _, e := range internas {
		orixe := filepath.Join(temporal, e.Name())
		if err := os.Rename(orixe, filepath.Join(destino, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(temporal)
}

// extraerZip descomprime zipPath en destino, evitando "zip slip" (entradas
// que escapen do cartafol destino).
func extraerZip(zipPath, destino string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destinoAbs, err := filepath.Abs(destino)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		rutaAbs, err := filepath.Abs(filepath.Join(destino, f.Name))
		if err != nil || (rutaAbs != destinoAbs && !strings.HasPrefix(rutaAbs, destinoAbs+string(os.PathSeparator))) {
			return fmt.Errorf("entrada insegura no zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(rutaAbs, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(rutaAbs), 0o755); err != nil {
			return err
		}
		if err := extraerFicheiroZip(f, rutaAbs); err != nil {
			return err
		}
	}
	return nil
}

func extraerFicheiroZip(f *zip.File, destino string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(destino, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}
