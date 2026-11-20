// Package historial é o porte de core/historial_tao.py: rexistra quen usou
// cada equipo e cando. Cada asignación equipo↔alumnado (publicada por
// Tao, ver internal/tao) que se manteña estable polo menos
// cfg.Filebrowser.ConfirmarSeg segundos xera un evento "inicio"; cando esa
// asignación remata (cambia de alumnado ou a sesión desaparece), xérase un
// evento "fin". Só se rexistran asignacións que chegaron a confirmarse —
// evita ruído de conexións de proba ou reinicios moi curtos.
//
// Os eventos gárdanse nun ficheiro JSON Lines local por día
// (cfg.BaseDir/historial/<data>.jsonl) e sincronízanse coa mesma ruta
// relativa en File Browser (cfg.Filebrowser.HistorialDir/<data>.jsonl) a
// través da súa API HTTP, autenticada co usuario administrador. O ficheiro
// local é sempre a fonte fiable: se a sincronización falla, o evento xa
// quedou gardado no disco e o seguinte evento tentará subir de novo o
// ficheiro completo do día.
package historial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"piztu/internal/config"
	"piztu/internal/tao"
)

// now é substituíble nos tests para simular o paso do tempo sen esperar de verdade.
var now = time.Now

// ── Cliente mínimo da API de File Browser Quantum ───────────────────────────
//
// Quantum non é un fork compatible do File Browser clásico (contrato comprobado contra
// a v1.5.6): o login é POST /api/auth/login?username=… co contrasinal codificado na
// cabeceira X-Password (devolve o JWT en texto plano, que despois vai como Bearer), e
// as rutas van na query (?source=…&path=/a/b), non na URL.

// fbSource é o nome da "source" de Quantum (server.sources[].name no seu config.yaml)
// que contén cfg.Filebrowser.HistorialDir; a instalación crea exactamente esta, a mesma
// que usan tao/ e pancho/.
const fbSource = "arquivos"

// cabeceiraContrasinal codifica o contrasinal para X-Password, que Quantum descodifica
// como URL: un "ñ" ou un "%" en crú tomaríase por contrasinal erróneo. O espazo vai como
// %20 (non "+") para que valga igual se o servidor o trata como query ou como ruta.
func cabeceiraContrasinal(contrasinal string) string {
	return strings.ReplaceAll(url.QueryEscape(contrasinal), "+", "%20")
}

type clienteFilebrowser struct {
	cfg *config.Config

	mu    sync.Mutex
	token string
	// bloqueadoAte: Quantum bloqueou a conta polo bloqueo de intentos fallidos (ver
	// loginUnha); ata entón non se volve chamar ao login (só o agravaría).
	bloqueadoAte time.Time
}

func novoCliente(cfg *config.Config) *clienteFilebrowser {
	return &clienteFilebrowser{cfg: cfg}
}

// loginIntentos é canto tenta login cando Quantum responde 429. Quantum limita os accesos
// a unhas 8 ás de golpe por IP (despois 429 con "Retry-After: 6") e ignora
// X-Forwarded-For: co proxy Apache toda a aula é un só cliente, e ao comezo da clase o
// alumnado gasta a ráfaga. Sen agardar, o historial dese intre quedaría sen subir.
const loginIntentos = 4

// durmir é time.Sleep, substituíble nos tests.
var durmir = time.Sleep

func (c *clienteFilebrowser) login() string {
	fb := c.cfg.Filebrowser
	if fb.AdminContrasinal == "" {
		fmt.Println("[HISTORIAL] ⚠ Non hai contrasinal configurado en filebrowser.admin_contrasinal; " +
			"o historial gárdase só localmente.")
		return ""
	}
	if time.Now().Before(c.bloqueadoAte) {
		return ""
	}
	for intento := 1; ; intento++ {
		token, espera, bloqueo := c.loginUnha()
		if bloqueo > 0 {
			c.bloqueadoAte = time.Now().Add(bloqueo)
			fmt.Printf("[HISTORIAL] ❌ File Browser bloqueou a conta %q por intentos fallidos de contrasinal (uns %d min): "+
				"non se reintenta ata entón. Comproba que ninguén (Tao, outro equipo) use un contrasinal antigo deste usuario.\n",
				fb.AdminUsuario, int(bloqueo.Minutes()))
			return ""
		}
		if espera == 0 || intento >= loginIntentos {
			return token
		}
		fmt.Printf("[HISTORIAL] Quantum limita os accesos (429); reintento %d/%d en %v\n", intento, loginIntentos-1, espera)
		durmir(espera)
	}
}

// loginUnha é un intento de login. Unha espera non nula significa que Quantum pediu ir máis
// amodo (429 de ráfaga, con "Retry-After" curto). Un bloqueo non nulo é outra cousa: o
// bloqueo da conta por intentos fallidos (429 "too many failed authentication attempts"
// con Retry-After de 15 min), que afecta tamén aos logins CORRECTOS e non se supera
// agardando uns segundos.
func (c *clienteFilebrowser) loginUnha() (string, time.Duration, time.Duration) {
	fb := c.cfg.Filebrowser
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(fb.URL, "/")+"/api/auth/login?username="+url.QueryEscape(fb.AdminUsuario), nil)
	if err != nil {
		return "", 0, 0
	}
	req.Header.Set("X-Password", cabeceiraContrasinal(fb.AdminContrasinal))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("[HISTORIAL] ❌ Non se puido iniciar sesión en File Browser: %v\n", err)
		return "", 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		cru, _ := io.ReadAll(resp.Body)
		if n > 30 || strings.Contains(string(cru), "failed authentication") {
			return "", 0, time.Duration(max(n, 60)) * time.Second
		}
		espera := 6 * time.Second
		if n > 0 {
			espera = time.Duration(n) * time.Second
		}
		return "", espera, 0
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[HISTORIAL] ❌ Login en File Browser devolveu %d\n", resp.StatusCode)
		return "", 0, 0
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", 0, 0
	}
	return strings.Trim(strings.TrimSpace(buf.String()), `"`), 0, 0
}

func (c *clienteFilebrowser) obterToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		c.token = c.login()
	}
	return c.token
}

// escribirFicheiro escribe (sobrescribindo) un ficheiro en File Browser.
func (c *clienteFilebrowser) escribirFicheiro(rutaRelativa string, contido []byte) bool {
	fb := c.cfg.Filebrowser
	for intento := 0; intento < 2; intento++ { // 1 reintento se o token caducou (401)
		token := c.obterToken()
		if token == "" {
			return false
		}
		q := url.Values{}
		q.Set("source", fbSource)
		q.Set("path", "/"+strings.Trim(rutaRelativa, "/"))
		q.Set("override", "true")
		destino := strings.TrimRight(fb.URL, "/") + "/api/resources?" + q.Encode()
		req, err := http.NewRequest(http.MethodPost, destino, bytes.NewReader(contido))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("[HISTORIAL] ❌ Erro subindo %s a File Browser: %v\n", rutaRelativa, err)
			return false
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fmt.Printf("[HISTORIAL] ❌ Erro subindo %s a File Browser: HTTP %d\n", rutaRelativa, resp.StatusCode)
			return false
		}
		return true
	}
	return false
}

// ── Rastrexador de asignacións ───────────────────────────────────────────────

type estadoEquipo struct {
	usuario     string
	nome        string
	grupo       string
	dende       time.Time
	confirmado  bool
	ultimaVista time.Time
}

// Rastrexador segue o estado de cada equipo e xera eventos inicio/fin.
type Rastrexador struct {
	cfg    *config.Config
	fb     *clienteFilebrowser
	estado map[string]*estadoEquipo
}

func Novo(cfg *config.Config) *Rastrexador {
	return &Rastrexador{cfg: cfg, fb: novoCliente(cfg), estado: map[string]*estadoEquipo{}}
}

func (r *Rastrexador) cartafolLocal() string {
	d := filepath.Join(r.cfg.BaseDir, "historial")
	os.MkdirAll(d, 0o755)
	return d
}

func (r *Rastrexador) ficheiroDoDia(cando time.Time) string {
	return filepath.Join(r.cartafolLocal(), cando.Format("2006-01-02")+".jsonl")
}

func (r *Rastrexador) rexistrarEvento(tipo, equipo string, previo *estadoEquipo) {
	cando := now()
	evento := map[string]string{
		"tipo":    tipo,
		"equipo":  equipo,
		"usuario": previo.usuario,
		"nome":    previo.nome,
		"grupo":   previo.grupo,
		"hora":    cando.Format(time.RFC3339),
	}
	linea, _ := json.Marshal(evento)

	ficheiro := r.ficheiroDoDia(cando)
	f, err := os.OpenFile(ficheiro, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("[HISTORIAL] ❌ Non se puido escribir %s: %v\n", ficheiro, err)
		return
	}
	_, werr := f.Write(append(linea, '\n'))
	f.Close()
	if werr != nil {
		fmt.Printf("[HISTORIAL] ❌ Non se puido escribir %s: %v\n", ficheiro, werr)
		return
	}
	usuarioLog := previo.usuario
	if usuarioLog == "" {
		usuarioLog = "—"
	}
	fmt.Printf("[HISTORIAL] %s: %s ← %s\n", tipo, equipo, usuarioLog)

	// Sincroniza o ficheiro completo do día con File Browser (mellor esforzo).
	contido, err := os.ReadFile(ficheiro)
	if err != nil {
		fmt.Printf("[HISTORIAL] ❌ Non se puido ler %s para sincronizar: %v\n", ficheiro, err)
		return
	}
	rutaRemota := strings.TrimRight(r.cfg.Filebrowser.HistorialDir, "/") + "/" + filepath.Base(ficheiro)
	r.fb.escribirFicheiro(rutaRemota, contido)
}

// Procesar compara o estado actual das sesións de Tao co estado previo e
// xera os eventos que corresponda. Chamar periodicamente (poucos segundos).
func (r *Rastrexador) Procesar(sesions map[string]tao.Sesion) {
	agora := now()
	confirmarSeg := time.Duration(r.cfg.Filebrowser.ConfirmarSeg) * time.Second

	equiposVistos := map[string]bool{}
	for equipo, info := range sesions {
		if !info.Vivo || info.Usuario == "" {
			continue
		}
		equiposVistos[equipo] = true
		previo, existe := r.estado[equipo]

		if !existe || previo.usuario != info.Usuario {
			// Nova asignación (ou primeira vez que se ve este equipo con sesión).
			if existe && previo.confirmado {
				r.rexistrarEvento("fin", equipo, previo)
			}
			r.estado[equipo] = &estadoEquipo{
				usuario: info.Usuario, nome: info.Nome, grupo: info.Grupo,
				dende: agora, confirmado: false, ultimaVista: agora,
			}
			continue
		}

		// Mesma asignación xa coñecida: actualizar "última vista" e confirmar
		// se xa pasou o tempo mínimo estable.
		previo.ultimaVista = agora
		if !previo.confirmado && agora.Sub(previo.dende) >= confirmarSeg {
			previo.confirmado = true
			r.rexistrarEvento("inicio", equipo, previo)
		}
	}

	// Equipos que deixaron de estar "vivos": pechar sesión se estaba confirmada.
	for equipo, previo := range r.estado {
		if equiposVistos[equipo] {
			continue
		}
		delete(r.estado, equipo)
		if previo.confirmado {
			r.rexistrarEvento("fin", equipo, previo)
		}
	}
}
