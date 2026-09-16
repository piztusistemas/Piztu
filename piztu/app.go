package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"piztu/internal/actualizacion"
	"piztu/internal/api"
	"piztu/internal/config"
	"piztu/internal/db"
	"piztu/internal/discover"
	"piztu/internal/i18n"
	"piztu/internal/inventory"
	"piztu/internal/modulos"
	"piztu/internal/motor"
	"piztu/internal/ping"
	"piztu/internal/recolector"
	"piztu/internal/ruido"
	"piztu/internal/saltsetup"
	"piztu/internal/sshkey"
	"piztu/internal/sshterm"
	"piztu/internal/tao"
	"piztu/recursos"
)

// App é o núcleo de Piztu: expón métodos chamables desde o frontend (bindings)
// e emite eventos de runtime. Substitúe a app.py + blueprints/*.
type App struct {
	cfg   *config.Config
	store *db.Store
	tr    *i18n.Translator
	ssh   *sshterm.Manager
	ruido *ruido.Monitor
	ping  *ping.Monitor
	tao   *tao.Monitor
	recol *recolector.Monitor

	// acendidos garda a última foto do monitor de ping. O monitor só emite
	// eventos ao frontend; para poder pasarlle o estado a un módulo hai que
	// lembralo aquí.
	acendidosMu sync.Mutex
	acendidos   map[string]bool

	// catalogo é a última foto do índice de módulos descargables (piztu.org +
	// catalogo_local), refrescada en fondo (ver refrescarCatalogo). Lese en
	// cada repintado de ⚙ Aula → Módulos SEN tocar a rede — evita que abrir
	// esa ventá ou volver ao foco de piztu quede á espera dunha resposta.
	catalogoMu sync.Mutex
	catalogo   map[string]modulos.CatalogoEntrada

	// api é o servidor da API oficial de módulos (internal/api, ver
	// docs/api-modulos.md). Substitúe o vello camiño Xesta→app.py→
	// blueprints/xesta.py: agora vive nativo aquí, sen precisar arrincar
	// python3 en fondo (ver escribirContexto e SetModulo).
	api *api.Servidor

	// actualizacion é a última foto de se hai unha versión nova de Piztu
	// publicada en GitHub (ver comprobarActualizacionPiztu). Consultada en
	// fondo ao arrincar, para non atrasar a apertura de piztu se GitHub non
	// responde.
	actualizacionMu sync.Mutex
	actualizacion   actualizacion.Estado

	// saltMasterResolto é o enderezo do salt-master xa validado. Deducilo
	// implica resolucións DNS que poden tardar varios segundos cada unha se o
	// nome non resolve (ver saltsetup.EnderezoMasterValidado); faise unha soa
	// vez en fondo ao arrincar (ver resolverSaltMaster) para que abrir ⚙ Aula
	// —que chama a GetAulaConfig— non quede bloqueado agardando por elas.
	saltMasterMu      sync.Mutex
	saltMasterResolto string

	// procesos garda, por ID de módulo, o último proceso lanzado por
	// AbrirModulo mentres siga vivo — así unha segunda chamada (o profesorado
	// preme outra vez o botón) pode pedirlle foco en vez de abrir outra
	// instancia (ver lanzarAplicacion). Só módulos lanzados por AbrirModulo
	// (Xesta, Pancho...): AbrirTao ten o seu propio camiño, sen este rexistro.
	procesosMu sync.Mutex
	procesos   map[string]*procesoModulo
}

// procesoModulo é un proceso lanzado por lanzarAplicacion e o xeito de saber
// se segue vivo sen ter que sinalizalo (máis portable ca syscall.Signal: en
// Windows os.Process.Signal só soporta os.Interrupt/os.Kill). feito péchase
// dende a goroutine que xa facía cmd.Wait() para non deixar zombies.
type procesoModulo struct {
	cmd   *exec.Cmd
	feito chan struct{}
}

func (p *procesoModulo) vivo() bool {
	select {
	case <-p.feito:
		return false
	default:
		return true
	}
}

func NewApp() *App { return &App{} }

// emitirEvento envía un evento ao frontend (substitúe wruntime.EventsEmit(a.ctx,
// ...) de v2 - v3 non precisa ctx). A garda application.Get() != nil cobre os
// mesmos casos ca "if a.ctx != nil" de antes (chamado dende un test ou antes
// de que ServiceStartup remate) sen ter que repetila en cada un dos ~49
// puntos de chamada.
func emitirEvento(nome string, dato any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(nome, dato)
	}
}

// init rexistra o TIPO de cada evento (obrigatorio en Wails v3,
// application.RegisterEvent[T] - Emit sen isto rexistrado falla). Un tipo
// por nome de evento, o mesmo que xa se lle pasaba a wruntime.EventsEmit en
// v2 - a inmensa maioría son map[string]any; os tres que non seguen esa
// forma (estado_tao, publicacions_modulo, actualizacion_piztu_cambiada)
// levan o tipo real que emiten os seus monitores (ver iniciarTao/
// iniciarRecolectores/comprobarActualizacionPiztu). Chamadas individuais
// (non un bucle sobre unha lista de nomes): "wails3 generate bindings"
// analiza o código de forma estática para descubrir os eventos - un nome
// que non sexa un literal constante (ex. unha variable de bucle) non se
// detecta, e o evento queda sen o wrapper tipado no JS xerado (confirmado
// co aviso "non-constant event name" a primeira vez que se probou o bucle).
func init() {
	application.RegisterEvent[actualizacion.Estado]("actualizacion_piztu_cambiada")
	application.RegisterEvent[map[string]tao.Sesion]("estado_tao")
	application.RegisterEvent[map[string]map[string]string]("publicacions_modulo")
	application.RegisterEvent[map[string]any]("aula_salt_master_resolto")
	application.RegisterEvent[map[string]any]("ansible_setup_completo")
	application.RegisterEvent[map[string]any]("ansible_setup_log")
	application.RegisterEvent[map[string]any]("clave_completo")
	application.RegisterEvent[map[string]any]("clave_hosts_completo")
	application.RegisterEvent[map[string]any]("clave_hosts_log")
	application.RegisterEvent[map[string]any]("clave_log")
	application.RegisterEvent[map[string]any]("comando_completo")
	application.RegisterEvent[map[string]any]("comando_resultado")
	application.RegisterEvent[map[string]any]("descubrimento_completo")
	application.RegisterEvent[map[string]any]("descubrimento_log")
	application.RegisterEvent[map[string]any]("envio_completo")
	application.RegisterEvent[map[string]any]("envio_individualizado_completo")
	application.RegisterEvent[map[string]any]("modulos_cambiado")
	application.RegisterEvent[map[string]any]("motor_cambiado")
	application.RegisterEvent[map[string]any]("ping_estado")
	application.RegisterEvent[map[string]any]("progreso_envio")
	application.RegisterEvent[map[string]any]("progreso_envio_individualizado")
	application.RegisterEvent[map[string]any]("progreso_recollida")
	application.RegisterEvent[map[string]any]("recollida_completa")
	application.RegisterEvent[map[string]any]("ruido_actual")
	application.RegisterEvent[map[string]any]("ruido_notificacion")
	application.RegisterEvent[map[string]any]("salt_setup_completo")
	application.RegisterEvent[map[string]any]("salt_setup_log")
	application.RegisterEvent[map[string]any]("scan_completo")
	application.RegisterEvent[map[string]any]("scan_log")
	application.RegisterEvent[map[string]any]("ssh_closed")
	application.RegisterEvent[map[string]any]("ssh_data")
	application.RegisterEvent[map[string]any]("tao_clientes_completo")
	application.RegisterEvent[map[string]any]("tao_clientes_log")
}

// ServiceStartup é o hook de ciclo de vida que Wails v3 chama unha soa vez
// ao arrincar (interface application.ServiceStartup) - equivalente ao
// OnStartup(ctx) de v2. Xa non fai falla gardar ctx (v3 non o precisa para
// diálogos/eventos, ver emitirEvento e o único diálogo en SelectFicheiros).
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	arranxarPATH()
	cfg, err := config.Load()
	if err != nil {
		println("Erro cargando config:", err.Error())
		cfg, _ = config.Load() // segunda oportunidade con defaults
	}
	a.cfg = cfg

	store, err := db.Open(cfg)
	if err != nil {
		println("Erro abrindo BD:", err.Error())
	}
	a.store = store

	// API oficial de módulos (internal/api, ver docs/api-modulos.md): un só
	// socket para todos os módulos con proceso propio (Xesta hoxe). Un fallo
	// aquí non é fatal — Piztu segue funcionando, só os módulos que a
	// precisen quedarán sen poder falar con el.
	//
	// axustesAPI (non a.store directamente): se db.Open fallou, a.store é un
	// *db.Store nil — metido tal cal nun parámetro de tipo interface
	// (modulos.Axustes) deixaría de ser == nil (a interface leva o tipo
	// dinámico *db.Store aínda que o valor sexa nil), e modulos.Activo
	// chamaría un método sobre un punteiro nil. Esta variable intermedia
	// mantén a interface realmente nil nese caso.
	var axustesAPI modulos.Axustes
	if a.store != nil {
		axustesAPI = a.store
	}
	a.api = api.Novo(cfg, axustesAPI)
	if err := a.api.Escoitar(); err != nil {
		println("Aviso arrincando a API de módulos:", err.Error())
	}

	// Despregar os playbooks embebidos en base_dir/playbooks se faltan (primeira
	// execución, sobre todo en macOS onde base_dir arranca baleiro). Necesarios
	// para o motor Ansible; o SSH non os precisa.
	if n, err := recursos.DesplegarPlaybooks(cfg.PlaybooksDir); err != nil {
		println("Aviso despregando playbooks:", err.Error())
	} else if n > 0 {
		println("Despregados", n, "playbooks en", cfg.PlaybooksDir)
	}

	// O mesmo cos estados .sls, que precisan os motores Salt e Salt-SSH.
	if n, err := recursos.DesplegarEstadosSalt(cfg.SaltDir); err != nil {
		println("Aviso despregando estados Salt:", err.Error())
	} else if n > 0 {
		println("Despregados", n, "estados Salt en", cfg.SaltDir)
	}

	// Crear o cartafol de módulos con instrucións dentro. Se non existe, o
	// profesor non ten onde soltar un apeiro novo nin de onde deducir o formato.
	if err := modulos.Preparar(cfg.ModulosDir); err != nil {
		println("Aviso preparando o cartafol de módulos:", err.Error())
	}

	// Catálogo de módulos descargables (piztu.org): en fondo, para non atrasar
	// a apertura de piztu se piztu.org non responde. Avisa ⚙ Aula → Módulos
	// (evento modulos_cambiado) en canto remate, para que "Descargar" apareza
	// sen ter que ir e vir da ventá. E, se o cartafol está baleiro de todo,
	// baixa os módulos sen agardar a que ninguén prema nada (ver sementarModulos).
	go func() {
		a.refrescarCatalogo()
		a.sementarModulos()
		a.actualizarModulosAoDia()
		// E seguir comprobando mentres piztu estea aberto: un ordenador de
		// aula queda acendido todo o día, así que só mirar no arranque
		// deixaría sen aplicar calquera versión publicada pola mañá.
		a.vixiarActualizacionsModulos()
	}()

	// Versión de Piztu: en fondo tamén, mesmo motivo (ver
	// comprobarActualizacionPiztu). Se hai unha nova, o botón "i" da barra
	// amosa un bordo vermello (evento actualizacion_piztu_cambiada).
	go a.comprobarActualizacionPiztu()

	// Enderezo do salt-master: tamén en fondo, porque validalo pode implicar
	// varias consultas DNS con timeout (ver resolverSaltMaster). Así abrir
	// ⚙ Aula é inmediato; o campo enche/corríxese co evento
	// aula_salt_master_resolto se aínda non estaba listo.
	go a.resolverSaltMaster()

	// Tao é un apeiro externo coma Xesta (PorDefecto=false): non se activa só.
	// É unha ferramenta auxiliar (probablemente de pago); o profesor pode
	// descargala dende ⚙ Aula → Módulos se está no catálogo, ou copiala á man
	// en ModulosDir/tao/ se non o está. Activala é decisión súa nese mesmo sitio.

	// E os idiomas: sen eles a interface cae nos textos de reserva do código
	// (ver recursos.DesplegarIdiomas).
	if n, err := recursos.DesplegarIdiomas(filepath.Join(cfg.BaseDir, "idiomas")); err != nil {
		println("Aviso despregando idiomas:", err.Error())
	} else if n > 0 {
		println("Despregados", n, "ficheiros de idioma")
	}

	// O idioma persistido en internal/db (chave "idioma", ver SetIdioma) gaña sobre
	// cfg.Idioma se existe — así un cambio feito en ⚙ Configuración sobrevive a un
	// reinicio sen ter que reescribir config.yaml. Sen BD (a.store == nil) ou sen
	// axuste gardado aínda, cae en cfg.Idioma coma sempre.
	idiomaInicial := cfg.Idioma
	if a.store != nil {
		idiomaInicial = a.store.LerAxuste("idioma", cfg.Idioma)
	}
	a.tr = i18n.New(dirsIdiomas(cfg), idiomaInicial)
	a.ssh = sshterm.NewManager(cfg)

	a.rexistrarAccionsModulos()

	a.configurarRuido()
	a.iniciarPing()
	a.iniciarRecolectores()

	// Os monitores dos módulos só arrancan se o seu módulo está activo: un
	// módulo apagado non debe seguir consumindo rede nin micrófono só porque a
	// súa interface estea oculta.
	if a.moduloActivo(modulos.Tao) {
		a.iniciarTao()
	}
	if a.moduloActivo(modulos.Ruido) && a.ruidoAutoarranque() {
		// SoEscoita: ao abrir Piztu o micrófono pode alimentar o vúmetro, pero
		// o control de ruído queda DESARMADO. Armalo é sempre un xesto
		// explícito do profesorado (IniciarRuido ← 3s na icona do micrófono).
		a.ruido.Iniciar(ruido.SoEscoita)
	}
	return nil
}

// ServiceShutdown é o hook de ciclo de vida que Wails v3 chama ao pechar
// Piztu (interface application.ServiceShutdown) - equivalente a
// OnShutdown(ctx) de v2.
func (a *App) ServiceShutdown() error {
	if a.api != nil {
		a.api.Pechar()
	}
	return nil
}

// iniciarTao arranca o monitor de sesións de Tao (porte de
// core/tao.py + blueprints/tao.py): lee periodicamente quen está a usar
// cada equipo e notifica o mapa por evento, coma o resto de monitores.
func (a *App) iniciarTao() {
	a.tao = tao.Novo(a.cfg, tao.Eventos{
		OnEstado: func(sesions map[string]tao.Sesion) {
			emitirEvento("estado_tao", sesions)
		},
	})
	a.tao.Iniciar()
}

// GetEstadoTao devolve o estado actual das sesións de Tao
// (← GET /estado_tao), para a carga inicial do mapa antes do primeiro
// evento periódico.
func (a *App) GetEstadoTao() map[string]tao.Sesion {
	return tao.Listar(a.cfg)
}

// AbrirTao abre Tao (instalado polo Paso 7 opcional do instalador,
// en BaseDir/tao/) coma un proceso independente (← abrir_tao de
// main.py). Devolve erro se non está instalado, para que o frontend poida
// avisar o profesor. O peche (ou non) de Piztu decídeo o frontend: por
// defecto substitúe a Piztu (chama a Quit() despois), agás con Ctrl+clic.
func (a *App) AbrirTao() error {
	if !a.moduloActivo(modulos.Tao) {
		return errModuloInactivo(modulos.Tao)
	}
	ruta, ok := a.localizarTao()
	if !ok {
		dir := filepath.Join(a.cfg.ModulosDir, "tao")
		nome := "tao"
		if runtime.GOOS == "darwin" {
			nome = "Tao.app"
		}
		return fmt.Errorf("Tao non atopado. Crea o cartafol %s e garda dentro o %s (ruta final: %s)",
			dir, nome, filepath.Join(dir, nome))
	}

	// Tao é o único módulo interno que non pasaba por escribirContexto: como
	// ata agora nunca precisara de PIZTU_CONTEXTO (é un caso á parte de
	// AbrirModulo, con .app/log propios en vez do lanzamento xenérico de
	// abaixo), nunca se lle escribira. Agora si o precisa —
	// AbrirYangParaTarefa (tao/yang_launch.go) le base_dir de aí para
	// localizar Yang — así que se prepara igual que para calquera outro
	// módulo, best-effort: se falla (Tao aínda non rexistrado coma módulo
	// externo, por raro que sexa) Tao ábrese igual, só sen esa función.
	var contexto string
	if m, ok := modulos.Buscar(a.cfg.ModulosDir, modulos.Tao); ok {
		if c, err := a.escribirContexto(m); err == nil {
			contexto = c
		}
	}

	var cmd *exec.Cmd
	var log *os.File
	if runtime.GOOS == "darwin" {
		// En macOS Tao é un bundle .app: lánzase con `open` (xestiona o bundle
		// e a activación correctamente).
		if contexto != "" {
			cmd = exec.Command("open", ruta, "--args", "--piztu-contexto", contexto)
		} else {
			cmd = exec.Command("open", ruta)
		}
	} else {
		if contexto != "" {
			cmd = exec.Command(ruta, "--piztu-contexto", contexto)
		} else {
			cmd = exec.Command(ruta)
		}
		cmd.Dir = filepath.Dir(ruta)
		// Sen isto, calquera erro de Tao ao arrincar (permisos, biblioteca
		// gráfica que falta, etc.) desaparece: por defecto exec.Cmd manda
		// stdout/stderr do fillo a /dev/null, e como Piztu se peche xusto
		// despois (comportamento por defecto do botón), non queda rastro
		// ningún do fallo. Gárdase en TmpDir para poder diagnosticalo.
		if f, err := os.Create(filepath.Join(a.cfg.TmpDir, "tao-abrir.log")); err == nil {
			log = f
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	if contexto != "" {
		cmd.Env = append(os.Environ(), "PIZTU_CONTEXTO="+contexto)
	}
	if err := cmd.Start(); err != nil {
		if log != nil {
			log.Close()
		}
		return err
	}
	// Recolle o proceso fillo cando remate para non deixar zombies acumulándose cada vez
	// que se abre/pecha Tao dende este botón (piztu queda aberto moito tempo).
	go func() {
		cmd.Wait()
		if log != nil {
			log.Close()
		}
	}()
	return nil
}

// localizarTao devolve a ruta do Tao instalado. En macOS busca o bundle Tao.app
// (base_dir, /Applications ou canda piztu.app); noutros sistemas, o binario
// "tao" en base_dir (layout do instalador de Linux).
func (a *App) localizarTao() (string, bool) {
	if runtime.GOOS == "darwin" {
		cands := []string{
			// Sitio canónico a partir de agora: Tao é un apeiro coma outro
			// calquera e o seu sitio é o cartafol de módulos. As rutas de
			// abaixo mantéñense para non romper instalacións anteriores.
			filepath.Join(a.cfg.ModulosDir, "tao", "Tao.app"),
			filepath.Join(a.cfg.BaseDir, "Tao.app"),
			"/Applications/Tao.app",
		}
		// Irmán do propio .app en execución:
		// …/piztu.app/Contents/MacOS/piztu → …/<dir>/Tao.app
		if exe, err := os.Executable(); err == nil {
			dirApp := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(exe))))
			cands = append(cands, filepath.Join(dirApp, "Tao.app"))
		}
		for _, c := range cands {
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}
		return "", false
	}
	for _, p := range []string{
		filepath.Join(a.cfg.ModulosDir, "tao", "tao"),
		filepath.Join(a.cfg.BaseDir, "tao"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// iniciarPing arranca o monitor de ping (porte Go de main.py:_iniciar_ping_loop),
// que comproba periodicamente por ICMP se cada equipo do mapa está acendido.
func (a *App) iniciarPing() {
	equiposFn := func() []string { return motor.Get(a.cfg, "").GetEquipos() }
	a.ping = ping.Novo(a.cfg, equiposFn, ping.Eventos{
		OnEstado: func(host string, online bool) {
			a.acendidosMu.Lock()
			if a.acendidos == nil {
				a.acendidos = map[string]bool{}
			}
			a.acendidos[host] = online
			a.acendidosMu.Unlock()
			emitirEvento("ping_estado", map[string]any{"host": host, "online": online})
		},
	})
	a.ping.Iniciar()
}

// iniciarRecolectores arranca o monitor que executa as ordes declaradas polos
// módulos activos. A lista consúltase en cada volta, así que acender ou apagar
// un módulo ten efecto sen reiniciar.
func (a *App) iniciarRecolectores() {
	modulosFn := func() []modulos.Modulo {
		var res []modulos.Modulo
		for _, m := range modulos.Todos(a.cfg.ModulosDir) {
			if m.Recolector != nil && a.moduloActivo(m.ID) {
				res = append(res, m)
			}
		}
		return res
	}
	a.recol = recolector.Novo(a.cfg, modulosFn, recolector.Eventos{
		OnPublicacions: func(pub map[string]map[string]string) {
			emitirEvento("publicacions_modulo", pub)
		},
	})
	a.recol.Iniciar()
}

// GetPublicacions devolve a foto actual das publicacións dos módulos, para a
// carga inicial do mapa antes do primeiro evento periódico (mesmo patrón ca
// GetEstadoTao).
func (a *App) GetPublicacions() map[string]map[string]string {
	if a.recol == nil {
		return map[string]map[string]string{}
	}
	return a.recol.Publicacions()
}

// configurarRuido crea o monitor de ruído (porte Go de ruido.py) pero non o
// arrinca: quen decide iso é startup(), segundo o módulo estea activo e o
// axuste `ruido_autoarranque` (ver ruidoAutoarranque, que explica o cambio de
// criterio respecto de main.py).
func (a *App) configurarRuido() {
	umbralFn := func() int {
		if a.store == nil {
			return a.cfg.Ruido.UmbralDefecto
		}
		return a.store.LerUmbral()
	}
	a.ruido = ruido.Novo(a.cfg, umbralFn, ruido.Eventos{
		OnVumetro: func(e ruido.Estado) {
			emitirEvento("ruido_actual", map[string]any{
				"nivel": e.Nivel, "nivel_medio": e.NivelMedio, "estado": e.Estado,
			})
		},
		OnNotificacion: func(msg, tipo string) {
			emitirEvento("ruido_notificacion", map[string]any{
				"msg": msg, "tipo": tipo,
			})
		},
	})
}

// ── Módulos (os "apeiros" do tractor) ────────────────────────────────────────

// moduloActivo di se un módulo está acendido. Compróbase no BACKEND, non só
// escondendo a interface: os métodos de App son bindings de Wails e seguen sendo
// chamables aínda que o botón non se vexa.
func (a *App) moduloActivo(id string) bool {
	if a.store == nil {
		// Sen base de datos non hai estado gardado: vale o defecto do rexistro
		// (m.PorDefecto), que agora deixa todo apagado ata que o profesor o active.
		m, ok := modulos.Buscar(a.cfg.ModulosDir, id)
		return ok && m.PorDefecto
	}
	return modulos.ActivoPorID(a.store, a.cfg.ModulosDir, id)
}

// errModuloInactivo é a resposta única para calquera acción dun módulo apagado.
func errModuloInactivo(id string) error {
	return fmt.Errorf("o módulo %q está desactivado (⚙ Aula → Módulos)", id)
}

// ruidoAutoarranque di se o micrófono debe poñerse a ESCOITAR ao abrir piztu.
// Escoitar só enche o vúmetro: as accións sobre a aula van aparte (ver
// ruido.Modo), así que isto nunca fai que se bloquee nin se avise a ninguén.
//
// Historia deste axuste, que convén non repetir: durante un tempo valeu "1" por
// defecto E arrincaba o monitor completo. Como a clave nin sequera existía na
// táboa `axuste` (non hai interface que a escriba), o resultado era que o
// control de ruído se armaba só ao abrir Piztu, coa icona do micrófono apagada,
// e a aula bloqueábase e desbloqueábase soa cada poucos minutos —os avisos
// "EQUIPO DESBLOQUEADO" que ninguén pedira. Agora son dúas cousas distintas:
// este axuste (por defecto "0", nada de micrófono) e armar, que só pode facelo
// o profesorado mantendo premida a icona 3s.
func (a *App) ruidoAutoarranque() bool {
	if a.ruido == nil {
		return false
	}
	if a.store == nil {
		return false
	}
	return a.store.LerAxuste("ruido_autoarranque", "0") == "1"
}

// ModuloInfo describe un módulo para a ventá de configuración.
type ModuloInfo struct {
	ID    string `json:"id"`
	Clave string `json:"clave"`
	Nome  string `json:"nome"`
	Icona string `json:"icona"`
	// IconaImaxeDataURI, cando non baleiro, substitúe Icona por unha imaxe
	// (ver modulos.Modulo.IconaImaxeDataURI) nos botóns do frontend.
	IconaImaxeDataURI string `json:"iconaImaxeDataURI"`
	Activo            bool   `json:"activo"`
	Interno           bool   `json:"interno"`
	Dispo             bool   `json:"dispo"`
	Motivo            string `json:"motivo"`
	Instalado         bool   `json:"instalado"`
	// Novo marca un módulo que aínda non se activou nin desactivou nunca: é o
	// que acaba de aparecer no cartafol e agarda decisión.
	Novo bool `json:"novo"`
	// TenPanel indica que o módulo trae unha pantalla de configuración propia.
	TenPanel bool `json:"ten_panel"`
	// Ruta é onde vive o ficheiro do módulo, ou "" se non ten ningún porque vai
	// dentro de piztu. Amósase na lista para que se entenda de vista para que
	// serve o cartafol de módulos e que hai (ou non) dentro.
	Ruta string `json:"ruta"`
	// Descargable indica que o módulo está no catálogo de piztu.org e pode
	// instalarse premendo "Descargar", en vez de copialo á man.
	Descargable bool `json:"descargable"`
	// Version é a declarada no modulo.json instalado (baleira se o módulo non
	// a declara). VersionNova é a do catálogo cando é máis nova ca instalada
	// (ver modulos.VersionMaior) — só ten sentido xunto con Actualizable.
	Version      string `json:"version"`
	VersionNova  string `json:"version_nova"`
	Actualizable bool   `json:"actualizable"`
	// Autor, Email e Web son metadatos informativos que o módulo declara no
	// seu modulo.json (opcionais).
	Autor string `json:"autor"`
	Email string `json:"email"`
	Web   string `json:"web"`
	// Descricion é o texto curto opcional que o módulo declara no seu
	// modulo.json, amosado xunto ao nome en ⚙ Aula → Módulos.
	Descricion string `json:"descricion"`
	// MantenPiztuAberto — ver modulos.Modulo.MantenPiztuAberto. Lido polo
	// frontend para decidir se peche Piztu ao abrir este módulo (ver
	// pintarBotonsExternos en main.js).
	MantenPiztuAberto bool `json:"mantenPiztuAberto"`
}

// ModulosDispo devolve todos os módulos co seu estado, para a lista de caselas
// da ventá ⚙ Aula. Mesmo patrón ca MotoresDispo().
func (a *App) ModulosDispo() []ModuloInfo {
	var res []ModuloInfo
	vistos := map[string]bool{}
	for _, m := range modulos.Todos(a.cfg.ModulosDir) {
		vistos[m.ID] = true
		info := ModuloInfo{
			ID:                m.ID,
			Clave:             m.ClaveI18n,
			Nome:              m.Nome,
			Icona:             m.Icona,
			IconaImaxeDataURI: m.IconaImaxeDataURI,
			Interno:           m.Interno,
			Dispo:             true,
			Instalado:         true,
			MantenPiztuAberto: m.MantenPiztuAberto,
		}
		// A tradución faise no frontend con t(clave, nome): así o nome de
		// reserva vale igual se non hai ficheiros de idioma no equipo.
		if a.store == nil {
			info.Activo = m.PorDefecto
		} else {
			info.Activo = modulos.Activo(a.store, m)
			// Sen entrada gardada, é un módulo que nunca se tocou.
			info.Novo = a.store.LerAxuste(modulos.ClaveActivo(m.ID), "") == ""
		}
		if !m.Interno {
			if m.Executable != "" {
				info.Ruta = filepath.Join(m.Dir, m.Executable)
			}
			info.TenPanel = modulos.LerPanel(m) != nil
			info.Version = m.Version
			info.Autor = m.Autor
			info.Email = m.Email
			info.Web = m.Web
			info.Descricion = m.Descricion
		}
		res = append(res, a.completarInfoExterna(info))
	}

	// Calquera módulo do catálogo aínda sen instalar (Tao, Xesta, ou un futuro
	// terceiro) amósase igual: sen cartafol nin modulo.json, modulos.Todos non
	// o atopa (non é interno), así que sen isto o profesor non tería onde
	// premer "Descargar" a primeira vez. Iterar en orde fixa (non a dun mapa)
	// para que a lista non cambie de orde entre repintados por nada.
	//
	// Instalado: false é o que fai que completarInfoExterna marque Descargable
	// (non Actualizable): estes rexistros son módulos SEN cartafol nesta
	// máquina, non hai ningunha versión local que comparar. Con Instalado:true
	// caían na rama de actualización e, ao ter Version baleira, VersionMaior
	// dábaos sempre por desactualizados → botón "Actualizar" en falso (só Tao
	// se libraba, polo seu caso especial en completarInfoExterna).
	catalogo := a.catalogoCache()
	idsCatalogo := make([]string, 0, len(catalogo))
	for id := range catalogo {
		idsCatalogo = append(idsCatalogo, id)
	}
	sort.Strings(idsCatalogo)
	for _, id := range idsCatalogo {
		if vistos[id] {
			continue
		}
		vistos[id] = true
		res = append(res, a.completarInfoExterna(ModuloInfo{
			ID: id, Nome: nomeReservaModulo(id), Icona: iconaReservaModulo(id),
			Dispo: true, Instalado: false,
		}))
	}
	return res
}

// nomeReservaModulo/iconaReservaModulo son o nome e a icona a amosar dun
// módulo do catálogo do que aínda non se sabe nada máis (nin instalado, así
// que non hai modulo.json propio que consultar). Para os que Piztu coñece de
// antemán (Tao, Xesta) hai un valor mellor ca o id a secas.
func nomeReservaModulo(id string) string {
	switch id {
	case modulos.Tao:
		return "Tao"
	case xestaModuloID:
		return "Xesta"
	default:
		return id
	}
}

func iconaReservaModulo(id string) string {
	switch id {
	case modulos.Tao:
		return "🧭"
	case xestaModuloID:
		return "🤖"
	default:
		return "📦"
	}
}

// completarInfoExterna resolve o estado real dun módulo externo: localiza Tao
// polas súas rutas propias (bundle .app en macOS, instalacións anteriores…) e
// mira se está no catálogo de piztu.org, para ofrecer descarga automática
// (módulo non instalado) ou actualización (módulo instalado cunha versión
// máis vella ca publicada) en vez de só o aviso de copialo á man.
func (a *App) completarInfoExterna(info ModuloInfo) ModuloInfo {
	if info.ID == modulos.Tao {
		if ruta, ok := a.localizarTao(); ok {
			info.Ruta = ruta
		} else {
			info.Instalado = false
			destino := filepath.Join(a.cfg.ModulosDir, "tao")
			if runtime.GOOS == "darwin" {
				info.Motivo = "non instalado: copia Tao.app a " + destino
			} else {
				info.Motivo = "non instalado: copia o binario \"tao\" a " + destino
			}
		}
	}

	entrada, noCatalogo := a.catalogoCache()[info.ID]
	if !info.Instalado {
		if noCatalogo {
			info.Descargable = true
			info.Motivo = "dispoñible para descargar"
		}
		return info
	}
	if noCatalogo {
		info.VersionNova = entrada.Version
		info.Actualizable = modulos.VersionMaior(entrada.Version, info.Version)
	}
	return info
}

// ── Catálogo de módulos descargables (piztu.org) ─────────────────────────────

// refrescarCatalogo pide o índice de módulos descargables (ver
// internal/modulos.IndiceRemoto) e avisa ⚙ Aula → Módulos se cambiou algo.
// Chámase en fondo desde startup(): ModulosDispo() só le a foto gardada aquí,
// nunca toca a rede, para que abrir esa ventá non quede á espera de piztu.org.
func (a *App) refrescarCatalogo() {
	catalogo := modulos.IndiceRemoto(a.cfg.Modulos.IndiceURL, a.cfg.Modulos.CatalogoLocal)
	a.catalogoMu.Lock()
	a.catalogo = catalogo
	a.catalogoMu.Unlock()
	a.avisarModulosCambiados()
}

// ComprobarActualizacionsModulos volve consultar o catálogo (piztu.org e
// calquera github_repo do catálogo local) sen ter que reiniciar piztu (←
// botón "🔄 Comprobar actualizacións" en ⚙ Aula → Módulos). Bloquea uns
// segundos (ata timeoutIndice por chamada de rede) — o frontend desactiva o
// botón mentres tanto; o repintado chega igual co evento modulos_cambiado.
func (a *App) ComprobarActualizacionsModulos() {
	a.refrescarCatalogo()
	// E aplicar o que atope, non só amosalo: os módulos mantéñense ao día
	// sós (ver vixiarActualizacionsModulos), e este botón é o mesmo traballo
	// pedido xa. Deixalo só en "avisar" faría que premelo amosase un botón
	// "Actualizar" que a seguinte pasada automática ía premer soa.
	a.actualizarModulosAoDia()
}

// intervaloActualizacionModulos é cada canto se volve mirar o catálogo mentres
// piztu está aberto. Unha hora: as versións de módulos non saen por minutos, e
// cada pasada son varias peticións de rede (o índice, e o .zip de cada módulo
// que cambiase).
const intervaloActualizacionModulos = time.Hour

// vixiarActualizacionsModulos repite indefinidamente o par
// refrescarCatalogo + actualizarModulosAoDia. Non devolve nunca: chámase
// dentro da goroutine de arranque, e remata cando remata o proceso.
//
// É deliberadamente silencioso: nin diálogo, nin permiso, nin botón. O
// profesor só ve que a lista de ⚙ Aula → Módulos xa non ten pendente ningún
// "Actualizar" (o evento modulos_cambiado repíntaa soa) e unha liña no
// rexistro. Actualizar NON activa nin desactiva nada, e non baixa módulos que
// non estean xa instalados — iso segue sendo decisión do profesor
// (ver sementarModulos e SetModulo).
//
// Un módulo aberto nese intre non se rompe: en Linux o binario en execución
// segue vivo polo seu inodo aínda que se renomee o cartafol; a versión nova
// colle ao seguinte lanzamento.
func (a *App) vixiarActualizacionsModulos() {
	for {
		time.Sleep(intervaloActualizacionModulos)
		a.refrescarCatalogo()
		a.actualizarModulosAoDia()
	}
}

// sementarModulos baixa TODOS os módulos do catálogo cando o cartafol de
// módulos non ten ningún. É o caso dunha instalación recén feita: sen isto o
// profesor abre Piztu e atópase a lista de módulos baleira agás os botóns
// "Descargar", tendo que premer un por un algo que nunca vai querer doutro
// xeito.
//
// Só actúa co cartafol COMPLETAMENTE baleiro, a posta: en canto haxa un módulo,
// a lista pasa a ser unha decisión do profesor (quitou un que non usa, engadiu
// un de terceiros á man) e ninguén debe desfacerlla por el. O efecto lateral
// aceptado é que baleirar o cartafol enteiro se comporta coma un "restaurar":
// ao seguinte arranque volven baixarse.
//
// Descarga pero NON activa, coma o botón "Descargar": activar un módulo segue
// sendo decisión do profesor en ⚙ Aula → Módulos (hai módulos que poden ser de
// pago, ver o comentario de Tao no arranque).
func (a *App) sementarModulos() {
	if len(modulos.Externos(a.cfg.ModulosDir)) > 0 {
		return
	}
	catalogo := a.catalogoCache()
	if len(catalogo) == 0 {
		return // sen rede, ou catálogo baleiro: nada que facer, e non é un erro
	}

	ids := make([]string, 0, len(catalogo))
	for id := range catalogo {
		ids = append(ids, id)
	}
	sort.Strings(ids) // orde fixa: fai o rexistro reproducible

	var baixados int
	for _, id := range ids {
		if err := modulos.Instalar(a.cfg.ModulosDir, id, catalogo[id]); err != nil {
			// Un módulo que falla non pode parar os demais: quedará co seu
			// botón "Descargar" na lista, coma antes desta función.
			println("Aviso baixando o módulo", id+":", err.Error())
			continue
		}
		println("Módulo baixado:", id, catalogo[id].Version)
		baixados++
	}
	if baixados > 0 {
		a.rexistrarAccionsModulos()
		a.avisarModulosCambiados()
	}
}

// actualizarModulosAoDia pon cada módulo externo XA INSTALADO na versión do
// catálogo se esta é máis nova (compárao con modulos.VersionMaior). Complementa
// sementarModulos: aquela só actúa co cartafol totalmente baleiro e nunca
// reintroduce un módulo que o profesor quitou; esta non engade nin quita
// ningún, só mantén ao día os que xa hai. O estado activo/inactivo consérvase
// (vive á parte, ver modulos.Actualizar). Chámase en fondo ao arrincar, xusto
// despois de refrescarCatalogo — así o profesor non ten que premer "Actualizar"
// módulo por módulo cada vez que sae unha versión nova en piztusistemas/modulos.
func (a *App) actualizarModulosAoDia() {
	catalogo := a.catalogoCache()
	if len(catalogo) == 0 {
		return // sen rede ou catálogo baleiro: nada que comparar, e non é un erro
	}

	var actualizados int
	for _, m := range modulos.Externos(a.cfg.ModulosDir) {
		entrada, noCatalogo := catalogo[m.ID]
		if !noCatalogo || !modulos.VersionMaior(entrada.Version, m.Version) {
			continue // de terceiros (fóra do catálogo) ou xa ao día: non se toca
		}
		if err := modulos.Actualizar(a.cfg.ModulosDir, m.ID, entrada); err != nil {
			// Un módulo que falla non pode parar os demais.
			println("Aviso actualizando o módulo", m.ID+":", err.Error())
			continue
		}
		println("Módulo actualizado:", m.ID, m.Version, "→", entrada.Version)
		// Silencioso non quere dicir invisible: unha liña no rexistro para
		// que un módulo que cambia de aspecto ou de comportamento teña unha
		// explicación á vista, sen interromper con ningún diálogo.
		emitirEvento("modulo_actualizado", map[string]any{
			"id": m.ID, "nome": m.Nome, "anterior": m.Version, "nova": entrada.Version,
		})
		actualizados++
	}
	if actualizados > 0 {
		a.rexistrarAccionsModulos()
		a.avisarModulosCambiados()
	}
}

// catalogoCache devolve a última foto do catálogo, sen tocar a rede.
func (a *App) catalogoCache() map[string]modulos.CatalogoEntrada {
	a.catalogoMu.Lock()
	defer a.catalogoMu.Unlock()
	return a.catalogo
}

// resolverCatalogoEntrada busca un módulo na cache do catálogo (ver
// catalogoCache) e, se non o atopa, fai unha consulta puntual antes de
// renderirse por erro — cubre tanto a cache aínda baleira (premeuse un botón
// antes de que remate refrescarCatalogo) coma un id chegado nun índice máis
// novo ca a última foto gardada.
func (a *App) resolverCatalogoEntrada(id string) (modulos.CatalogoEntrada, error) {
	entrada, ok := a.catalogoCache()[id]
	if !ok {
		fresco := modulos.IndiceRemoto(a.cfg.Modulos.IndiceURL, a.cfg.Modulos.CatalogoLocal)
		a.catalogoMu.Lock()
		a.catalogo = fresco
		a.catalogoMu.Unlock()
		entrada, ok = fresco[id]
	}
	if !ok {
		return modulos.CatalogoEntrada{}, fmt.Errorf("o módulo %q non está no catálogo de piztu.org", id)
	}
	return entrada, nil
}

// avisarModulosCambiados manda a foto actual de módulos e accións ao
// frontend (evento modulos_cambiado), para repintar ⚙ Aula → Módulos sen ter
// que reiniciar piztu.
func (a *App) avisarModulosCambiados() {
	emitirEvento("modulos_cambiado", map[string]any{
		"modulos": a.ModulosDispo(), "accions": a.AccionsModulos(),
	})
}

// comprobarActualizacionPiztu consulta a última Release de piztutao/piztu en
// GitHub (ver internal/actualizacion) e avisa o frontend (evento
// actualizacion_piztu_cambiada) para que o botón "i" amose o bordo vermello
// sen ter que reiniciar piztu.
func (a *App) comprobarActualizacionPiztu() {
	estado := actualizacion.Comprobar()
	a.actualizacionMu.Lock()
	a.actualizacion = estado
	a.actualizacionMu.Unlock()
	emitirEvento("actualizacion_piztu_cambiada", estado)
}

// GetActualizacionPiztu devolve a última foto de se hai actualización de
// Piztu dispoñible, sen tocar a rede (← diálogo "Sobre Piztu" ao abrirse).
func (a *App) GetActualizacionPiztu() actualizacion.Estado {
	a.actualizacionMu.Lock()
	defer a.actualizacionMu.Unlock()
	return a.actualizacion
}

// AplicarActualizacionPiztu descarga a última versión de Piztu, substitúe o
// executable en execución e arrinca xa a nova copia (← botón "Actualizar" no
// diálogo "Sobre Piztu"). O frontend é quen peche esta xanela despois (Quit(),
// mesmo camiño ca "Sair") en canto isto devolva sen erro — así non queda un
// intre con dúas copias de Piztu abertas á vez.
func (a *App) AplicarActualizacionPiztu() error {
	estado := a.GetActualizacionPiztu()
	if !estado.Disponible || estado.URLDescarga == "" {
		return fmt.Errorf("non hai ningunha actualización de Piztu dispoñible")
	}
	if err := actualizacion.AplicarEnCaliente(estado.URLDescarga, estado.URLChecksum); err != nil {
		return err
	}
	return actualizacion.Reiniciar()
}

// InstalarModulo descarga e descomprime un módulo do catálogo de piztu.org en
// ModulosDir/<id>. Non o activa — iso queda para SetModulo, decisión do
// profesor unha vez instalado (← botón "Descargar" en ⚙ Aula → Módulos).
func (a *App) InstalarModulo(id string) error {
	entrada, err := a.resolverCatalogoEntrada(id)
	if err != nil {
		return err
	}
	if err := modulos.Instalar(a.cfg.ModulosDir, id, entrada); err != nil {
		return err
	}
	a.avisarModulosCambiados()
	return nil
}

// ActualizarModulo substitúe un módulo xa instalado pola última versión do
// catálogo (← botón "Actualizar" en ⚙ Aula → Módulos). Non toca se está
// activo ou non: iso persístese á parte (ver modulos.Actualizar).
func (a *App) ActualizarModulo(id string) error {
	entrada, err := a.resolverCatalogoEntrada(id)
	if err != nil {
		return err
	}
	if err := modulos.Actualizar(a.cfg.ModulosDir, id, entrada); err != nil {
		return err
	}
	a.avisarModulosCambiados()
	return nil
}

// SetModulo acende ou apaga un módulo e aplica o efecto no acto (arrancar ou
// deter os seus monitores), para non obrigar a reiniciar piztu.
func (a *App) SetModulo(id string, activo bool) error {
	if a.store == nil {
		return errors.New("non hai base de datos: non se pode gardar o estado dos módulos")
	}
	m, ok := modulos.Buscar(a.cfg.ModulosDir, id)
	if !ok {
		return fmt.Errorf("módulo descoñecido: %q", id)
	}
	if err := modulos.Set(a.store, id, activo); err != nil {
		return err
	}

	// Permisos da API de módulos (internal/api, ver docs/api-modulos.md §5):
	// activar UN módulo concédelle de golpe exactamente os permisos que
	// declara agora mesmo en modulo.json — sen diálogo á parte (ver a
	// xustificación en internal/modulos.GardarPermisosConcedidos).
	// Desactivalo bórraos e revoga calquera token vivo no acto: non abonda
	// con deixar de conceder permisos novos, un módulo aberto nese intre non
	// debe poder seguir chamando á API.
	if activo {
		if err := modulos.GardarPermisosConcedidos(a.store, id, m.Permisos); err != nil {
			println("Aviso gardando permisos de", id, ":", err.Error())
		}
	} else {
		if err := modulos.GardarPermisosConcedidos(a.store, id, nil); err != nil {
			println("Aviso borrando permisos de", id, ":", err.Error())
		}
		if a.api != nil {
			a.api.Revogar(id)
		}
	}

	switch id {
	case modulos.Ruido:
		if activo {
			if a.ruidoAutoarranque() {
				a.ruido.Iniciar(ruido.SoEscoita) // acender o módulo non arma nada
			}
		} else if a.ruido != nil {
			a.ruido.Detener()
		}
	case modulos.Tao:
		if activo {
			if a.tao == nil {
				a.iniciarTao()
			}
		} else if a.tao != nil {
			a.tao.Detener()
			a.tao = nil
		}
	}

	// As accións do módulo entran ou saen da resolución por nome.
	a.rexistrarAccionsModulos()
	if !activo && a.recol != nil {
		a.recol.Esquecer(id) // que non quede o seu dato pegado no mapa
	}
	emitirEvento("modulos_cambiado", map[string]any{
		"modulos": a.ModulosDispo(), "accions": a.AccionsModulos(),
	})
	return nil
}

// RutaModulos devolve o cartafol onde se engaden módulos novos, para amosalo na
// ventá de configuración: en macOS está dentro de ~/Library, que Finder oculta
// por defecto, así que hai que dicir onde é.
func (a *App) RutaModulos() string { return a.cfg.ModulosDir }

// AbrirCartafolModulos ábreo no xestor de ficheiros do sistema.
func (a *App) AbrirCartafolModulos() error {
	if err := modulos.Preparar(a.cfg.ModulosDir); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", a.cfg.ModulosDir)
	case "windows":
		cmd = exec.Command("explorer", a.cfg.ModulosDir)
	default:
		cmd = exec.Command("xdg-open", a.cfg.ModulosDir)
	}
	return cmd.Start()
}

// AccionInfo é unha acción que trae un módulo, para pintar o seu botón.
type AccionInfo struct {
	ID       string `json:"id"`
	Modulo   string `json:"modulo"`
	Etiqueta string `json:"etiqueta"`
	Icona    string `json:"icona"`
}

// patronsAutosign constrúe os patróns de id de minion que o master acepta sen
// intervención (autosign). Derívanse do prefixo da aula que o profesor xa
// escribiu en ⚙ Aula ("tux" → "tux*"), con recurso ao propio inventario se ese
// axuste aínda non está gardado.
//
// Un comodín por prefixo, non a lista exacta de equipos, é unha decisión
// deliberada: o caso que ten que funcionar sen tocar nada é o equipo
// reinstalado ou clonado que volve entrar coa chave nova, e unha lista pecha
// obrigaría a reconfigurar o master cada vez que cambia a aula. A contrapartida
// é que calquera máquina da rede que diga chamarse <prefixo>NN entra soa; é
// asumible nunha aula administrada, e por iso patronsValidos rexeita "*".
func (a *App) patronsAutosign(hosts []string) []string {
	if a.store != nil {
		if p := strings.TrimSpace(a.store.LerAxuste("aula_prefixo", "")); p != "" {
			return []string{p + "*"}
		}
	}
	vistos := map[string]bool{}
	var patróns []string
	for _, h := range hosts {
		p := strings.TrimRight(nomeBaseEquipo(h), "0123456789")
		if p == "" || vistos[p] {
			continue
		}
		vistos[p] = true
		patróns = append(patróns, p+"*")
	}
	return patróns
}

// nomeBaseEquipo queda co nome ata o primeiro punto (tux01.local → tux01).
func nomeBaseEquipo(h string) string {
	if i := strings.Index(h, "."); i >= 0 {
		return h[:i]
	}
	return h
}

// ComprobarSalt executa o diagnóstico do motor Salt e devolve a lista de
// comprobacións para pintar en ⚙ Aula. Non modifica nada nin pide contrasinal:
// é a resposta a "non me funciona e descoñezo o motivo", que ata agora só se
// podía responder executando ordes de `salt` á man nunha terminal.
// Devolve liñas xa formatadas, non as Comprobacion en cru, para que o binding
// non teña que arrastrar un tipo novo ata o frontend: a estrutura (e as súas
// probas) queda en saltsetup.Diagnostico, que é onde importa.
func (a *App) ComprobarSalt() []string {
	var liñas []string
	for _, c := range saltsetup.Diagnostico(motor.Get(a.cfg, "").GetEquipos()) {
		l := "✔ "
		if !c.Ok {
			l = "✘ "
		}
		l += c.Titulo
		if c.Detalle != "" {
			l += ": " + c.Detalle
		}
		if c.Arranxo != "" {
			l += "\n     → " + c.Arranxo
		}
		liñas = append(liñas, l)
	}
	return liñas
}

// raicesSaltModulos devolve os cartafoles que o fileserver do master ten que
// servir ademais de cfg.SaltDir, para que os estados que traen os módulos sexan
// aplicables (ver ConfigurarFileRoots):
//
//   - ModulosDir, para que un estado poida referirse a ficheiros doutro módulo
//     por salt://<id>/… (non expón datos de alumnado: Tao garda os seus en
//     ~/.config/dixitalizacion, fóra deste cartafol);
//   - o cartafol onde vive o .sls de cada acción de módulo ACTIVO. Fai falla
//     ademais do anterior: `state.apply <nome>` só resolve se <nome>.sls está
//     na RAÍZ dun file_root, e AccionExterna.Estado é unha ruta calquera dentro
//     do módulo (modulos.Accion), non unha convención fixa. Con só ModulosDir,
//     modulos/xesta/salt/limpar.sls sería o estado "xesta.salt.limpar", pero
//     cfg.SaltEstadoNome() pide "limpar" — e volvería o "No matching sls found".
//
// Só se consulta cando se (re)configura o master. Un módulo instalado despois
// non entra ata que se repita o paso de Salt en ⚙ Aula: escribir o drop-in pide
// root, e o contrasinal só o temos nese fluxo.
func (a *App) raicesSaltModulos() []string {
	raices := []string{a.cfg.ModulosDir}
	for _, m := range modulos.Todos(a.cfg.ModulosDir) {
		if !a.moduloActivo(m.ID) {
			continue
		}
		for _, ac := range m.Accions {
			if ac.Estado != "" {
				raices = append(raices, filepath.Dir(ac.Estado))
			}
		}
	}
	return raices
}

// rexistrarAccionsModulos ensínalle á configuración onde están os ficheiros das
// accións que traen os módulos ACTIVOS, para que a resolución por nome que xa
// fan os motores as atope. Chámase no arranque e cada vez que muda un módulo.
func (a *App) rexistrarAccionsModulos() {
	a.cfg.LimparAccionsModulo()
	for _, m := range modulos.Todos(a.cfg.ModulosDir) {
		if !a.moduloActivo(m.ID) {
			continue
		}
		for _, ac := range m.Accions {
			a.cfg.RexistrarAccionModulo(ac.ID, config.AccionExterna{
				Playbook: ac.Playbook, Estado: ac.Estado, Script: ac.Script,
			})
		}
	}
}

// AccionsModulos devolve as accións dos módulos activos que o MOTOR ACTUAL pode
// executar. Unha acción que só trae, por exemplo, un .sls non se ofrece estando
// en SSH: mellor non pintar o botón que pintalo e que falle ao premelo.
func (a *App) AccionsModulos() []AccionInfo {
	nome := motor.MotorActivo(a.cfg)
	var res []AccionInfo
	for _, m := range modulos.Todos(a.cfg.ModulosDir) {
		if !a.moduloActivo(m.ID) {
			continue
		}
		for _, ac := range m.Accions {
			var ruta string
			switch nome {
			case "ansible":
				ruta = ac.Playbook
			case "salt":
				ruta = ac.Estado
			default:
				ruta = ac.Script
			}
			if ruta == "" {
				continue
			}
			if _, err := os.Stat(ruta); err != nil {
				continue // declarada pero o ficheiro non está
			}
			res = append(res, AccionInfo{ID: ac.ID, Modulo: m.ID, Etiqueta: ac.Etiqueta, Icona: ac.Icona})
		}
	}
	return res
}

// PanelModulo devolve a descrición do panel dun módulo activo (ou nil).
// Acompáñase dos valores xa gardados e dos equipos, para poder encher as listas
// con `fonte: equipos` sen unha segunda chamada.
type PanelResposta struct {
	Panel   *modulos.Panel    `json:"panel"`
	Valores map[string]string `json:"valores"`
	Equipos []string          `json:"equipos"`
}

func (a *App) PanelModulo(id string) *PanelResposta {
	if !a.moduloActivo(id) {
		return nil
	}
	m, ok := modulos.Buscar(a.cfg.ModulosDir, id)
	if !ok {
		return nil
	}
	panel := modulos.LerPanel(m)
	if panel == nil {
		return nil
	}
	res := &PanelResposta{Panel: panel, Valores: map[string]string{}}
	for _, c := range panel.Campos {
		if c.ID == "" || a.store == nil {
			continue
		}
		res.Valores[c.ID] = a.store.LerAxuste(modulos.ClaveCampo(id, c.ID), c.Defecto)
	}
	for _, c := range panel.Campos {
		if c.Tipo == modulos.CampoLista && c.Fonte == "equipos" {
			res.Equipos = motor.Get(a.cfg, "").GetEquipos()
			break
		}
	}
	return res
}

// GardarCampoModulo persiste o valor dun campo do panel.
func (a *App) GardarCampoModulo(moduloID, campoID, valor string) error {
	if a.store == nil {
		return errors.New("non hai base de datos")
	}
	if !a.moduloActivo(moduloID) {
		return errModuloInactivo(moduloID)
	}
	return a.store.GardarAxuste(modulos.ClaveCampo(moduloID, campoID), valor)
}

// AbrirModulo lanza un módulo externo (unha aplicación independente). Tao ten o
// seu propio método por compatibilidade e polas rutas de busca herdadas.
func (a *App) AbrirModulo(id string) error {
	if id == modulos.Tao {
		return a.AbrirTao()
	}
	if !a.moduloActivo(id) {
		return errModuloInactivo(id)
	}
	m, ok := modulos.Buscar(a.cfg.ModulosDir, id)
	if !ok || m.Executable == "" {
		return fmt.Errorf("o módulo %q non ten nada que lanzar", id)
	}
	ruta := filepath.Join(m.Dir, m.Executable)
	if _, err := os.Stat(ruta); err != nil {
		return fmt.Errorf("non se atopa %s", ruta)
	}
	contexto, err := a.escribirContexto(m)
	if err != nil {
		return fmt.Errorf("non se puido preparar o contexto: %w", err)
	}
	return a.lanzarAplicacion(id, ruta, contexto)
}

// xestaModuloID é o id do módulo Xesta — usado só para o nome/icona de
// reserva (nomeReservaModulo/iconaReservaModulo) cando aínda non hai
// modulo.json que consultar. Xesta lánzase hoxe por AbrirModulo, coma
// calquera outro módulo externo con xanela propia (Tao/Pancho) — deixou de
// ser un servidor HTTP de fondo incrustado nun <iframe>.
const xestaModuloID = "xesta"

// ContextoModulo é a foto que recibe un módulo ao lanzarse. É un contrato
// público: os módulos e as súas librerías dependen destes nomes.
//
// APISocket/APIToken/APIPermisos son a porta de entrada á API en marcha
// (internal/api, ver docs/api-modulos.md) — a diferenza do resto do
// contexto, que é unha foto estática, isto permite ao módulo preguntar
// datos frescos (motor/equipos) e executar contido mentres está aberto.
// APIToken vai baleiro se o módulo non ten ningún permiso concedido (nunca
// se activou con "permisos" en modulo.json, ou desactivouse): nese caso o
// módulo simplemente non fala coa API, non é un erro.
type ContextoModulo struct {
	Version     int               `json:"version"`
	BaseDir     string            `json:"base_dir"`
	TmpDir      string            `json:"tmp_dir"`
	ModuloID    string            `json:"modulo_id"`
	ModuloDir   string            `json:"modulo_dir"`
	Motor       string            `json:"motor"`
	Equipos     []EquipoContexto  `json:"equipos"`
	Axustes     map[string]string `json:"axustes"`
	APISocket   string            `json:"api_socket"`
	APIToken    string            `json:"api_token,omitempty"`
	APIPermisos []string          `json:"api_permisos,omitempty"`
	// Idioma é o código (gl/es/en/pt) que o profesorado ten escollido en ⚙ Aula
	// para o propio Piztu (cfg.Idioma) — un módulo úsao coma valor por defecto
	// do SEU idioma, non coma unha orde: unha elección explícita xa gardada
	// dentro do módulo (ex. localStorage de Tao) segue tendo prioridade sobre
	// isto. Ver docs/api-modulos.md e o i18n de cada módulo.
	Idioma string `json:"idioma"`
}

// EquipoContexto é un equipo da aula tal e como o ve o módulo.
type EquipoContexto struct {
	Nome     string `json:"nome"`
	Mac      string `json:"mac"`
	Acendido bool   `json:"acendido"`
}

// escribirContexto deixa a foto nun ficheiro e devolve a súa ruta.
//
// Vai a ficheiro e non pola entrada estándar por unha limitación real de macOS:
// os módulos son bundles .app e lánzanse con `open`, que desconecta o proceso e
// non deixa canalizarlle stdin. Un ficheiro máis a ruta por argumento e variable
// de contorno funciona nos dous sistemas e con calquera linguaxe.
func (a *App) escribirContexto(m modulos.Modulo) (string, error) {
	vars := inventory.LerHostsConVars(a.cfg.HostsFile)
	a.acendidosMu.Lock()
	acendidos := make(map[string]bool, len(a.acendidos))
	for k, v := range a.acendidos {
		acendidos[k] = v
	}
	a.acendidosMu.Unlock()

	// a.tr pode ser nil en tests que constrúen un App{} minimalista sen pasar
	// por startup() — dexenera a "" (o módulo cae no seu propio fallback) en
	// vez de entrar en pánico.
	idioma := ""
	if a.tr != nil {
		idioma = a.tr.Idioma()
	}
	ctx := ContextoModulo{
		Version: 1, BaseDir: a.cfg.BaseDir, TmpDir: a.cfg.TmpDir,
		ModuloID: m.ID, ModuloDir: m.Dir, Motor: motor.MotorActivo(a.cfg),
		Axustes: map[string]string{}, Idioma: idioma,
	}
	if a.api != nil && a.store != nil {
		ctx.APISocket = a.api.RutaSocket()
		if permisos := modulos.PermisosConcedidos(a.store, m.ID); len(permisos) > 0 {
			ctx.APIToken = a.api.EmitirToken(m.ID, permisos)
			ctx.APIPermisos = permisos
		}
	}
	if panel := modulos.LerPanel(m); panel != nil && a.store != nil {
		for _, c := range panel.Campos {
			if c.ID == "" {
				continue
			}
			ctx.Axustes[c.ID] = a.store.LerAxuste(modulos.ClaveCampo(m.ID, c.ID), c.Defecto)
		}
	}
	for _, h := range motor.Get(a.cfg, "").GetEquipos() {
		ctx.Equipos = append(ctx.Equipos, EquipoContexto{
			Nome: h, Mac: inventory.ExtraerMac(vars[h]), Acendido: acendidos[h],
		})
	}

	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	ruta := filepath.Join(a.cfg.TmpDir, "contexto_"+m.ID+".json")
	if err := os.WriteFile(ruta, data, 0o600); err != nil {
		return "", err
	}
	return ruta, nil
}

// lanzarAplicacion abre `ruta` como proceso independente, pasándolle o
// contexto por argumento e por variable de contorno (ver escribirContexto).
// Se xa hai un proceso vivo para `id` (lanzado por unha chamada anterior),
// non abre outro: pídelle foco por GET /api/v1/eventos (ver PedirFoco en
// internal/api) para que traia a súa xanela a primeiro plano — así premer o
// botón dun módulo xa aberto (Xesta, Pancho) non deixa instancias soltas
// detrás de Piztu nin as duplica.
func (a *App) lanzarAplicacion(id, ruta, contexto string) error {
	a.procesosMu.Lock()
	if p, ok := a.procesos[id]; ok && p.vivo() {
		a.procesosMu.Unlock()
		if a.api != nil {
			a.api.PedirFoco(id)
		}
		return nil
	}
	a.procesosMu.Unlock()

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && strings.HasSuffix(ruta, ".app") {
		cmd = exec.Command("open", "-n", ruta, "--args", "--piztu-contexto", contexto)
	} else {
		cmd = exec.Command(ruta, "--piztu-contexto", contexto)
		cmd.Dir = filepath.Dir(ruta)
	}
	cmd.Env = append(os.Environ(), "PIZTU_CONTEXTO="+contexto)
	if err := cmd.Start(); err != nil {
		return err
	}

	feito := make(chan struct{})
	a.procesosMu.Lock()
	if a.procesos == nil {
		a.procesos = map[string]*procesoModulo{}
	}
	a.procesos[id] = &procesoModulo{cmd: cmd, feito: feito}
	a.procesosMu.Unlock()

	go func() {
		_ = cmd.Wait() // non deixar zombies: piztu queda aberto moito tempo
		close(feito)
	}()
	return nil
}

// IniciarRuido/DetenerRuido expóñense ao frontend, ligados á casilla "Activo"
// (← chk_ruido/toggle_monitor de main.py).
// IniciarRuido ARMA o control de ruído: a partir de aquí o monitor pode abrir
// avisos e bloquear/desbloquear a aula el só. É o único camiño que arma, e
// chámase desde un único sitio: manter premida 3s a icona do micrófono
// (main.js). Non o chames desde ningún arranque automático.
func (a *App) IniciarRuido() {
	if !a.moduloActivo(modulos.Ruido) {
		return
	}
	if a.ruido != nil {
		a.ruido.Iniciar(ruido.ConAccions)
	}
}

func (a *App) DetenerRuido() {
	if a.ruido != nil {
		a.ruido.Detener()
	}
}

// RuidoArmado di se o control de ruído está armado agora mesmo. O frontend
// consúltao ao arrincar para pintar a icona do micrófono conforme ao estado
// real do backend: antes a icona nacía sempre apagada aínda que o monitor
// estivese a traballar, e era imposible saber desde a aula que estaba activo.
func (a *App) RuidoArmado() bool {
	return a.ruido != nil && a.ruido.Armado()
}

func dirsIdiomas(cfg *config.Config) []string {
	return []string{
		"idiomas",
		filepath.Join("..", "idiomas"),
		filepath.Join(cfg.BaseDir, "idiomas"),
	}
}

// ── Datos iniciais / i18n ────────────────────────────────────────────────────

// Equipo é un equipo do mapa coa súa posición.
type Equipo struct {
	Nome string  `json:"nome"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// GetEquipos devolve os equipos co seu nome e posición (← app.py:index).
func (a *App) GetEquipos() []Equipo {
	m := motor.Get(a.cfg, "")
	res := []Equipo{}
	if a.store == nil {
		return res
	}
	pos, _ := a.store.GetPosicions()
	for _, e := range m.GetEquipos() {
		p, ok := pos[e]
		if !ok {
			p = db.Posicion{X: 50, Y: 50}
		}
		res = append(res, Equipo{Nome: e, X: p.X, Y: p.Y})
	}
	return res
}

// AulaConfig son os datos da aula que persisten entre sesións (na táboa `axuste`).
type AulaConfig struct {
	Prefixo    string `json:"prefixo"`
	Dominio    string `json:"dominio"`
	NEquipos   int    `json:"nequipos"`
	SSHUser    string `json:"sshuser"`
	SaltMaster string `json:"saltmaster"`
}

// GetAulaConfig devolve os datos da aula gardados, con valores por defecto
// razoables a primeira vez (aula baleira aínda sen configurar).
func (a *App) GetAulaConfig() AulaConfig {
	c := AulaConfig{Prefixo: "tux", Dominio: ".local", NEquipos: 0, SSHUser: "usuario"}
	if a.store == nil {
		c.SaltMaster = saltsetup.EnderezoMaster(c.Dominio)
		return c
	}
	c.Prefixo = a.store.LerAxuste("aula_prefixo", c.Prefixo)
	c.Dominio = a.store.LerAxuste("aula_dominio", c.Dominio)
	c.SSHUser = a.store.LerAxuste("aula_ssh_user", c.SSHUser)
	// O enderezo do master dedúcese do nome deste equipo máis o dominio da aula;
	// o campo da interface só está para os casos raros (un master noutra máquina,
	// ou macOS, onde non se pode instalar aquí). Antes o defecto era
	// os.Hostname() a secas — ver saltsetup.EnderezoMaster para por que iso
	// deixaba os minions apuntando a un nome que eles non saben resolver.
	//
	// A dedución/validación é lenta (DNS con timeout) e faise en fondo ao
	// arrincar (resolverSaltMaster); aquí só se le a foto xa calculada para
	// non bloquear a apertura de ⚙ Aula. Se aínda non rematou, devólvese o
	// valor cru gardado e o frontend enche o campo co evento
	// aula_salt_master_resolto en canto estea.
	a.saltMasterMu.Lock()
	c.SaltMaster = a.saltMasterResolto
	a.saltMasterMu.Unlock()
	if c.SaltMaster == "" {
		c.SaltMaster = a.store.LerAxuste("aula_salt_master", "")
	}
	if n, err := strconv.Atoi(a.store.LerAxuste("aula_nequipos", "0")); err == nil {
		c.NEquipos = n
	}
	return c
}

// resolverSaltMaster deduce e valida o enderezo do salt-master (lento: DNS con
// timeout, ver saltsetup.EnderezoMasterValidado), garda a foto en
// a.saltMasterResolto e avisa o frontend co evento aula_salt_master_resolto —
// así ⚙ Aula ábrese ao instante e o campo do master enche/corríxese despois.
// Se o valor gardado era malo (defecto antigo sen dominio) déixao xa
// arranxado no almacén para a próxima. Chámase en fondo ao arrincar e tras
// InstalarSalt.
func (a *App) resolverSaltMaster() {
	dominio := ".local"
	gardado := ""
	if a.store != nil {
		dominio = a.store.LerAxuste("aula_dominio", dominio)
		gardado = a.store.LerAxuste("aula_salt_master", "")
	}
	addr := saltsetup.EnderezoMasterValidado(gardado, dominio)

	a.saltMasterMu.Lock()
	a.saltMasterResolto = addr
	a.saltMasterMu.Unlock()

	if a.store != nil && addr != "" && addr != gardado {
		_ = a.store.GardarAxuste("aula_salt_master", addr)
	}
	emitirEvento("aula_salt_master_resolto", map[string]any{"saltmaster": addr})
}

// claveKeyPath devolve a ruta da clave SSH compartida da aula (~/.ssh/…).
func (a *App) claveKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "id_rsa_piztu_aula")
}

// DistribuirClaveSSH xera (se cómpre) a clave compartida da aula e empúrraa a
// todos os equipos do inventario por SSH nativo (sen sshpass). Emite progreso
// polos eventos "clave_log" e "clave_completo". É o Paso "clave SSH" do
// instalador, agora integrado en piztu e válido tamén en macOS.
func (a *App) DistribuirClaveSSH(usuario, contrasinal string) {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("clave_log", map[string]any{"host": host, "msg": linha})
		}
		rematar := func(ok bool, total int) {
			emitirEvento("clave_completo", map[string]any{"ok": ok, "total": total})
		}

		if a.store != nil {
			_ = a.store.GardarAxuste("aula_ssh_user", usuario)
		}

		keyPath := a.claveKeyPath()
		creada, err := sshkey.Xerar(keyPath)
		if err != nil {
			emit("", "❌ erro xerando a clave: "+err.Error())
			rematar(false, 0)
			return
		}
		if creada {
			emit("", "🔑 clave nova xerada en "+keyPath)
		} else {
			emit("", "🔑 usando clave existente "+keyPath)
		}

		hosts := motor.Get(a.cfg, "").GetEquipos()
		if len(hosts) == 0 {
			emit("", "⚠️ non hai equipos no inventario; xera a aula primeiro (⚙ Aula).")
			rematar(false, 0)
			return
		}
		emit("", fmt.Sprintf("Distribuíndo a clave a %d equipos…", len(hosts)))
		sshkey.Distribuir(hosts, usuario, contrasinal, keyPath, emit)
		emit("", "Feito.")
		rematar(true, len(hosts))
	}()
}

// InstalarTaoClientes despraza Tao (o mesmo binario que xa usa AbrirTao no
// servidor) a /opt/piztu en cada equipo cliente da aula, con acceso directo
// no menú e no escritorio do alumnado. É a mesma acción que o instalador fai
// unha soa vez (Paso 8), dispoñible aquí para repetila cando faga falla
// (equipos novos, reinstalacións) sen volver pasar polo instalador.
func (a *App) InstalarTaoClientes() {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("tao_clientes_log", map[string]any{"host": host, "msg": linha})
		}
		rematar := func(ok bool, total int) {
			emitirEvento("tao_clientes_completo", map[string]any{"ok": ok, "total": total})
		}

		if !a.moduloActivo(modulos.Tao) {
			emit("", "❌ o módulo Tao está desactivado (⚙ Aula → Módulos)")
			rematar(false, 0)
			return
		}
		taoSrc, ok := a.localizarTao()
		if !ok {
			emit("", "❌ non se atopa Tao neste servidor — instálao primeiro (⚙ Aula → Módulos → Tao)")
			rematar(false, 0)
			return
		}

		// A icona vai embebida en piztu (a mesma que usa o instalador);
		// escríbese nun temporal só porque o playbook a copia por SSH cun
		// `copy: src=`, que precisa dunha ruta de ficheiro real.
		taoIconSrc := ""
		if iconTmp, err := os.CreateTemp(a.cfg.TmpDir, "tao_icon_*.png"); err == nil {
			if _, err := iconTmp.Write(recursos.IconaTao()); err == nil {
				taoIconSrc = iconTmp.Name()
				defer os.Remove(taoIconSrc)
			}
			iconTmp.Close()
		}

		hosts := motor.Get(a.cfg, "ansible").GetEquipos()
		if len(hosts) == 0 {
			emit("", "⚠️ non hai equipos no inventario")
			rematar(false, 0)
			return
		}
		ansible, ok := motor.Get(a.cfg, "ansible").(*motor.MotorAnsible)
		if !ok {
			emit("", "❌ erro interno: motor Ansible non dispoñible")
			rematar(false, 0)
			return
		}

		emit("", fmt.Sprintf("Instalando Tao en %d equipo(s)…", len(hosts)))
		fallo := false
		err := ansible.InstalarTaoClientes(hosts, taoSrc, taoIconSrc, motor.Callbacks{
			OnOk: func(host, _ string) {
				emit(host, "✔ instalado")
			},
			OnErro: func(host, saida string) {
				fallo = true
				emit(host, "✖ erro: "+extraerErro(saida))
			},
		})
		if err != nil {
			emit("", "❌ "+err.Error())
			rematar(false, len(hosts))
			return
		}
		emit("", "Feito.")
		rematar(!fallo, len(hosts))
	}()
}

// XerarInventario escribe o ficheiro `hosts` a partir dos datos da aula
// (prefixo, dominio e número de equipos), persiste eses datos e devolve a ruta
// escrita. É o primeiro paso da "Configuración" integrada: substitúe o que antes
// facía o instalador, para que o profesor xere a aula desde o propio piztu
// (tamén en macOS).
func (a *App) XerarInventario(prefixo, dominio string, n int) (string, error) {
	if err := inventory.EscribirHosts(a.cfg.HostsFile, prefixo, dominio, n); err != nil {
		return "", err
	}
	if a.store != nil {
		_ = a.store.GardarAxuste("aula_prefixo", prefixo)
		_ = a.store.GardarAxuste("aula_dominio", dominio)
		_ = a.store.GardarAxuste("aula_nequipos", strconv.Itoa(n))
	}
	return a.cfg.HostsFile, nil
}

// EscanearRede conéctase por SSH (con clave) a cada equipo do inventario,
// detecta os acendidos e garda a súa MAC no ficheiro `hosts` (para poder
// acendelos por Wake-on-LAN). Emite progreso por "scan_log"/"scan_completo".
// Porte nativo de xerarInventario.yaml, integrado en piztu.
func (a *App) EscanearRede() {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("scan_log", map[string]any{"host": host, "msg": linha})
		}
		rematar := func(ok bool, total, online int) {
			emitirEvento("scan_completo",
				map[string]any{"ok": ok, "total": total, "online": online})
		}

		hosts := motor.Get(a.cfg, "").GetEquipos()
		if len(hosts) == 0 {
			emit("", "⚠️ non hai equipos no inventario; xera a aula primeiro (⚙ Aula).")
			rematar(false, 0, 0)
			return
		}
		keyPath := a.claveKeyPath()
		if _, err := os.Stat(keyPath); err != nil {
			emit("", "⚠️ non hai clave SSH; distribúe a clave primeiro (Paso clave).")
			rematar(false, len(hosts), 0)
			return
		}
		usuario := "usuario"
		if a.store != nil {
			usuario = a.store.LerAxuste("aula_ssh_user", usuario)
		}

		emit("", fmt.Sprintf("Escaneando %d equipos…", len(hosts)))
		macs := sshkey.Escanear(hosts, usuario, keyPath, emit)
		if len(macs) > 0 {
			if err := inventory.ActualizarMacs(a.cfg.HostsFile, macs); err != nil {
				emit("", "⚠️ non se puideron gardar as MACs: "+err.Error())
			} else {
				emit("", fmt.Sprintf("Gardadas %d MACs no inventario.", len(macs)))
			}
		}
		emit("", fmt.Sprintf("Feito: %d de %d equipos acendidos.", len(macs), len(hosts)))
		rematar(true, len(hosts), len(macs))
	}()
}

// DescubrirRede escanea a rede local (porto 22, sen depender do inventario)
// e devolve os equipos atopados por eventos "descubrimento_log" (progreso) e
// "descubrimento_completo" (lista final). É a alternativa ao esquema
// prefixo+número: atopa máquinas aínda non rexistradas na aula.
func (a *App) DescubrirRede() {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("descubrimento_log", map[string]any{"host": host, "msg": linha})
		}
		equipos := discover.Escanear(emit)
		emitirEvento("descubrimento_completo",
			map[string]any{"ok": true, "equipos": equipos})
	}()
}

// HostCredencial é un equipo descuberto por DescubrirRede xunto co
// usuario/contrasinal que o admin escribiu para el (poden ser calquera,
// non seguen a convención da aula).
type HostCredencial struct {
	IP          string `json:"ip"`
	Usuario     string `json:"usuario"`
	Contrasinal string `json:"contrasinal"`
}

// DistribuirClaveHosts empurra a clave SSH compartida da aula a equipos
// atopados por DescubrirRede, cada un co seu propio usuario/contrasinal (o
// contrasinal só se usa unha vez, para esta distribución puntual). Os
// equipos nos que ten éxito engádense ao inventario. Emite progreso por
// "clave_hosts_log"/"clave_hosts_completo".
func (a *App) DistribuirClaveHosts(credenciais []HostCredencial) {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("clave_hosts_log", map[string]any{"host": host, "msg": linha})
		}
		rematar := func(ok bool, total int, exitosos []string) {
			emitirEvento("clave_hosts_completo",
				map[string]any{"ok": ok, "total": total, "exitosos": exitosos})
		}

		keyPath := a.claveKeyPath()
		creada, err := sshkey.Xerar(keyPath)
		if err != nil {
			emit("", "❌ erro xerando a clave: "+err.Error())
			rematar(false, len(credenciais), nil)
			return
		}
		if creada {
			emit("", "🔑 clave nova xerada en "+keyPath)
		}

		credMap := map[string]sshkey.Credencial{}
		for _, c := range credenciais {
			credMap[c.IP] = sshkey.Credencial{Usuario: c.Usuario, Contrasinal: c.Contrasinal}
		}
		exitosos := sshkey.DistribuirCredenciais(credMap, keyPath, emit)

		if len(exitosos) > 0 {
			if err := inventory.AppendEquipos(a.cfg.HostsFile, exitosos); err != nil {
				emit("", "⚠️ non se puideron engadir ao inventario: "+err.Error())
			} else {
				emit("", fmt.Sprintf("Engadidos %d equipo(s) ao inventario.", len(exitosos)))
			}
		}
		rematar(len(exitosos) == len(credenciais), len(credenciais), exitosos)
	}()
}

// InstalarSalt pon a punto Salt para a aula: (A) salt-master neste equipo (só
// Linux; en macOS omítese), (B) salt-minion en cada cliente por SSH con clave,
// e (C) aceptación das chaves no master. Emite progreso por "salt_setup_log" e
// "salt_setup_completo". É o Paso 5 do instalador, integrado en piztu.
func (a *App) InstalarSalt(masterAddr, usuario, contrasinal string) {
	go func() {
		emit := func(host, linha string) {
			emitirEvento("salt_setup_log", map[string]any{"host": host, "msg": linha})
		}
		fin := func(ok bool) {
			emitirEvento("salt_setup_completo", map[string]any{"ok": ok})
		}

		masterAddr = strings.TrimSpace(masterAddr)
		usuario = strings.TrimSpace(usuario)
		if usuario == "" || contrasinal == "" {
			emit("", "⚠️ indica usuario e contrasinal.")
			fin(false)
			return
		}
		// O enderezo do master xa non se pide: dedúcese do nome deste equipo e do
		// dominio da aula. Só se respecta o do campo se o profesor escribiu un.
		dominio := ".local"
		if a.store != nil {
			dominio = a.store.LerAxuste("aula_dominio", dominio)
		}
		if masterAddr == "" {
			masterAddr = saltsetup.EnderezoMaster(dominio)
			emit("", "🔎 enderezo do master deducido: "+masterAddr)
		} else if suxerido := saltsetup.EnderezoMaster(dominio); suxerido != masterAddr {
			// Non se corrixe só: pode ser un master noutra máquina, a posta.
			emit("", "ℹ️ usarase o enderezo indicado ("+masterAddr+
				"); o deducido para este equipo sería "+suxerido+".")
		}

		hosts := motor.Get(a.cfg, "").GetEquipos()
		if len(hosts) == 0 {
			emit("", "⚠️ non hai equipos no inventario; xera a aula primeiro (⚙ Aula).")
			fin(false)
			return
		}
		keyPath := a.claveKeyPath()
		if _, err := os.Stat(keyPath); err != nil {
			emit("", "⚠️ non hai clave SSH; distribúe a clave primeiro (Paso clave).")
			fin(false)
			return
		}

		if a.store != nil {
			_ = a.store.GardarAxuste("aula_salt_master", masterAddr)
			_ = a.store.GardarAxuste("aula_ssh_user", usuario)
		}
		// Refresca a foto cacheada para ⚙ Aula sen agardar a reiniciar Piztu.
		go a.resolverSaltMaster()

		// Limpeza previa: parte de cero en cada instalación en vez de arrastrar
		// unha instalación vella ou a medio configurar. Non é un erro se non
		// había nada que desinstalar (é idempotente); en macOS omítese só.
		emit("", "🧹 Limpando instalacións anteriores…")
		if err := saltsetup.DesinstalarMaster(contrasinal, emit); err != nil && !errors.Is(err, saltsetup.ErrMasterNonSoportado) {
			emit("", "⚠️ non se puido limpar o salt-master anterior: "+err.Error())
		}
		saltsetup.DesinstalarMinions(hosts, usuario, keyPath, contrasinal, emit)

		// (A) Master neste equipo.
		masterOk := saltsetup.MasterInstalado()
		if masterOk {
			emit("", "🧩 salt-master xa instalado; omítese.")
		} else {
			emit("", "🧩 Instalando salt-master neste equipo…")
			if err := saltsetup.InstalarMaster(contrasinal, emit); err != nil {
				if errors.Is(err, saltsetup.ErrMasterNonSoportado) {
					emit("", "ℹ️ salt-master non soportado en macOS: omítese (usa o motor Ansible ou un master Linux aparte).")
				} else {
					emit("", "❌ erro instalando salt-master: "+err.Error())
				}
			} else {
				masterOk = true
				emit("", "✅ salt-master instalado.")
			}
		}

		// (A2) Axustes do master (file_roots + autosign). Faise sempre que haxa
		// master, tamén cando xa estaba instalado: sen file_roots o master non
		// serve ningún .sls e todo state.apply falla con "No matching sls found"
		// aínda que os minions respondan, e ese caso —master vello, xa
		// instalado— é precisamente o que se salta a rama de instalación.
		if masterOk && runtime.GOOS != "darwin" {
			opcions := saltsetup.OpcionsMaster{
				SaltDir:  a.cfg.SaltDir,
				Extras:   a.raicesSaltModulos(),
				Autosign: a.patronsAutosign(hosts),
			}
			if err := saltsetup.ConfigurarMaster(opcions, contrasinal, emit); err != nil {
				emit("", "⚠️ non se puido configurar o master: "+err.Error())
			} else {
				emit("", "✅ master configurado (file_roots → "+a.cfg.SaltDir+
					", autosign → "+strings.Join(opcions.Autosign, " ")+")")
			}
		}

		// (B) Minions en todos os clientes.
		emit("", fmt.Sprintf("🖥️ Instalando salt-minion en %d equipos…", len(hosts)))
		saltsetup.InstalarMinions(hosts, usuario, keyPath, masterAddr, contrasinal, emit)

		// (C) Aceptar chaves (só se hai master local en Linux).
		if masterOk && runtime.GOOS != "darwin" {
			emit("", "🔑 Aceptando chaves dos minions…")
			if err := saltsetup.AceptarChaves(contrasinal, emit); err != nil {
				emit("", "⚠️ non se puideron aceptar as chaves: "+err.Error())
			} else {
				emit("", "✅ chaves aceptadas.")
			}
		}
		emit("", "Feito.")
		fin(true)
	}()
}

// InstalarAnsible instala o motor Ansible. En Linux (Debian) instala o
// paquete `ansible` con apt neste equipo, co mesmo contrasinal de sudo que
// "Instalar Salt". En macOS instálao con Homebrew (se falta Homebrew, ábreo
// nun Terminal para completar a instalación interactivamente). Emite
// progreso por "ansible_setup_log"/"ansible_setup_completo".
func (a *App) InstalarAnsible(contrasinal string) {
	go func() {
		emit := func(l string) {
			emitirEvento("ansible_setup_log", map[string]any{"msg": l})
		}
		fin := func(ok bool) {
			emitirEvento("ansible_setup_completo", map[string]any{"ok": ok})
		}
		if runtime.GOOS != "darwin" {
			if saltsetup.AnsibleInstalado() {
				emit("✅ ansible xa está instalado; omítese.")
				fin(true)
				return
			}
			if strings.TrimSpace(contrasinal) == "" {
				emit("⚠️ indica o contrasinal (sudo) de arriba.")
				fin(false)
				return
			}
			emit("📦 Instalando ansible con apt… (pode tardar uns minutos)")
			logLinha := func(_, l string) { emit(l) }
			if err := saltsetup.InstalarAnsible(contrasinal, logLinha); err != nil {
				emit("❌ erro instalando ansible: " + err.Error())
				fin(false)
				return
			}
			emit("✅ Ansible instalado. Xa podes usar o motor Ansible (⚙, premido 3s).")
			fin(true)
			return
		}

		brew := localizarBrew()
		if brew == "" {
			emit("Homebrew non atopado. Ábrese un Terminal para instalalo (require o teu contrasinal)…")
			cmd := `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
			if err := abrirTerminalConComando(cmd); err != nil {
				emit("❌ non se puido abrir o Terminal: " + err.Error())
				emit("Instala Homebrew manualmente e volve premer o botón.")
			} else {
				emit("Completa a instalación de Homebrew no Terminal e volve premer o botón para instalar Ansible.")
			}
			fin(false)
			return
		}

		emit("Homebrew atopado en " + brew + ". Instalando Ansible… (pode tardar uns minutos)")
		if err := execStream(exec.Command(brew, "install", "ansible"), emit); err != nil {
			emit("❌ erro instalando Ansible: " + err.Error())
			fin(false)
			return
		}
		emit("✅ Ansible instalado. Xa podes usar o motor Ansible (cambia o motor na barra).")
		fin(true)
	}()
}

// arranxarPATH engade os directorios de Homebrew ao PATH en macOS: as apps
// lanzadas dende Finder herdan un PATH mínimo que non inclúe /opt/homebrew/bin,
// así que sen isto non se atoparían ansible-playbook nin brew. /usr/local/sbin e
// /opt/salt son onde o paquete oficial de Salt deixa salt-ssh.
func arranxarPATH() {
	if runtime.GOOS != "darwin" {
		return
	}
	path := os.Getenv("PATH")
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/sbin", "/opt/salt"} {
		if _, err := os.Stat(d); err == nil && !strings.Contains(path, d) {
			path = d + string(os.PathListSeparator) + path
		}
	}
	os.Setenv("PATH", path)
}

// localizarBrew devolve a ruta do executable brew, ou "" se non está.
func localizarBrew() string {
	if p, err := exec.LookPath("brew"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// abrirTerminalConComando abre Terminal.app executando `cmd` (macOS).
func abrirTerminalConComando(cmd string) error {
	esc := strings.ReplaceAll(cmd, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	script := "tell application \"Terminal\"\n\tactivate\n\tdo script \"" + esc + "\"\nend tell"
	return exec.Command("osascript", "-e", script).Run()
}

// execStream executa `cmd` reenviando a súa saída liña a liña a `emit`.
func execStream(cmd *exec.Cmd, emit func(string)) error {
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			emit(sc.Text())
		}
		close(done)
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	return err
}

// Traducions expón as cadeas do idioma activo ao frontend (substitúe o Jinja t()).
func (a *App) Traducions() map[string]string { return a.tr.All() }

func (a *App) Idioma() string { return a.tr.Idioma() }

// IdiomaInfo é un idioma dispoñible para o selector de ⚙ Configuración.
type IdiomaInfo struct {
	Code string `json:"code"`
	Nome string `json:"nome"`
}

// IdiomasDisponibles é a lista pechada de idiomas con ficheiro .lang propio
// (recursos/idiomas/) — a mesma tríade de códigos (gl/es/en/pt) que xa usan
// Tao, Xesta e Pancho, para que un módulo lanzado herde sempre un idioma que
// tamén el sabe amosar (ver ContextoModulo.Idioma, escribirContexto).
func (a *App) IdiomasDisponibles() []IdiomaInfo {
	return []IdiomaInfo{
		{Code: "gl", Nome: "Galego"},
		{Code: "es", Nome: "Castellano"},
		{Code: "en", Nome: "English"},
		{Code: "pt", Nome: "Português"},
	}
}

// SetIdioma cambia o idioma activo de Piztu e persíste (internal/db, chave
// "idioma") para que sobreviva a un reinicio — ver a lectura equivalente en
// startup(). Devolve xa as traducións completas do idioma novo, para que o
// frontend non teña que facer unha segunda chamada a Traducions().
//
// Tao, Pancho e Xesta (xanela propia, proceso novo por lanzamento — ver
// AbrirModulo) non reciben este cambio en quente se xa están abertos: só
// lanzamentos futuros herdan o idioma novo coma valor por defecto (ver
// ContextoModulo.Idioma) — cada un mantén a súa propia elección explícita se
// xa a fixo.
func (a *App) SetIdioma(code string) (map[string]string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	soportado := false
	for _, d := range a.IdiomasDisponibles() {
		if d.Code == code {
			soportado = true
			break
		}
	}
	if !soportado {
		return nil, fmt.Errorf("idioma non soportado: %q", code)
	}
	a.tr.SetIdioma(code)
	if a.store != nil {
		if err := a.store.GardarAxuste("idioma", code); err != nil {
			return nil, err
		}
	}
	return a.tr.All(), nil
}

func (a *App) GetUmbral() int {
	if a.store == nil {
		return a.cfg.Ruido.UmbralDefecto
	}
	return a.store.LerUmbral()
}

func (a *App) GardarUmbral(v int) {
	if a.store == nil {
		return
	}
	a.store.GardarUmbral(v)
}

// GardarLayout persiste a posición dun equipo (← /gardar_layout).
func (a *App) GardarLayout(nome string, x, y float64) {
	if a.store == nil {
		return
	}
	a.store.GardarPosicion(nome, x, y)
}

// ── Interruptor de motor ─────────────────────────────────────────────────────

func (a *App) GetMotor() string { return motor.MotorActivo(a.cfg) }

// MotorBase devolve o motor "base" deste sistema (o defecto por-OS: nativo en
// macOS, salt en Linux). O frontend úsao para saber a que motor volver cando se
// desactiva Ansible.
func (a *App) MotorBase() string { return motor.MotorDefecto }

// MotorInfo describe un motor para o menú do botón ⚙.
type MotorInfo struct {
	Nome       string `json:"nome"`
	Etiqueta   string `json:"etiqueta"`
	Dispo      bool   `json:"dispo"`
	Motivo     string `json:"motivo"`
	Recomendar bool   `json:"recomendar"`
}

// MotoresDispo devolve os motores que teñen sentido neste sistema, co seu
// estado, para que o menú poida amosar en gris os que non se poden usar agora
// mesmo e explicar por que.
func (a *App) MotoresDispo() []MotorInfo {
	temAnsible := enPATH("ansible-playbook")

	info := []MotorInfo{
		{Nome: "ssh", Dispo: true},
	}
	// O motor Salt fala cos sockets dun salt-master local (`sudo -n salt`), que
	// en macOS non existe nin pode existir (ver saltsetup.ErrMasterNonSoportado).
	// Alí nin sequera se lista. Fóra de macOS non abonda con que exista o
	// binario `salt` —fai falla o master e a regra sudoers NOPASSWD—, pero o
	// binario é o mellor indicio barato que temos.
	if runtime.GOOS != "darwin" {
		info = append(info, MotorInfo{Nome: "salt", Dispo: enPATH("salt")})
	}
	info = append(info, MotorInfo{Nome: "ansible", Dispo: temAnsible})

	for i := range info {
		info[i].Etiqueta = motor.Etiquetas[info[i].Nome]
		info[i].Recomendar = info[i].Nome == motor.MotorDefecto
		if info[i].Dispo {
			continue
		}
		switch info[i].Nome {
		case "salt":
			info[i].Motivo = "salt-master non instalado"
		case "ansible":
			info[i].Motivo = "ansible non instalado (⚙ Instalar Ansible)"
		}
	}
	return info
}

func enPATH(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func (a *App) SetMotor(nome string) (string, error) {
	n, err := motor.SetMotor(a.cfg, nome)
	if err != nil {
		return "", err
	}
	emitirEvento("motor_cambiado", map[string]any{
		"motor": n, "accions": a.AccionsModulos(),
	})
	return n, nil
}

// ── Execución de accións (← control.executar_comando) ────────────────────────

func (a *App) ExecutarComando(accion, target string) {
	// Se a acción vén dun módulo, ten que estar rexistrada — e só se rexistran
	// as dos módulos activos. Sen isto, un frontend desactualizado podería
	// executar a acción dun apeiro apagado.
	if !modulos.AccionsReservadas[accion] {
		if _, ok := a.cfg.AccionModulo(accion); !ok {
			emitirEvento("comando_resultado", map[string]any{
				"accion": accion, "target": target, "estado": "erro",
				"msg": "acción descoñecida ou dun módulo desactivado",
			})
			return
		}
	}
	m := motor.Get(a.cfg, "")
	hosts := a.resolverDestinos([]string{target}, m)
	if len(hosts) == 0 {
		return
	}
	go func() {
		cb := motor.Callbacks{
			OnOk: func(h, _ string) {
				emitirEvento("comando_resultado",
					map[string]any{"accion": accion, "host": h, "estado": "ok"})
			},
			OnErro: func(h, out string) {
				emitirEvento("comando_resultado",
					map[string]any{"accion": accion, "host": h, "estado": "erro", "msg": extraerErro(out)})
			},
		}
		if err := m.ExecutarAccion(accion, hosts, cb); err != nil {
			emitirEvento("comando_resultado",
				map[string]any{"accion": accion, "target": target, "estado": "erro", "msg": err.Error()})
			return
		}
		emitirEvento("comando_completo",
			map[string]any{"accion": accion, "total": len(hosts)})
	}()
}

// ── Bloqueo configurable (← btn-bloqueo, premido 3s para configurar) ────────

// GetOpcionsBloqueo devolve que se bloquea por defecto (claves "internet",
// "ssh", "son", "rato", "teclado"). Lémbrase entre sesións (bloqueo_opcions.json).
func (a *App) GetOpcionsBloqueo() map[string]bool {
	if a.store == nil {
		return map[string]bool{"internet": false, "ssh": false, "son": false, "rato": true, "teclado": true}
	}
	return a.store.LerOpcionsBloqueo()
}

// GardarOpcionsBloqueo persiste a configuración escollida na pantalla de
// opcións (premido 3s sobre "Bloquear").
func (a *App) GardarOpcionsBloqueo(opcions map[string]bool) {
	if a.store == nil {
		return
	}
	a.store.GardarOpcionsBloqueo(opcions)
}

// ExecutarBloqueo bloquea `target` cos elementos marcados na configuración
// gardada (← clic simple sobre "Bloquear", ou o botón LOK dun equipo). Mesmo
// patrón de eventos ca ExecutarComando, con accion="bloqueoTotal" fixo para
// que o frontend poida tratalo coma calquera outro resultado de comando.
func (a *App) ExecutarBloqueo(target string) {
	const accion = "bloqueoTotal"
	m := motor.Get(a.cfg, "")
	hosts := a.resolverDestinos([]string{target}, m)
	if len(hosts) == 0 {
		return
	}
	opcions := a.GetOpcionsBloqueo()
	go func() {
		cb := motor.Callbacks{
			OnOk: func(h, _ string) {
				emitirEvento("comando_resultado",
					map[string]any{"accion": accion, "host": h, "estado": "ok"})
			},
			OnErro: func(h, out string) {
				emitirEvento("comando_resultado",
					map[string]any{"accion": accion, "host": h, "estado": "erro", "msg": extraerErro(out)})
			},
		}
		if err := m.ExecutarBloqueo(hosts, opcions, cb); err != nil {
			emitirEvento("comando_resultado",
				map[string]any{"accion": accion, "target": target, "estado": "erro", "msg": err.Error()})
			return
		}
		emitirEvento("comando_completo",
			map[string]any{"accion": accion, "total": len(hosts)})
	}()
}

// ── Terminal SSH (← control ssh_*) ───────────────────────────────────────────

func (a *App) SSHOpen(host string) {
	variables := inventory.LerHostsConVars(a.cfg.HostsFile)[host]
	destino := inventory.ResolverHostCached(host, inventory.ExtraerMac(variables))
	err := a.ssh.Open(destino,
		func(out string) { emitirEvento("ssh_data", map[string]any{"output": out}) },
		func() { emitirEvento("ssh_closed", map[string]any{}) },
	)
	if err != nil {
		emitirEvento("ssh_data",
			map[string]any{"output": "\r\n\033[1;31m[ERRO SSH] " + err.Error() + "\033[0m\r\n"})
		emitirEvento("ssh_closed", map[string]any{})
	}
}

func (a *App) SSHInput(data string)     { a.ssh.Input(data) }
func (a *App) SSHResize(cols, rows int) { a.ssh.Resize(cols, rows) }
func (a *App) SSHClose()                { a.ssh.Close() }

// ── Prácticas: seleccionar / enviar / limpar / recoller ──────────────────────

// SelectFicheiros abre o diálogo nativo e devolve as rutas escollidas.
func (a *App) SelectFicheiros() ([]string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle(a.tr.T("envio.ficheiros")).
		PromptForMultipleSelection()
}

// EnviarPracticas envía os ficheiros aos destinos. Devolve op_id. (← ficheiros.enviar_practicas)
func (a *App) EnviarPracticas(rutas []string, destinos []string) string {
	m := motor.Get(a.cfg, "")
	hosts := a.resolverDestinos(destinos, m)
	opID := uuid.NewString()
	// O módulo pode estar apagado: os bindings de Wails seguen sendo chamables
	// aínda que o botón non se vexa, así que a comprobación ten que estar aquí.
	if !a.moduloActivo(modulos.Ficheiros) {
		return opID
	}
	if len(hosts) == 0 || len(rutas) == 0 {
		return opID
	}
	total := len(hosts)
	var mu sync.Mutex
	okN, errN := 0, 0

	go func() {
		cb := motor.Callbacks{
			OnInicio: func(h string) {
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "enviando", "total": total,
				})
			},
			OnOk: func(h, _ string) {
				mu.Lock()
				okN++
				actual := okN + errN
				mu.Unlock()
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "ok", "actual": actual, "total": total, "msg": "",
				})
			},
			OnErro: func(h, out string) {
				mu.Lock()
				errN++
				actual := okN + errN
				mu.Unlock()
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "erro", "actual": actual, "total": total, "msg": extraerErro(out),
				})
			},
		}
		m.EnviarFicheiros(hosts, rutas, cb)
		emitirEvento("envio_completo", map[string]any{
			"op_id": opID, "total": total, "ok": okN, "erros": errN,
		})
	}()
	return opID
}

// LimparPracticas borra as prácticas nos destinos. Devolve op_id.
func (a *App) LimparPracticas(destinos []string) string {
	m := motor.Get(a.cfg, "")
	hosts := a.resolverDestinos(destinos, m)
	opID := uuid.NewString()
	// O módulo pode estar apagado: os bindings de Wails seguen sendo chamables
	// aínda que o botón non se vexa, así que a comprobación ten que estar aquí.
	if !a.moduloActivo(modulos.Ficheiros) {
		return opID
	}
	if len(hosts) == 0 {
		return opID
	}
	total := len(hosts)
	var mu sync.Mutex
	okN, errN := 0, 0
	go func() {
		cb := motor.Callbacks{
			OnInicio: func(h string) {
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "enviando", "total": total})
			},
			OnOk: func(h, _ string) {
				mu.Lock()
				okN++
				actual := okN + errN
				mu.Unlock()
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "ok", "actual": actual, "total": total})
			},
			OnErro: func(h, out string) {
				mu.Lock()
				errN++
				actual := okN + errN
				mu.Unlock()
				emitirEvento("progreso_envio", map[string]any{
					"op_id": opID, "host": h, "estado": "erro", "actual": actual, "total": total, "msg": extraerErro(out)})
			},
		}
		m.LimparPracticas(hosts, cb)
		emitirEvento("envio_completo", map[string]any{
			"op_id": opID, "total": total, "ok": okN, "erros": errN})
	}()
	return opID
}

// RecollerPracticas recolle os traballos dos destinos. Devolve op_id.
func (a *App) RecollerPracticas(destinos []string) string {
	m := motor.Get(a.cfg, "")
	hosts := a.resolverDestinos(destinos, m)
	opID := uuid.NewString()
	// O módulo pode estar apagado: os bindings de Wails seguen sendo chamables
	// aínda que o botón non se vexa, así que a comprobación ten que estar aquí.
	if !a.moduloActivo(modulos.Ficheiros) {
		return opID
	}
	if len(hosts) == 0 {
		return opID
	}
	total := len(hosts)
	var mu sync.Mutex
	resultados := map[string]map[string]any{}
	go func() {
		cb := motor.Callbacks{
			OnInicio: func(h string) {
				mu.Lock()
				actual := len(resultados)
				mu.Unlock()
				emitirEvento("progreso_recollida", map[string]any{
					"op_id": opID, "host": h, "estado": "conectando", "actual": actual, "total": total})
			},
			OnOk: func(h, _ string) {
				fs := a.ficheirosLocais(h)
				mu.Lock()
				resultados[h] = map[string]any{"ok": true, "ficheiros": fs}
				actual := len(resultados)
				mu.Unlock()
				emitirEvento("progreso_recollida", map[string]any{
					"op_id": opID, "host": h, "estado": "ok", "actual": actual, "total": total, "ficheiros": fs, "msg": ""})
			},
			OnErro: func(h, out string) {
				mu.Lock()
				resultados[h] = map[string]any{"ok": false, "erro": extraerErro(out)}
				actual := len(resultados)
				mu.Unlock()
				emitirEvento("progreso_recollida", map[string]any{
					"op_id": opID, "host": h, "estado": "erro", "actual": actual, "total": total, "msg": extraerErro(out)})
			},
		}
		m.RecollerPracticas(hosts, cb)
		emitirEvento("recollida_completa", map[string]any{
			"op_id": opID, "resultados": resultados, "total": total})
	}()
	return opID
}

// ── Explorador de ficheiros locais (← ficheiros.listar_ficheiros) ────────────

// FicheiroInfo describe un ficheiro recollido.
type FicheiroInfo struct {
	Nome       string `json:"nome"`
	Tamano     int64  `json:"tamaño"`
	Modificado int64  `json:"modificado"`
}

// AbrirFicheiro abre un ficheiro recollido co xestor/aplicación por defecto
// do sistema (← main.py:abrir_cartafol_equipo, que usaba QDesktopServices).
func (a *App) AbrirFicheiro(host, nome string) error {
	ruta := filepath.Join(a.cfg.PracticasDir, host, nome)
	if _, err := os.Stat(ruta); err != nil {
		return err
	}
	return exec.Command("xdg-open", ruta).Start()
}

func (a *App) ListarFicheiros(host string) map[string]any {
	dir := filepath.Join(a.cfg.PracticasDir, host)
	fics := []FicheiroInfo{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil {
				fics = append(fics, FicheiroInfo{
					Nome: e.Name(), Tamano: info.Size(), Modificado: info.ModTime().Unix(),
				})
			}
		}
	}
	return map[string]any{"host": host, "ficheiros": fics}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (a *App) resolverDestinos(destinos []string, m motor.Motor) []string {
	equipos := m.GetEquipos()
	// "todos"/baleiro/nome do grupo → todos os equipos.
	todos := len(destinos) == 0
	for _, d := range destinos {
		if d == "" || d == "todos" || d == "all" ||
			d == a.cfg.Ansible.GrupoAula || d == a.cfg.Salt.GrupoAula {
			todos = true
		}
	}
	if todos {
		return equipos
	}
	known := map[string]bool{}
	for _, e := range equipos {
		known[e] = true
	}
	res := []string{}
	for _, d := range destinos {
		if known[d] {
			res = append(res, d)
		}
	}
	return res
}

func (a *App) ficheirosLocais(host string) []string {
	dir := filepath.Join(a.cfg.PracticasDir, host)
	os.MkdirAll(dir, 0o755)
	res := []string{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				res = append(res, e.Name())
			}
		}
	}
	return res
}

func ultimaLinea(s string) string {
	linas := strings.Split(s, "\n")
	for i := len(linas) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(linas[i]); t != "" {
			return t
		}
	}
	return "erro"
}

var msgRe = regexp.MustCompile(`"msg":\s*"((?:[^"\\]|\\.)*)"`)

// extraerErro busca no output de ansible-playbook a mensaxe real dun fallo
// (liñas 'fatal: ... => {"msg": "..."}'). Se non atopa ningunha, cae á
// última liña non baleira coma respaldo. Porte de extraer_erro de core/ansible.py.
func extraerErro(s string) string {
	linas := strings.Split(s, "\n")
	for i := len(linas) - 1; i >= 0; i-- {
		line := linas[i]
		if strings.Contains(line, "fatal:") || strings.Contains(line, "FAILED!") || strings.Contains(line, "UNREACHABLE!") {
			if m := msgRe.FindStringSubmatch(line); m != nil {
				if unquoted, err := strconv.Unquote(`"` + m[1] + `"`); err == nil {
					return unquoted
				}
				return m[1]
			}
			if t := strings.TrimSpace(line); t != "" {
				return t
			}
		}
	}
	return ultimaLinea(s)
}
