package main

import (
	"sync"

	"github.com/google/uuid"

	"piztu/internal/motor"
	"piztu/internal/tao"
)

// Envío individualizado: un ficheiro distinto por equipo (ex. un exame en PDF que
// Yang xerou co nome de cada alumno), a diferenza de EnviarPracticas (app.go), que
// manda os MESMOS ficheiros a todos os destinos.
//
// Deliberadamente NON pasa pola API de módulos (internal/api, docs/api-modulos.md):
// esa API é a porta de entrada para módulos externos e illa estruturalmente
// calquera dato de Tao (sesións, equipo↔alumno) — non existe nin debe existir un
// permiso que o exporía (ver CLAUDE.md, "Xesta/Tao isolation"). Isto é código do
// propio núcleo de Piztu, que xa consome as sesións de Tao lexitimamente para o
// mapa (iniciarTao/GetEstadoTao en app.go) — a mesma fonte, reutilizada aquí.

// AlumnadoPorEquipo devolve, para cada equipo cunha sesión de Tao activa agora
// mesmo, o nome e apelidos completos do alumno que o está a usar. Un equipo sen
// sesión viva (ninguén loggeado, ou sesión caducada — ver tao.Sesion.Vivo) non
// aparece: non hai a quen individualizar o contido dese equipo.
func (a *App) AlumnadoPorEquipo() map[string]string {
	return filtrarAlumnadoVivo(tao.Listar(a.cfg))
}

// filtrarAlumnadoVivo illa a lóxica de filtrado de AlumnadoPorEquipo para poder
// testala sen depender do axudante tao-helper (ver individualizado_test.go).
func filtrarAlumnadoVivo(sesions map[string]tao.Sesion) map[string]string {
	res := map[string]string{}
	for equipo, sesion := range sesions {
		if !sesion.Vivo || sesion.Nome == "" {
			continue
		}
		res[equipo] = sesion.Nome
	}
	return res
}

// maxEnviosSimultaneos limita cantos equipos se serven á vez: a diferenza de
// EnviarPracticas (unha soa execución do motor para todos os destinos á vez), aquí
// cada equipo precisa a súa propia execución independente (leva un ficheiro
// distinto) — sen tope, N equipos lanzarían N procesos ansible-playbook/salt-ssh
// simultáneos.
const maxEnviosSimultaneos = 6

// EnviarIndividualizado envía, a cada equipo, o ficheiro local que lle corresponde
// (porEquipo: nome de equipo → ruta dun ficheiro xa existente nesta máquina). A
// correspondencia equipo→ficheiro xa a decidiu quen chama (tipicamente cruzando
// AlumnadoPorEquipo() co seu propio mapa alumno→ficheiro antes de chamar isto).
//
// Devolve op_id de seguido; progreso e resultado final chegan por evento — mesmo
// contrato de eventos que EnviarPracticas, en versión "individualizado":
//   - "progreso_envio_individualizado": {op_id, host, estado: enviando|ok|erro, actual, total, msg}
//   - "envio_individualizado_completo": {op_id, total, ok, erros}
func (a *App) EnviarIndividualizado(porEquipo map[string]string) string {
	opID := uuid.NewString()
	if len(porEquipo) == 0 {
		return opID
	}
	m := motor.Get(a.cfg, "")
	emitir := func(evento string, dados map[string]any) { emitirEvento(evento, dados) }
	go executarEnvioIndividualizado(opID, m, porEquipo, emitir)
	return opID
}

// executarEnvioIndividualizado fai o traballo, de forma BLOQUEANTE (o wg.Wait() final
// non remata ata que todos os equipos acabaron) — á parte de EnviarIndividualizado
// para poder chamala directamente nun test, cun motor.Motor falso e un emitir que
// garda os eventos nunha slice, sen executar ansible/salt de verdade nin depender do
// contexto de Wails (ver individualizado_test.go).
func executarEnvioIndividualizado(opID string, m motor.Motor, porEquipo map[string]string, emitir func(string, map[string]any)) {
	total := len(porEquipo)
	var mu sync.Mutex
	okN, errN := 0, 0
	rexistrar := func(host, estado, msg string) {
		mu.Lock()
		if estado == "ok" {
			okN++
		} else if estado == "erro" {
			errN++
		}
		actual := okN + errN
		mu.Unlock()
		emitir("progreso_envio_individualizado", map[string]any{
			"op_id": opID, "host": host, "estado": estado, "actual": actual, "total": total, "msg": msg,
		})
	}

	sem := make(chan struct{}, maxEnviosSimultaneos)
	var wg sync.WaitGroup
	for equipo, ruta := range porEquipo {
		wg.Add(1)
		go func(equipo, ruta string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cb := motor.Callbacks{
				OnInicio: func(h string) {
					emitir("progreso_envio_individualizado", map[string]any{
						"op_id": opID, "host": h, "estado": "enviando", "total": total,
					})
				},
				OnOk:   func(h, _ string) { rexistrar(h, "ok", "") },
				OnErro: func(h, out string) { rexistrar(h, "erro", extraerErro(out)) },
			}
			// Un erro devolto aquí (a diferenza dun fallo reportado por cb.OnErro)
			// significa que a execución nin sequera arrincou neste equipo (ex.
			// ansible non atopado) — cb nunca se chamaría, así que hai que contalo
			// á man para que non desapareza do reconto final.
			if err := m.EnviarFicheiros([]string{equipo}, []string{ruta}, cb); err != nil {
				rexistrar(equipo, "erro", err.Error())
			}
		}(equipo, ruta)
	}
	wg.Wait()

	emitir("envio_individualizado_completo", map[string]any{
		"op_id": opID, "total": total, "ok": okN, "erros": errN,
	})
}
