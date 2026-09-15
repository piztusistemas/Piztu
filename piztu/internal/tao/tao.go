// Package tao é o porte de core/tao.py: le as sesións que Tao publica en
// File Browser (dixitalizacion/sesions/<equipo>.json) para saber
// quen está a usar cada equipo, e as difunde en bucle periódico coma un
// Monitor máis (espello de internal/ping). Agnóstico de Wails.
package tao

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"piztu/internal/config"
)

// helperBinName é o nome do binario auxiliar (ver cmd/taohelper), que
// vive canda piztu en cfg.BaseDir. Só ese binario leva o bit setgid
// www-data — piztu nunca o leva, porque GTK recusa inicializarse en
// calquera proceso setuid/setgid (ver cmd/taohelper/main.go).
const helperBinName = "tao-helper"

// Sesion é o estado dun equipo, tal e como o consome o frontend (mesmo
// contrato JSON que /estado_tao en blueprints/tao.py). Ve tamén
// dixitalizacion/filebrowser.go (sesionTao) no lado de quen escribe.
type Sesion struct {
	Usuario     string `json:"usuario"`
	Nome        string `json:"nome"`
	Grupo       string `json:"grupo"`
	Conectado   bool   `json:"conectado"`
	Actualizado string `json:"actualizado"`
	Vivo        bool   `json:"vivo"`
}

type sesionBruta struct {
	Equipo      string `json:"equipo"`
	Usuario     string `json:"usuario"`
	Nome        string `json:"nome"`
	Grupo       string `json:"grupo"`
	Conectado   bool   `json:"conectado"`
	Actualizado string `json:"actualizado"`
}

// Listar devolve {equipo: Sesion}, lendo cfg.SesionsDir()/*.json a través do
// axudante setgid tao-helper (piztu en si mesmo nunca ten permiso
// directo para entrar en dixitalizacion/, 750 www-data:www-data).
//
// Tolerante: se o axudante non existe, falla, ou o cartafol de sesións non
// existe/non hai permiso, devolve un mapa baleiro; un ficheiro individual
// corrompido sáltase sen afectar aos demais.
func Listar(cfg *config.Config) map[string]Sesion {
	resultado := map[string]Sesion{}

	helper := filepath.Join(cfg.BaseDir, helperBinName)
	saida, err := exec.Command(helper, cfg.SesionsDir()).Output()
	if err != nil {
		return resultado
	}

	var brutos map[string]json.RawMessage
	if err := json.Unmarshal(saida, &brutos); err != nil {
		return resultado
	}

	caducidade := time.Duration(cfg.Tao.CaducidadeSeg) * time.Second
	agora := time.Now()

	for nomeFicheiro, raw := range brutos {
		var bruto sesionBruta
		if err := json.Unmarshal(raw, &bruto); err != nil {
			continue
		}
		equipo := bruto.Equipo
		if equipo == "" {
			equipo = nomeFicheiro
		}
		resultado[equipo] = Sesion{
			Usuario:     bruto.Usuario,
			Nome:        bruto.Nome,
			Grupo:       bruto.Grupo,
			Conectado:   bruto.Conectado,
			Actualizado: bruto.Actualizado,
			Vivo:        bruto.Conectado && recente(bruto.Actualizado, agora, caducidade),
		}
	}
	return resultado
}

// recente reporta se actualizadoISO (RFC3339, como o escribe Go en
// dixitalizacion/filebrowser.go) é máis novo que a caducidade configurada.
func recente(actualizadoISO string, agora time.Time, caducidade time.Duration) bool {
	if actualizadoISO == "" {
		return false
	}
	marca, err := time.Parse(time.RFC3339, actualizadoISO)
	if err != nil {
		return false
	}
	return agora.Sub(marca) < caducidade
}

// ── Monitor (bucle periódico, espello de internal/ping.Monitor) ─────────────

// Eventos son os "callbacks" que fornece a capa de presentación (app.go).
type Eventos struct {
	OnEstado func(map[string]Sesion)
}

// intervaloPolling: a fonte só cambia como moito cada 4 min (heartbeat de
// Tao) ou inmediatamente en login/logout, así que 10s abonda sen carga.
const intervaloPolling = 10 * time.Second

// Monitor faz polling do cartafol de sesións cada intervaloPolling.
type Monitor struct {
	cfg *config.Config
	ev  Eventos

	mu       sync.Mutex
	activo   bool
	cancelar context.CancelFunc
}

func Novo(cfg *config.Config, ev Eventos) *Monitor {
	return &Monitor{cfg: cfg, ev: ev}
}

// Iniciar arranca o bucle de polling nunha goroutine, se non está xa activo.
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

// Detener para o bucle de polling.
func (mon *Monitor) Detener() {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	if !mon.activo {
		return
	}
	mon.cancelar()
	mon.activo = false
}

func (mon *Monitor) bucle(ctx context.Context) {
	for {
		if mon.ev.OnEstado != nil {
			mon.ev.OnEstado(Listar(mon.cfg))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(intervaloPolling):
		}
	}
}
