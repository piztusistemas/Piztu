package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"piztu/internal/inventory"
	"piztu/internal/modulos"
	"piztu/internal/motor"
)

// ── GET /api/v1/whoami ───────────────────────────────────────────────────────

// RespostaWhoami confirma o token e di que pode facer quen o usa — primeira
// chamada recomendada para calquera módulo ao arrincar (ver
// docs/api-modulos.md §7). Non esixe ningún permiso concreto: un token
// válido abonda para preguntar quen es.
type RespostaWhoami struct {
	ModuloID   string   `json:"modulo_id"`
	Permisos   []string `json:"permisos"`
	VersionAPI string   `json:"api_version"`
}

func (s *Servidor) handleWhoami(w http.ResponseWriter, r *http.Request, ses sesion) {
	permisos := make([]string, 0, len(ses.permisos))
	for p := range ses.permisos {
		permisos = append(permisos, p)
	}
	escribirJSON(w, http.StatusOK, RespostaWhoami{
		ModuloID: ses.moduloID, Permisos: permisos, VersionAPI: VersionAPI,
	})
}

// ── GET /api/v1/motor/estado ─────────────────────────────────────────────────

// RespostaMotorEstado é o motor activo e os equipos dispoñibles — para que un
// módulo saiba que formato xerar (YAML de Ansible ou .sls de Salt).
type RespostaMotorEstado struct {
	Motor   string   `json:"motor"`
	Equipos []string `json:"equipos"`
}

func (s *Servidor) handleMotorEstado(w http.ResponseWriter, r *http.Request, ses sesion) {
	m := s.motor()
	escribirJSON(w, http.StatusOK, RespostaMotorEstado{Motor: m.Nome(), Equipos: m.GetEquipos()})
}

// ── GET /api/v1/equipos ──────────────────────────────────────────────────────

// Equipo é o inventario detallado (nome + MAC), independente do motor —
// para un módulo que só precise a listaxe, sen que o formato de resposta
// cambie se o profesorado troca de motor.
type Equipo struct {
	Nome string `json:"nome"`
	Mac  string `json:"mac"`
}

func (s *Servidor) handleEquipos(w http.ResponseWriter, r *http.Request, ses sesion) {
	vars := inventory.LerHostsConVars(s.cfg.HostsFile)
	var res []Equipo
	for _, h := range s.motor().GetEquipos() {
		res = append(res, Equipo{Nome: h, Mac: inventory.ExtraerMac(vars[h])})
	}
	escribirJSON(w, http.StatusOK, res)
}

// ── POST /api/v1/motor/executar ──────────────────────────────────────────────

// ResultadoHost é o resultado de executar contido nun equipo concreto.
type ResultadoHost struct {
	Estado string `json:"estado"` // "ok" ou "erro"
	Msg    string `json:"msg,omitempty"`
}

// RespostaMotorExecutar é a resposta de executar, por equipo — mesma forma
// que a que xa devolvía /api/xesta/executar (blueprints/xesta.py), para que
// un módulo migrado non teña que cambiar como interpreta o resultado.
type RespostaMotorExecutar struct {
	Motor      string                   `json:"motor"`
	Resultados map[string]ResultadoHost `json:"resultados"`
}

type peticionExecutar struct {
	Contido  string `json:"contido"`
	Destinos any    `json:"destinos"` // "all" (por defecto) ou []string
}

func (s *Servidor) handleMotorExecutar(w http.ResponseWriter, r *http.Request, ses sesion) {
	var peticion peticionExecutar
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&peticion); err != nil {
		escribirErro(w, http.StatusBadRequest, "corpo_invalido", "corpo JSON non válido: "+err.Error())
		return
	}
	if strings.TrimSpace(peticion.Contido) == "" {
		escribirErro(w, http.StatusBadRequest, "contido_baleiro", "contido baleiro")
		return
	}

	m := s.motor()
	equiposValidos := m.GetEquipos()
	hosts := resolverDestinos(peticion.Destinos, equiposValidos)
	if len(hosts) == 0 {
		escribirErro(w, http.StatusBadRequest, "sen_equipos_destino", "sen equipos destino válidos")
		return
	}

	var mu sync.Mutex
	resultados := map[string]ResultadoHost{}
	cb := motor.Callbacks{
		OnOk: func(h, _ string) {
			mu.Lock()
			resultados[h] = ResultadoHost{Estado: "ok"}
			mu.Unlock()
		},
		OnErro: func(h, saida string) {
			mu.Lock()
			resultados[h] = ResultadoHost{Estado: "erro", Msg: truncar(saida, 4000)}
			mu.Unlock()
		},
	}

	if err := m.ExecutarAdhoc(peticion.Contido, hosts, cb); err != nil {
		escribirErro(w, http.StatusBadRequest, "erro_motor", err.Error())
		return
	}

	s.rexistrarAuditoria(ses.moduloID, m.Nome(), hosts, peticion.Contido)
	escribirJSON(w, http.StatusOK, RespostaMotorExecutar{Motor: m.Nome(), Resultados: resultados})
}

// ── GET /api/v1/modulos ──────────────────────────────────────────────────────

// ModuloInfo é o que un módulo pode saber doutro módulo activo: identidade e
// presentación, nada da súa configuración interna nin dos seus datos.
type ModuloInfo struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Icona string `json:"icona,omitempty"`
}

// RespostaModulos é a lista de módulos actualmente activos en ⚙ Aula →
// Módulos — para que un módulo (ex. Tao) poida saber que outros apeiros do
// ecosistema Piztu están dispoñibles agora mesmo, sen ter que adiviñar nin
// asumir que están instalados. Un módulo inactivo non aparece: para quen
// pregunta, "inactivo" e "non instalado" son o mesmo — non hai nada con que
// falar en ningún dos dous casos.
type RespostaModulos struct {
	Modulos []ModuloInfo `json:"modulos"`
}

func (s *Servidor) handleModulos(w http.ResponseWriter, r *http.Request, ses sesion) {
	res := []ModuloInfo{} // nunca null: unha lista baleira serializa coma [], non coma null
	for _, m := range modulos.Todos(s.cfg.ModulosDir) {
		if !modulos.Activo(s.axustes, m) {
			continue
		}
		res = append(res, ModuloInfo{ID: m.ID, Nome: m.Nome, Icona: m.Icona})
	}
	escribirJSON(w, http.StatusOK, RespostaModulos{Modulos: res})
}

// resolverDestinos traduce o campo "destinos" da petición (xson.Unmarshal
// deixouno en any: string "all" ou []any de strings) a hostnames reais,
// filtrando calquera que non exista xa no inventario — evita que un contido
// malicioso ou erróneo tente executar contra equipos descoñecidos (mesma
// regra que xa aplicaba blueprints/xesta.py).
func resolverDestinos(destinos any, validos []string) []string {
	if s, ok := destinos.(string); ok && s == "all" {
		return validos
	}
	lista, ok := destinos.([]any)
	if !ok {
		return nil
	}
	coñecidos := make(map[string]bool, len(validos))
	for _, v := range validos {
		coñecidos[v] = true
	}
	var res []string
	for _, item := range lista {
		if h, ok := item.(string); ok && coñecidos[h] {
			res = append(res, h)
		}
	}
	return res
}

func truncar(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
