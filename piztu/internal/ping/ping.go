// Package ping é o porte de main.py:_iniciar_ping_loop/_ping_host — comproba
// por ICMP se cada equipo do inventario está acendido, en bucle periódico, e
// notifica o resultado por Eventos. Agnóstico de Wails, coma internal/ruido.
package ping

import (
	"context"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"piztu/internal/config"
	"piztu/internal/inventory"
)

// Eventos son os "callbacks" que fornece a capa de presentación (app.go).
type Eventos struct {
	OnEstado func(host string, online bool)
}

// Monitor lanza un ping por equipo cada PingInterval segundos.
type Monitor struct {
	cfg       *config.Config
	equiposFn func() []string
	ev        Eventos

	mu       sync.Mutex
	activo   bool
	cancelar context.CancelFunc
}

// Novo crea un Monitor. equiposFn debe devolver a lista de hostnames vixente
// en cada pasada (ex. motor.Get(cfg, "").GetEquipos), para reflectir cambios
// no inventario sen reiniciar o monitor.
func Novo(cfg *config.Config, equiposFn func() []string, ev Eventos) *Monitor {
	return &Monitor{cfg: cfg, equiposFn: equiposFn, ev: ev}
}

// Iniciar arranca o bucle de ping nunha goroutine, se non está xa activo.
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

// Detener para o bucle de ping.
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
	intervalo := time.Duration(mon.cfg.UI.PingInterval) * time.Second
	if intervalo <= 0 {
		intervalo = 5 * time.Second
	}
	for {
		for _, host := range mon.equiposFn() {
			go mon.pingHost(host)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(intervalo):
		}
	}
}

func (mon *Monitor) pingHost(host string) {
	timeout := mon.cfg.UI.PingTimeout
	if timeout <= 0 {
		timeout = 1
	}
	variables := inventory.LerHostsConVars(mon.cfg.HostsFile)[host]
	destino := inventory.ResolverHostCached(host, inventory.ExtraerMac(variables))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+1)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(timeout), destino)
	online := cmd.Run() == nil

	if mon.ev.OnEstado != nil {
		mon.ev.OnEstado(host, online)
	}
}
