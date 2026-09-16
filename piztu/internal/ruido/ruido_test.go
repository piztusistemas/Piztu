package ruido

import (
	"os"
	"path/filepath"
	"testing"

	"piztu/internal/config"
	"piztu/internal/motor"
)

func TestRmsAPorcentaxe(t *testing.T) {
	for _, rms := range []float64{0, -5, 1, 100, 32768, 1e9} {
		if got := rmsAPorcentaxe(rms); got < 0 || got > 100 {
			t.Errorf("rmsAPorcentaxe(%v) = %d fóra de [0,100]", rms, got)
		}
	}
	if got := rmsAPorcentaxe(32768); got != 100 {
		t.Errorf("rmsAPorcentaxe(32768) = %d, quería 100 (nivel máximo)", got)
	}
	if got := rmsAPorcentaxe(0); got != 0 {
		t.Errorf("rmsAPorcentaxe(0) = %d, quería 0 (silencio)", got)
	}
}

func TestAudioQueueFIFOEMaxlen(t *testing.T) {
	q := &audioQueue{}
	for i := 0; i < audioQueueMax+5; i++ {
		q.push([]byte{byte(i)})
	}
	if len(q.bufs) != audioQueueMax {
		t.Fatalf("len(q.bufs) = %d, quería %d (maxlen)", len(q.bufs), audioQueueMax)
	}
	// Os primeiros 5 empurrados deberían terse descartado; o primeiro que queda é o 5.
	b, ok := q.pop()
	if !ok || b[0] != 5 {
		t.Fatalf("primeiro elemento tras exceder maxlen = %v, quería [5]", b)
	}
}

func TestAudioQueuePopBaleiro(t *testing.T) {
	q := &audioQueue{}
	if _, ok := q.pop(); ok {
		t.Fatal("pop() nunha cola baleira debería devolver ok=false")
	}
}

// ── Armado: o monitor non toca a aula ata que o profesorado o arma ──────────

// motorFalso conta as accións que se lle piden, para comprobar cales chegan (ou
// non) aos equipos. Só implementa o que usa o monitor; o resto é relleno da
// interface motor.Motor.
type motorFalso struct {
	accions []string
}

func (m *motorFalso) Nome() string        { return "falso" }
func (m *motorFalso) GetEquipos() []string { return nil }
func (m *motorFalso) ExecutarAccion(nomeLogico string, hosts []string, cb motor.Callbacks) error {
	m.accions = append(m.accions, nomeLogico)
	return nil
}
func (m *motorFalso) ExecutarBloqueo([]string, map[string]bool, motor.Callbacks) error { return nil }
func (m *motorFalso) EnviarFicheiros([]string, []string, motor.Callbacks) error        { return nil }
func (m *motorFalso) LimparPracticas([]string, motor.Callbacks) error                  { return nil }
func (m *motorFalso) RecollerPracticas([]string, motor.Callbacks) error                { return nil }
func (m *motorFalso) ExecutarAdhoc(string, []string, motor.Callbacks) error            { return nil }

// monitorDeProba devolve un monitor cun inventario de dous equipos e un motor
// falso, sen arrincar a captura de audio.
func monitorDeProba(t *testing.T) (*Monitor, *motorFalso) {
	t.Helper()
	hosts := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hosts, []byte("[aula]\ntux01\ntux02\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &motorFalso{}
	return &Monitor{cfg: &config.Config{HostsFile: hosts}, m: m, activo: true}, m
}

func TestSenArmarNonSeEnviaNadaAosEquipos(t *testing.T) {
	mon, m := monitorDeProba(t) // modo = SoEscoita (valor cero)

	if mon.Armado() {
		t.Fatal("un monitor recén creado non debería estar armado")
	}
	mon.dispararSync("liberar")
	mon.dispararAsync("aviso_ruido")
	if len(m.accions) != 0 {
		t.Fatalf("sen armar non se debería enviar nada á aula; enviouse: %v", m.accions)
	}
	if mon.desbloquearHostIndividual("tux01") {
		t.Fatal("sen armar non se debería desbloquear ningún equipo")
	}
}

func TestArmadoEnviaAsAccions(t *testing.T) {
	mon, m := monitorDeProba(t)
	mon.modo = ConAccions

	if !mon.Armado() {
		t.Fatal("con modo ConAccions e activo, Armado() debería ser certo")
	}
	mon.dispararSync("liberar")
	if len(m.accions) != 1 || m.accions[0] != "liberar" {
		t.Fatalf("armado debería executar a acción; accións = %v", m.accions)
	}
}

func TestDetenerDesarma(t *testing.T) {
	mon, _ := monitorDeProba(t)
	mon.modo = ConAccions
	mon.cancelar = func() {}

	mon.Detener()
	if mon.Armado() {
		t.Fatal("Detener debería desarmar o monitor")
	}
	// E un arranque posterior sen pedir accións non o volve armar só.
	mon.activo = true
	if mon.Armado() {
		t.Fatal("volver escoitar non debería rearmar o monitor")
	}
}
