// so.go: detección do sistema operativo de cada equipo cliente, para poder
// adaptar as instrucións que se lle envían (un `/sbin/shutdown -h now` de Linux
// non vale nun Windows 11, que quere `shutdown /s /t 0 /f`).
//
// A adaptación real das accións con nome (apagar, durmir, aviso de ruído…)
// faina Salt: cada estado salt/*.sls leva unha rama Jinja
// `{% if grains['kernel'] == 'Windows' %}` que se renderiza xa no minion. Este
// ficheiro é a parte que corre no CONTROLADOR: deixa que piztu saiba de que SO
// é cada equipo sen depender de Salt (para avisar na interface, ou para
// rexeitar con claridade unha operación que aínda non ten equivalente Windows,
// coma o envío de prácticas por SFTP con rutas POSIX).
package motor

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"piztu/internal/inventory"
	"piztu/internal/sshkey"
)

// SO identifica o sistema operativo dun equipo cliente.
type SO string

const (
	SOLinux       SO = "linux"
	SOWindows     SO = "windows"
	SOMac         SO = "mac"
	SODescoñecido SO = "" // minion apagado, sen master, ou grain inesperado
)

// caducidadeSO: canto tempo se fía piztu do valor cacheado. Un equipo pode
// reinstalarse (Linux → Windows ou ao revés) sen que piztu se decate; 10 min é
// o compromiso entre non consultar os grains en cada clic e non quedar pillado
// cun valor vello toda a sesión. InvalidarSOCache fórzao a refrescar antes.
const caducidadeSO = 10 * time.Minute

type entradaSO struct {
	so    SO
	visto time.Time
}

var (
	soMu    sync.RWMutex
	soCache = map[string]entradaSO{}
)

// SODeHost devolve o SO de `host`. Orde de resolución:
//
//  1. Pista explícita no inventario (var por equipo en `hostsFile`, ex.
//     `bac01.local ansible_shell_type=powershell`): manda, sen rede nin caché.
//     É a rede de seguridade para os equipos cuxo minion Salt non responde —
//     ver pistaSOInventario. Nunha aula mixta cada liña leva a súa, así que
//     isto non afecta aos Linux da mesma aula.
//  2. Grains de Salt (`salt <host> grains.item kernel`), cacheados.
//  3. Se o minion non responde ou non hai master: SODescoñecido, e o chamador
//     debe asumir o comportamento Linux de sempre (o caso maioritario).
func SODeHost(hostsFile, host string) SO {
	return soDeHost(inventory.LerHostsConVars(hostsFile)[host], host)
}

// soDeHost é o núcleo de SODeHost coa liña de variables do equipo xa lida
// (para non reler o inventario unha vez por host en SOsDeHosts).
func soDeHost(vars, host string) SO {
	if hint := pistaSOInventario(vars); hint != SODescoñecido {
		return hint
	}

	soMu.RLock()
	e, ok := soCache[host]
	soMu.RUnlock()
	if ok && time.Since(e.visto) < caducidadeSO {
		return e.so
	}

	so := consultarSOSalt(host)

	soMu.Lock()
	soCache[host] = entradaSO{so: so, visto: time.Now()}
	soMu.Unlock()
	return so
}

// SOsDeHosts resolve o SO de varios equipos á vez (reusa a caché de SODeHost).
func SOsDeHosts(hostsFile string, hosts []string) map[string]SO {
	hv := inventory.LerHostsConVars(hostsFile)
	res := make(map[string]SO, len(hosts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			so := soDeHost(hv[host], host)
			mu.Lock()
			res[host] = so
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	return res
}

// pistaSOInventario deduce o SO a partir das variables Ansible dun equipo no
// inventario, SEN tocar a rede. Recoñece:
//
//   - so=windows|linux|mac ou piztu_so=…  → override explícito do profesorado
//   - ansible_shell_type=powershell        → Windows (o marcador clásico)
//   - ansible_connection=winrm|psrp        → Windows
//
// Se non hai ningunha pista devolve SODescoñecido (segue por Salt).
func pistaSOInventario(vars string) SO {
	kv := map[string]string{}
	for _, tok := range strings.Fields(vars) {
		if i := strings.IndexByte(tok, '='); i > 0 {
			kv[strings.ToLower(tok[:i])] = strings.ToLower(tok[i+1:])
		}
	}
	if v := kv["piztu_so"] + kv["so"]; v != "" {
		switch {
		case strings.Contains(v, "windows"):
			return SOWindows
		case strings.Contains(v, "linux"):
			return SOLinux
		case strings.Contains(v, "mac"), strings.Contains(v, "darwin"):
			return SOMac
		}
	}
	if kv["ansible_shell_type"] == "powershell" {
		return SOWindows
	}
	if c := kv["ansible_connection"]; c == "winrm" || c == "psrp" {
		return SOWindows
	}
	return SODescoñecido
}

// InvalidarSOCache esquece o SO cacheado de `host` (ou de todos, se host=="").
// Chámase tras reinstalar/reimaxinar un equipo.
func InvalidarSOCache(host string) {
	soMu.Lock()
	defer soMu.Unlock()
	if host == "" {
		soCache = map[string]entradaSO{}
		return
	}
	delete(soCache, host)
}

// consultarSOSalt pregunta a Salt (grains) e por SSH Á VEZ e queda coa
// primeira resposta coñecida. Antes ían en serie, Salt primeiro: cun minion
// que non responde (bac01), `salt -t 30` tardaba ~60 s en rendirse e "Bloquear"
// semellaba non facer nada; a sonda SSH contesta en menos dun segundo.
func consultarSOSalt(host string) SO {
	fontes := []func() SO{func() SO { return sondarSOSSH(host) }}
	if HaiSaltMaster() {
		fontes = append(fontes, func() SO {
			out, rc := runSalt(host, []string{"grains.item", "kernel", "os_family", "shell"})
			if rc != 0 {
				return SODescoñecido
			}
			return parsearSOGrains(out, host)
		})
	}
	// Canle con búfer: a fonte que remate despois non queda bloqueada.
	res := make(chan SO, len(fontes))
	for _, f := range fontes {
		go func(f func() SO) { res <- f() }(f)
	}
	for range fontes {
		if so := <-res; so != SODescoñecido {
			return so
		}
	}
	return SODescoñecido
}

// cfgSonda: usuario e clave SSH para sondarSOSSH. Gárdaos Get() (o único
// sitio por onde pasa a config antes de calquera acción), porque SODeHost só
// recibe a ruta do inventario.
var (
	cfgSondaMu      sync.RWMutex
	cfgSondaUsuario string
	cfgSondaClave   string
)

func lembrarCredenciaisSonda(usuario, clave string) {
	cfgSondaMu.Lock()
	cfgSondaUsuario, cfgSondaClave = usuario, clave
	cfgSondaMu.Unlock()
}

func sondarSOSSH(host string) SO {
	cfgSondaMu.RLock()
	usuario, clave := cfgSondaUsuario, cfgSondaClave
	cfgSondaMu.RUnlock()
	if clave == "" {
		return SODescoñecido
	}
	if usuario == "" {
		usuario = "usuario"
	}
	out, err := sshkey.SondarSO(host, usuario, clave, 4*time.Second)
	if err != nil {
		return SODescoñecido
	}
	return parsearSondaSO(out)
}

// parsearSondaSO interpreta a saída de `uname -s || ver` (ver sshkey.SondarSO).
func parsearSondaSO(out string) SO {
	o := strings.ToLower(out)
	switch {
	case strings.Contains(o, "microsoft windows"), strings.Contains(o, "windows_nt"):
		return SOWindows
	case strings.Contains(o, "darwin"):
		return SOMac
	case strings.Contains(o, "linux"):
		return SOLinux
	// Reserva: só a mensaxe de erro de PowerShell (ex. `cmd /c ver` sen
	// resposta). Ningunha shell de Linux/macOS imprime estes marcadores.
	case strings.Contains(o, "categoryinfo"), strings.Contains(o, "fullyqualifiederrorid"):
		return SOWindows
	}
	return SODescoñecido
}

// parsearSOGrains extrae o SO da saída JSON de
// `salt <host> grains.item kernel os_family shell`, coa forma
// {"<host>": {"kernel": "...", "os_family": "...", "shell": "..."}}.
//
// `kernel` é o discriminante limpo, pero nalgúns minions Windows o subsistema
// de grains que depende de WMI falla e devolve `kernel`/`os_family` baleiros
// (visto en produción, Salt 3008 onedir con WMI roto). Nese caso recórrese ao
// grain `shell` (`C:\WINDOWS\system32\cmd.exe` en Windows), que non depende de
// WMI. Illado nunha función propia para probalo sen un salt-master real.
func parsearSOGrains(stdout, host string) SO {
	var data map[string]map[string]any
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return SODescoñecido
	}
	ret, ok := data[host]
	if !ok {
		return SODescoñecido
	}
	str := func(k string) string {
		v, _ := ret[k].(string)
		return strings.ToLower(strings.TrimSpace(v))
	}
	switch str("kernel") {
	case "windows":
		return SOWindows
	case "linux":
		return SOLinux
	case "darwin":
		return SOMac
	}
	// kernel baleiro/inesperado → reserva sen dependencia de WMI.
	if str("os_family") == "windows" || strings.Contains(str("shell"), "cmd.exe") {
		return SOWindows
	}
	if sh := str("shell"); strings.HasPrefix(sh, "/") {
		return SOLinux
	}
	return SODescoñecido
}
