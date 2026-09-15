// Package ruido é o porte de ruido.py: monitor de ruído por micrófono que
// avisa e bloquea a aula cando o nivel de son persiste por riba do limiar, e
// despois verifica e reintenta o desbloqueo. Agnóstico de Wails: comunica
// resultados a través de Eventos (callbacks), non importa wails/runtime —
// o wiring a eventos de UI faino app.go, igual que core/ nunca importaba
// flask/PyQt6 no proxecto orixinal.
package ruido

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"

	"piztu/internal/config"
	"piztu/internal/inventory"
	"piztu/internal/motor"
)

// Estado é un instante do vúmetro (← payload de /actualizar_vumetro).
type Estado struct {
	Nivel      int
	NivelMedio int
	Estado     int // 0=normal 1=aviso 2=bloqueo
}

// Eventos son os "callbacks" que fornece a capa de presentación (app.go).
type Eventos struct {
	OnVumetro      func(Estado)
	OnNotificacion func(msg, tipo string)
}

// Monitor captura audio, calcula niveis e dispara aviso/bloqueo/desbloqueo.
// Sempre usa o motor Ansible: ruido.py orixinal nunca pasaba pola
// abstracción de motor.Get, chamaba "ansible-playbook" directamente.
type Monitor struct {
	cfg    *config.Config
	m      motor.Motor
	ev     Eventos
	umbral func() int

	mu       sync.Mutex
	activo   bool
	cancelar context.CancelFunc
}

// Novo crea un Monitor. umbral debe ler o limiar vixente en cada chamada
// (ex. a.store.LerUmbral) para que os cambios feitos na UI xurdan efecto
// sen reiniciar o monitor.
func Novo(cfg *config.Config, umbral func() int, ev Eventos) *Monitor {
	return &Monitor{cfg: cfg, m: motor.Get(cfg, "ansible"), ev: ev, umbral: umbral}
}

// Iniciar arranca a captura e o bucle principal nunha goroutine, se non
// están xa activos.
func (mon *Monitor) Iniciar() {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	if mon.activo {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	mon.cancelar = cancel
	mon.activo = true
	go mon.bucle(ctx)
}

// Detener para a captura e o bucle principal.
func (mon *Monitor) Detener() {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	if !mon.activo {
		return
	}
	mon.cancelar()
	mon.activo = false
}

func (mon *Monitor) Activo() bool {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	return mon.activo
}

func (mon *Monitor) notificar(msg, tipo string) {
	if mon.ev.OnNotificacion != nil {
		mon.ev.OnNotificacion(msg, tipo)
	}
}

func (mon *Monitor) vumetro(e Estado) {
	if mon.ev.OnVumetro != nil {
		mon.ev.OnVumetro(e)
	}
}

// ── Captura de audio (← _hilo_lector / _novo_proceso_arecord) ───────────────

// audioQueue é un buffer circular das últimas 10 mostras (← deque(maxlen=10)).
type audioQueue struct {
	mu   sync.Mutex
	bufs [][]byte
}

const audioQueueMax = 10

func (q *audioQueue) push(b []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.bufs = append(q.bufs, b)
	for len(q.bufs) > audioQueueMax {
		q.bufs = q.bufs[1:]
	}
}

func (q *audioQueue) pop() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.bufs) == 0 {
		return nil, false
	}
	b := q.bufs[0]
	q.bufs = q.bufs[1:]
	return b, true
}

// capturarAudio captura do micrófono con malgo (miniaudio) e empurra mostras de
// tamaño ChunkBytes() á cola. É multiplataforma: CoreAudio en macOS, ALSA en
// Linux — mesmo código, sen depender de `arecord`. O formato (S16_LE, mono,
// Rate) é idéntico ao anterior, así que o resto do monitor non cambia.
func (mon *Monitor) capturarAudio(ctx context.Context, q *audioQueue) {
	r := mon.cfg.Ruido
	chunkBytes := r.ChunkBytes()

	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		println("[RUÍDO] non se puido iniciar o audio:", err.Error())
		return
	}
	defer func() { _ = mctx.Uninit(); mctx.Free() }()

	devCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	devCfg.Capture.Format = malgo.FormatS16
	devCfg.Capture.Channels = uint32(r.Channels)
	devCfg.SampleRate = uint32(r.Rate)
	devCfg.Alsa.NoMMap = 1

	// A "orixe por defecto" de PulseAudio pode estar apuntando ao monitor da
	// saída (o que soa polos altofalantes) en vez de ao micrófono real —
	// configuración do sistema, non de Piztu, pero bastante común en Linux
	// (menos en macOS/CoreAudio) e o resultado é un vúmetro que nunca se move
	// sen dar ningún erro. Se hai un dispositivo de entrada real dispoñible,
	// escóllese explicitamente el en vez de confiar no "por defecto".
	if devs, err := mctx.Devices(malgo.Capture); err == nil {
		for i := range devs {
			nome := strings.ToLower(devs[i].Name())
			if strings.Contains(nome, "monitor") {
				continue
			}
			id := devs[i].ID
			devCfg.Capture.DeviceID = id.Pointer()
			println("[RUÍDO] micrófono:", devs[i].Name())
			break
		}
	}

	// O callback pode entregar bloques de calquera tamaño; acumúlanse e córtanse
	// en chunks de chunkBytes (protexido por mutex: o callback corre noutro fío).
	var bufMu sync.Mutex
	buf := make([]byte, 0, chunkBytes*2)
	onData := func(_, entrada []byte, frames uint32) {
		bufMu.Lock()
		buf = append(buf, entrada...)
		for len(buf) >= chunkBytes {
			chunk := make([]byte, chunkBytes)
			copy(chunk, buf[:chunkBytes])
			q.push(chunk)
			buf = buf[chunkBytes:]
		}
		bufMu.Unlock()
	}

	device, err := malgo.InitDevice(mctx.Context, devCfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		println("[RUÍDO] non se puido abrir o micrófono:", err.Error())
		return
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		println("[RUÍDO] non se puido arrancar a captura:", err.Error())
		return
	}
	<-ctx.Done() // captura ata que se detén o monitor
	_ = device.Stop()
}

func rmsAPorcentaxe(rms float64) int {
	if rms <= 0 {
		return 0
	}
	db := 20 * math.Log10(rms/32768.0)
	pct := (db + 60) / 60 * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return int(pct)
}

// ── Bucle principal (← "while True" de ruido.py) ────────────────────────────

type mostra struct {
	t     time.Time
	nivel int
}

func (mon *Monitor) bucle(ctx context.Context) {
	r := mon.cfg.Ruido
	q := &audioQueue{}
	go mon.capturarAudio(ctx, q)

	var historico []mostra
	estadoAlerta := 0
	var tAviso time.Time
	ciclo := 0
	segPromedio := time.Duration(r.SegPromedio) * time.Second

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	println(fmt.Sprintf("--- 🎙️ Control de Ruído Activo (Media %ds · bloqueo tras %ds) ---",
		r.SegPromedio, r.SegBloqueo))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		nivelInstan := 0
		if chunk, ok := q.pop(); ok {
			n := len(chunk) / 2
			if n > 0 {
				var suma float64
				for i := 0; i < n; i++ {
					v := int16(binary.LittleEndian.Uint16(chunk[i*2 : i*2+2]))
					suma += float64(v) * float64(v)
				}
				nivelInstan = rmsAPorcentaxe(math.Sqrt(suma / float64(n)))
			}
		}

		agora := time.Now()
		historico = append(historico, mostra{agora, nivelInstan})
		for len(historico) > 0 && agora.Sub(historico[0].t) > segPromedio {
			historico = historico[1:]
		}
		sumaNiveis := 0
		for _, h := range historico {
			sumaNiveis += h.nivel
		}
		nivelMedio := sumaNiveis / len(historico)

		umbral := r.UmbralDefecto
		if mon.umbral != nil {
			umbral = mon.umbral()
		}

		ciclo++
		enWarmup := ciclo <= r.WarmupCiclos

		mon.vumetro(Estado{Nivel: nivelInstan, NivelMedio: nivelMedio, Estado: estadoAlerta})

		switch {
		case !enWarmup && nivelMedio > umbral:
			switch estadoAlerta {
			case 0:
				estadoAlerta = 1
				tAviso = agora
				mon.dispararAsync("aviso_ruido")
			case 1:
				if agora.Sub(tAviso) >= time.Duration(r.SegBloqueo)*time.Second {
					estadoAlerta = 2
					mon.dispararAsync("bloqueo")

					select {
					case <-ctx.Done():
						return
					case <-time.After(60 * time.Second):
					}

					mon.dispararSync("liberar")
					go mon.verificarEReintentarDesbloqueo(ctx)

					estadoAlerta = 0
					historico = historico[:0]
				}
			}
		case !enWarmup && nivelMedio < umbral-5:
			estadoAlerta = 0
		}
	}
}

// ── Disparo de accións (← _run_playbook / _run_playbook_sync) ──────────────

func (mon *Monitor) todosHosts() []string {
	return inventory.GetEquipos(mon.cfg.HostsFile)
}

// dispararAsync lanza a acción sen agardar (← Popen sen .wait()).
func (mon *Monitor) dispararAsync(nomeLogico string) {
	hosts := mon.todosHosts()
	if len(hosts) == 0 {
		return
	}
	go mon.m.ExecutarAccion(nomeLogico, hosts, motor.Callbacks{})
}

// dispararSync lanza a acción e agarda a que remate en todos os hosts
// (← Popen + .wait()).
func (mon *Monitor) dispararSync(nomeLogico string) {
	hosts := mon.todosHosts()
	if len(hosts) == 0 {
		return
	}
	_ = mon.m.ExecutarAccion(nomeLogico, hosts, motor.Callbacks{})
}

// ── Verificación e reintentos de desbloqueo ─────────────────────────────────

func (mon *Monitor) verificarEReintentarDesbloqueo(ctx context.Context) {
	r := mon.cfg.Ruido
	if !r.VerificarDesbloqueo {
		return
	}

	if !esperarOuCancelar(ctx, time.Duration(r.EsperaInicial)*time.Second) {
		return
	}

	hosts := mon.todosHosts()
	if len(hosts) == 0 {
		println("[VERIF] Non se atoparon hosts no inventario.")
		return
	}

	mon.notificar(fmt.Sprintf("🔍 Verificando desbloqueo en %d equipo(s)…", len(hosts)), "info")

	var mu sync.Mutex
	var bloqueados []string
	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			if mon.comprobarBloqueado(host) {
				mu.Lock()
				bloqueados = append(bloqueados, host)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()

	if len(bloqueados) == 0 {
		mon.notificar(fmt.Sprintf("✅ Todos os equipos (%d) desbloqueados correctamente.", len(hosts)), "ok")
		return
	}

	mon.notificar(fmt.Sprintf("⚠️ %d equipo(s) aínda bloqueado(s): %s",
		len(bloqueados), strings.Join(bloqueados, ", ")), "aviso")

	pendentes := append([]string(nil), bloqueados...)
	for intento := 1; intento <= r.MaxReintentos && len(pendentes) > 0; intento++ {
		mon.notificar(fmt.Sprintf("🔄 Reintento %d/%d: %s",
			intento, r.MaxReintentos, strings.Join(pendentes, ", ")), "aviso")

		var aunFallidos []string
		for _, host := range pendentes {
			if mon.desbloquearHostIndividual(host) {
				if !esperarOuCancelar(ctx, 5*time.Second) {
					return
				}
				if mon.comprobarBloqueado(host) {
					aunFallidos = append(aunFallidos, host)
				} else {
					mon.notificar(fmt.Sprintf("✅ %s desbloqueado (intento %d)", host, intento), "ok")
				}
			} else {
				aunFallidos = append(aunFallidos, host)
			}
		}
		pendentes = aunFallidos

		if len(pendentes) > 0 && intento < r.MaxReintentos {
			if !esperarOuCancelar(ctx, time.Duration(r.EsperaEntreIntentos)*time.Second) {
				return
			}
		}
	}

	if len(pendentes) > 0 {
		mon.notificar(fmt.Sprintf("🚨 Non se puido desbloquear tras %d intentos: %s. Require intervención manual.",
			r.MaxReintentos, strings.Join(pendentes, ", ")), "erro")
	} else {
		mon.notificar("✅ Todos os equipos desbloqueados correctamente tras reintentos.", "ok")
	}
}

func esperarOuCancelar(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (mon *Monitor) desbloquearHostIndividual(host string) bool {
	exito := false
	err := mon.m.ExecutarAccion("liberar", []string{host}, motor.Callbacks{
		OnOk: func(_ string, _ string) { exito = true },
	})
	return err == nil && exito
}

// comprobarBloqueado (← _comprobar_bloqueado): usa o playbook comprobar_bloqueo
// se existe, senón cae a unha comprobación directa por SSH.
func (mon *Monitor) comprobarBloqueado(host string) bool {
	playbookComp := mon.cfg.PlaybookPath("comprobar_bloqueo")
	if _, err := os.Stat(playbookComp); err != nil {
		return mon.comprobarBloqueadoSSH(host)
	}

	inv, err := motor.InventarioTemporal(mon.cfg, []string{host})
	if err != nil {
		return true
	}
	defer os.Remove(inv)

	saida, rc := motor.RunAnsiblePlaybook(host, playbookComp, inv)
	if strings.Contains(saida, "unreachable=1") || rc == 4 {
		// Inaccesible (apagado, sen rede…) non é o mesmo que bloqueado: se se
		// tratase coma "aínda bloqueado" aquí, un equipo simplemente apagado
		// faría que verificarEReintentarDesbloqueo reintentase para sempre e
		// acabase avisando "require intervención manual" sen que houbese
		// ningún bloqueo real — exactamente o falso aviso constante que se
		// quería evitar. Non se pode confirmar → non se insiste.
		return false
	}
	if strings.Contains(saida, "changed=1") && strings.Contains(saida, "failed=0") {
		return true // BLOQUEADO
	}
	return false // libre
}

func (mon *Monitor) comprobarBloqueadoSSH(host string) bool {
	cmd := exec.Command("ssh",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		mon.cfg.SSHUser+"@"+host,
		"pgrep -x xtrlock || pgrep -f 'bloqueo' || echo LIBRE",
	)
	out, err := cmd.CombinedOutput()
	saida := strings.TrimSpace(string(out))
	if err != nil || saida == "" {
		// Sen conexión SSH non se pode confirmar nada (mesmo razoamento que
		// en comprobarBloqueado): non tratar coma "bloqueado".
		return false
	}
	return !strings.Contains(saida, "LIBRE")
}
