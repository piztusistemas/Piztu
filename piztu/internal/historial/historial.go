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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"piztu/internal/config"
	"piztu/internal/tao"
)

// now é substituíble nos tests para simular o paso do tempo sen esperar de verdade.
var now = time.Now

// ── Cliente mínimo da API de File Browser ───────────────────────────────────

type clienteFilebrowser struct {
	cfg *config.Config

	mu    sync.Mutex
	token string
}

func novoCliente(cfg *config.Config) *clienteFilebrowser {
	return &clienteFilebrowser{cfg: cfg}
}

func (c *clienteFilebrowser) login() string {
	fb := c.cfg.Filebrowser
	if fb.AdminContrasinal == "" {
		fmt.Println("[HISTORIAL] ⚠ Non hai contrasinal configurado en filebrowser.admin_contrasinal; " +
			"o historial gárdase só localmente.")
		return ""
	}
	corpo, _ := json.Marshal(map[string]string{
		"username": fb.AdminUsuario,
		"password": fb.AdminContrasinal,
	})
	resp, err := http.Post(strings.TrimRight(fb.URL, "/")+"/api/login", "application/json", bytes.NewReader(corpo))
	if err != nil {
		fmt.Printf("[HISTORIAL] ❌ Non se puido iniciar sesión en File Browser: %v\n", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[HISTORIAL] ❌ Login en File Browser devolveu %d\n", resp.StatusCode)
		return ""
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return ""
	}
	token := strings.Trim(strings.TrimSpace(buf.String()), `"`)
	return token
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
		url := fmt.Sprintf("%s/api/resources/%s?override=true", strings.TrimRight(fb.URL, "/"), rutaRelativa)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(contido))
		if err != nil {
			return false
		}
		req.Header.Set("X-Auth", token)
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
