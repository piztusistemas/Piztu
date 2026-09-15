// Package recolector executa periodicamente a orde que declara un módulo en
// todos os equipos da aula e publica o resultado para pintalo no mapa.
//
// É o espello de internal/ping e internal/tao: un bucle con callbacks, agnóstico
// de Wails. De feito resolve o mesmo problema ca internal/tao (amosar quen usa
// cada equipo), pero de forma xenérica e sen depender do File Browser.
//
// Vai por SSH directo (internal/sshkey) e NON polo motor activo, aínda que os
// motores tamén saberían facelo. O motivo é o custo medido: unha acción por
// Salt-SSH tarda uns 6 s e por SSH directo 0,6 s. Un recolector que se repite
// cada 15 ou 30 segundos sobre trinta equipos non pode ir polo camiño caro; e
// ademais isto é ler un dato sen privilexios, non aplicar un estado.
package recolector

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"piztu/internal/config"
	"piztu/internal/inventory"
	"piztu/internal/modulos"
	"piztu/internal/sshkey"
)

// maxSimultaneos limita as conexións SSH á vez, coma no resto de piztu.
const maxSimultaneos = 8

// Dato é o valor publicado por un módulo para un equipo.
type Dato struct {
	Valor string    `json:"valor"`
	Visto time.Time `json:"-"`
}

// Eventos son os callbacks da capa de presentación.
type Eventos struct {
	// OnPublicacions recibe {modulo: {equipo: valor}} tras cada volta.
	OnPublicacions func(pub map[string]map[string]string)
}

// Monitor executa os recolectores de todos os módulos activos.
type Monitor struct {
	cfg *config.Config
	ev  Eventos

	// modulosFn devolve os módulos ACTIVOS con recolector, consultado en cada
	// volta para reflectir que o profesor acenda ou apague módulos sen reiniciar.
	modulosFn func() []modulos.Modulo

	mu       sync.Mutex
	activo   bool
	cancelar context.CancelFunc

	datosMu sync.Mutex
	datos   map[string]map[string]Dato // modulo -> equipo -> dato
}

// equipos devolve os equipos do inventario, o mesmo que ve o mapa.
func (mon *Monitor) equipos() []string {
	return inventory.GetEquipos(mon.cfg.HostsFile)
}

func Novo(cfg *config.Config, modulosFn func() []modulos.Modulo, ev Eventos) *Monitor {
	return &Monitor{cfg: cfg, modulosFn: modulosFn, ev: ev, datos: map[string]map[string]Dato{}}
}

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

func (mon *Monitor) Detener() {
	mon.mu.Lock()
	defer mon.mu.Unlock()
	if !mon.activo {
		return
	}
	mon.cancelar()
	mon.activo = false
}

// bucle vai espertando cada segundo e executa os recolectores que xa lles toca.
// Un tic curto cun control de "cando toca" por módulo permite que cada un teña o
// seu propio intervalo sen un temporizador por módulo.
func (mon *Monitor) bucle(ctx context.Context) {
	seguinte := map[string]time.Time{}
	for {
		agora := time.Now()
		var pendentes []modulos.Modulo
		for _, m := range mon.modulosFn() {
			if m.Recolector == nil {
				continue
			}
			if t, hai := seguinte[m.ID]; hai && agora.Before(t) {
				continue
			}
			seguinte[m.ID] = agora.Add(time.Duration(m.Recolector.Segundos()) * time.Second)
			pendentes = append(pendentes, m)
		}
		for _, m := range pendentes {
			mon.unhaVolta(m)
		}
		if len(pendentes) > 0 {
			mon.emitir()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// unhaVolta executa a orde do módulo en todos os equipos, en paralelo.
func (mon *Monitor) unhaVolta(m modulos.Modulo) {
	equipos := mon.equipos()
	usuario := mon.cfg.SSHUser
	if usuario == "" {
		usuario = "usuario"
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxSimultaneos)
	for _, h := range equipos {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			saida, err := sshkey.EjecutarComoUsuario(host, usuario, mon.cfg.SSHKeyFile, m.Recolector.Orde)
			if err != nil {
				return // equipo apagado ou sen resposta: sen dato novo
			}
			if valor, ok := extraerValor(saida, m.Recolector.Campo); ok {
				mon.gardar(m.ID, host, valor)
			}
		}(h)
	}
	wg.Wait()
	mon.caducar(m)
}

// extraerValor le a saída da orde. Espérase JSON dunha liña; se `campo` está
// definido devólvese ese, se non o primeiro valor de texto que apareza.
//
// Unha saída que non sexa JSON descártase sen máis: un recolector mal escrito
// deixa o seu equipo sen dato, pero non tira o bucle nin ensucia o mapa.
func extraerValor(saida, campo string) (string, bool) {
	saida = strings.TrimSpace(saida)
	if saida == "" {
		return "", false
	}
	var datos map[string]any
	if err := json.Unmarshal([]byte(saida), &datos); err != nil {
		return "", false
	}
	if campo != "" {
		v, hai := datos[campo]
		if !hai {
			return "", false
		}
		return comoTexto(v), true
	}
	for _, v := range datos {
		return comoTexto(v), true
	}
	return "", false
}

func comoTexto(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func (mon *Monitor) gardar(modulo, equipo, valor string) {
	mon.datosMu.Lock()
	defer mon.datosMu.Unlock()
	if mon.datos[modulo] == nil {
		mon.datos[modulo] = map[string]Dato{}
	}
	mon.datos[modulo][equipo] = Dato{Valor: valor, Visto: time.Now()}
}

// caducar borra os datos que levan sen renovarse máis de tres voltas.
//
// Sen isto, un equipo que se apaga quedaría no mapa co último usuario que tivo,
// para sempre. É a mesma idea ca a caducidade de internal/tao: mellor non
// amosar nada que amosar algo falso.
func (mon *Monitor) caducar(m modulos.Modulo) {
	limite := time.Duration(m.Recolector.Segundos()) * 3 * time.Second
	mon.datosMu.Lock()
	defer mon.datosMu.Unlock()
	for equipo, d := range mon.datos[m.ID] {
		if time.Since(d.Visto) > limite {
			delete(mon.datos[m.ID], equipo)
		}
	}
}

func (mon *Monitor) emitir() {
	if mon.ev.OnPublicacions == nil {
		return
	}
	mon.datosMu.Lock()
	pub := make(map[string]map[string]string, len(mon.datos))
	for modulo, equipos := range mon.datos {
		m := make(map[string]string, len(equipos))
		for equipo, d := range equipos {
			m[equipo] = d.Valor
		}
		pub[modulo] = m
	}
	mon.datosMu.Unlock()
	mon.ev.OnPublicacions(pub)
}

// Esquecer borra os datos dun módulo (ao desactivalo).
func (mon *Monitor) Esquecer(moduloID string) {
	mon.datosMu.Lock()
	delete(mon.datos, moduloID)
	mon.datosMu.Unlock()
	mon.emitir()
}

// Publicacions devolve a foto actual, para a carga inicial do mapa.
func (mon *Monitor) Publicacions() map[string]map[string]string {
	mon.datosMu.Lock()
	defer mon.datosMu.Unlock()
	pub := make(map[string]map[string]string, len(mon.datos))
	for modulo, equipos := range mon.datos {
		m := make(map[string]string, len(equipos))
		for equipo, d := range equipos {
			m[equipo] = d.Valor
		}
		pub[modulo] = m
	}
	return pub
}
