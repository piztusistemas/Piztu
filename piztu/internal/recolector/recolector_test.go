package recolector

import (
	"testing"
	"time"

	"piztu/internal/config"
	"piztu/internal/modulos"
)

// TestExtraerValorIgnoraSaidaInvalida é o punto importante deste paquete: a orde
// dun recolector escríbea un terceiro e pode devolver calquera cousa. Todo o que
// non sexa JSON descártase, deixando o equipo sen dato en vez de ensuciar o mapa
// ou tirar o bucle.
func TestExtraerValorIgnoraSaidaInvalida(t *testing.T) {
	casos := []struct{ nome, saida, campo string }{
		{"texto solto", "ana", "usuario"},
		{"json roto", `{"usuario": `, "usuario"},
		{"baleiro", "   ", "usuario"},
		{"erro do shell", "sh: who: command not found", "usuario"},
		{"campo que non está", `{"outro":"x"}`, "usuario"},
	}
	for _, c := range casos {
		if _, ok := extraerValor(c.saida, c.campo); ok {
			t.Errorf("%s: debería descartarse (%q)", c.nome, c.saida)
		}
	}
}

func TestExtraerValor(t *testing.T) {
	if v, ok := extraerValor(`{"usuario":"ana"}`, "usuario"); !ok || v != "ana" {
		t.Errorf("campo explícito: obtívose %q, %v", v, ok)
	}
	// Sen campo declarado colle o primeiro que veña.
	if v, ok := extraerValor(`{"so":"un"}`, ""); !ok || v != "un" {
		t.Errorf("sen campo: obtívose %q, %v", v, ok)
	}
	// Un número tamén vale: convértese a texto para pintalo.
	if v, ok := extraerValor(`{"libre":42}`, "libre"); !ok || v != "42" {
		t.Errorf("número: obtívose %q, %v", v, ok)
	}
}

// TestCaducidade: un equipo que se apaga deixa de responder, e o seu dato ten
// que desaparecer do mapa. Sen isto quedaría pegado o último usuario que tivo.
func TestCaducidade(t *testing.T) {
	mon := Novo(&config.Config{}, func() []modulos.Modulo { return nil }, Eventos{})
	m := modulos.Modulo{ID: "quen", Recolector: &modulos.Recolector{Orde: "who", Cada: 10}}

	mon.gardar("quen", "bac01", "ana")
	if got := mon.Publicacions()["quen"]["bac01"]; got != "ana" {
		t.Fatalf("non se gardou o dato: %q", got)
	}

	// Envellecemos a marca por riba do límite (3 voltas de 10 s).
	mon.datosMu.Lock()
	mon.datos["quen"]["bac01"] = Dato{Valor: "ana", Visto: time.Now().Add(-31 * time.Second)}
	mon.datosMu.Unlock()

	mon.caducar(m)
	if _, hai := mon.Publicacions()["quen"]["bac01"]; hai {
		t.Error("o dato caducado debería desaparecer")
	}
}

// TestCadaMinimo: o recolector conéctase por SSH a toda a aula, así que non se
// lle deixa baixar do mínimo por moito que o pida o manifest.
func TestCadaMinimo(t *testing.T) {
	if got := (modulos.Recolector{Cada: 1}).Segundos(); got != modulos.CadaMinimo {
		t.Errorf("cada=1 deu %d; debería limitarse a %d", got, modulos.CadaMinimo)
	}
	if got := (modulos.Recolector{Cada: 0}).Segundos(); got != modulos.CadaMinimo {
		t.Errorf("sen declarar deu %d", got)
	}
	if got := (modulos.Recolector{Cada: 60}).Segundos(); got != 60 {
		t.Errorf("un valor razoable non debe tocarse: %d", got)
	}
}

// TestEsquecerLimpaOModulo: ao apagar un módulo os seus datos teñen que saír do
// mapa no acto.
func TestEsquecerLimpaOModulo(t *testing.T) {
	mon := Novo(&config.Config{}, func() []modulos.Modulo { return nil }, Eventos{})
	mon.gardar("quen", "bac01", "ana")
	mon.gardar("outro", "bac01", "algo")

	mon.Esquecer("quen")

	if _, hai := mon.Publicacions()["quen"]; hai {
		t.Error("os datos do módulo apagado deberían desaparecer")
	}
	if _, hai := mon.Publicacions()["outro"]; !hai {
		t.Error("non debería afectar aos demais módulos")
	}
}
