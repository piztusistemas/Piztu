package main

import (
	"errors"
	"sync"
	"testing"

	"piztu/internal/motor"
	"piztu/internal/tao"
)

// ── AlumnadoPorEquipo (a través de filtrarAlumnadoVivo, sen tao-helper) ──────────

func TestFiltrarAlumnadoVivoSoDeixaSesionsVivasConNome(t *testing.T) {
	entrada := map[string]tao.Sesion{
		"tux01": {Nome: "Ana López Vidal", Vivo: true},
		"tux02": {Nome: "Brais Cid", Vivo: false},      // sesión caducada/desconectada
		"tux03": {Nome: "", Vivo: true},                // sen nome (non debería pasar en produción)
		"tux04": {Nome: "Carla Mera", Conectado: true}, // Vivo=false por defecto: non entra
	}
	got := filtrarAlumnadoVivo(entrada)
	if len(got) != 1 {
		t.Fatalf("esperaba 1 equipo, obtido %d: %+v", len(got), got)
	}
	if got["tux01"] != "Ana López Vidal" {
		t.Fatalf("esperaba o nome completo de tux01, obtido %+v", got)
	}
}

func TestFiltrarAlumnadoVivoBaleiro(t *testing.T) {
	if got := filtrarAlumnadoVivo(nil); len(got) != 0 {
		t.Fatalf("esperaba mapa baleiro, obtido %+v", got)
	}
}

// ── EnviarIndividualizado / executarEnvioIndividualizado ─────────────────────────

// motorFalso é un motor.Motor de proba: EnviarFicheiros é o único método que
// executarEnvioIndividualizado chama, así que é o único con comportamento real —
// grava que ficheiros recibiu cada equipo e simula fallo nos equipos de `falla`.
type motorFalso struct {
	mu          sync.Mutex
	recibidos   map[string][]string // equipo -> ficheiros que recibiu
	falla       map[string]bool
	erroDirecto error // se non nil, EnviarFicheiros devolve isto sen chamar cb
}

func novoMotorFalso() *motorFalso {
	return &motorFalso{recibidos: map[string][]string{}, falla: map[string]bool{}}
}

func (m *motorFalso) Nome() string                                                     { return "falso" }
func (m *motorFalso) GetEquipos() []string                                             { return nil }
func (m *motorFalso) ExecutarAccion(string, []string, motor.Callbacks) error           { return nil }
func (m *motorFalso) ExecutarBloqueo([]string, map[string]bool, motor.Callbacks) error { return nil }
func (m *motorFalso) LimparPracticas([]string, motor.Callbacks) error                  { return nil }
func (m *motorFalso) RecollerPracticas([]string, motor.Callbacks) error                { return nil }
func (m *motorFalso) ExecutarAdhoc(string, []string, motor.Callbacks) error            { return nil }

func (m *motorFalso) EnviarFicheiros(hosts []string, ficheiros []string, cb motor.Callbacks) error {
	if m.erroDirecto != nil {
		return m.erroDirecto
	}
	h := hosts[0] // executarEnvioIndividualizado chama sempre con exactamente 1 host
	m.mu.Lock()
	m.recibidos[h] = ficheiros
	m.mu.Unlock()
	cb.OnInicio(h)
	if m.falla[h] {
		cb.OnErro(h, "fallo simulado en "+h)
	} else {
		cb.OnOk(h, "")
	}
	return nil
}

// eventosCapturados é un emisor de proba: garda cada evento nunha slice protexida
// por mutex (executarEnvioIndividualizado emite desde varias goroutines).
type eventosCapturados struct {
	mu   sync.Mutex
	todo []map[string]any
}

func (e *eventosCapturados) emitir(nome string, dados map[string]any) {
	dados["_evento"] = nome
	e.mu.Lock()
	e.todo = append(e.todo, dados)
	e.mu.Unlock()
}

func (e *eventosCapturados) completo() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.todo {
		if ev["_evento"] == "envio_individualizado_completo" {
			return ev
		}
	}
	return nil
}

func TestExecutarEnvioIndividualizadoCadaEquipoRecibeSoOSeuFicheiro(t *testing.T) {
	m := novoMotorFalso()
	porEquipo := map[string]string{
		"tux01": "/tmp/exame-ana.pdf",
		"tux02": "/tmp/exame-brais.pdf",
	}
	ev := &eventosCapturados{}
	executarEnvioIndividualizado("op1", m, porEquipo, ev.emitir)

	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.recibidos["tux01"]; len(got) != 1 || got[0] != "/tmp/exame-ana.pdf" {
		t.Fatalf("tux01 recibiu %v, esperaba só o seu propio ficheiro", got)
	}
	if got := m.recibidos["tux02"]; len(got) != 1 || got[0] != "/tmp/exame-brais.pdf" {
		t.Fatalf("tux02 recibiu %v, esperaba só o seu propio ficheiro", got)
	}
}

func TestExecutarEnvioIndividualizadoContaOkEErros(t *testing.T) {
	m := novoMotorFalso()
	m.falla["tux02"] = true
	porEquipo := map[string]string{
		"tux01": "/tmp/a.pdf",
		"tux02": "/tmp/b.pdf",
	}
	ev := &eventosCapturados{}
	executarEnvioIndividualizado("op1", m, porEquipo, ev.emitir)

	completo := ev.completo()
	if completo == nil {
		t.Fatal("esperaba un evento envio_individualizado_completo")
	}
	if completo["total"] != 2 || completo["ok"] != 1 || completo["erros"] != 1 {
		t.Fatalf("conteo incorrecto: %+v", completo)
	}
}

func TestExecutarEnvioIndividualizadoContaErroDeArranqueQueNonChamaCallbacks(t *testing.T) {
	m := novoMotorFalso()
	m.erroDirecto = errors.New("ansible non atopado")
	ev := &eventosCapturados{}
	executarEnvioIndividualizado("op1", m, map[string]string{"tux01": "/tmp/a.pdf"}, ev.emitir)

	completo := ev.completo()
	if completo == nil {
		t.Fatal("esperaba un evento envio_individualizado_completo")
	}
	if completo["ok"] != 0 || completo["erros"] != 1 {
		t.Fatalf("un erro de arranque (sen callback) debe contar coma erro: %+v", completo)
	}
}

func TestEnviarIndividualizadoBaleiroNonExecutaNadaNinPeta(t *testing.T) {
	a := &App{}
	opID := a.EnviarIndividualizado(nil)
	if opID == "" {
		t.Fatal("esperaba un op_id mesmo sen destinos")
	}
}
