package historial

import (
	"os"
	"strings"
	"testing"
	"time"

	"piztu/internal/config"
	"piztu/internal/tao"
)

func sesionsEn(usuario string, vivo bool) map[string]tao.Sesion {
	return map[string]tao.Sesion{
		"tux01": {Usuario: usuario, Nome: "Alumno " + usuario, Grupo: "1A", Conectado: vivo, Vivo: vivo},
	}
}

// TestFluxoConfirmacion replica o escenario probado á man na versión Python
// (core/historial_tao.py): confirma tras o tempo estable, pecha ao cambiar
// de alumno, e NON rexistra asignacións que nunca chegaron a confirmarse.
func TestFluxoConfirmacion(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		BaseDir: dir,
		Filebrowser: config.Filebrowser{
			ConfirmarSeg: 5,
			HistorialDir: "dixitalizacion/historial",
		},
	}
	r := Novo(cfg)

	t0 := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	defer func() { now = time.Now }()

	now = func() time.Time { return t0 }
	r.Procesar(sesionsEn("alumno1", true))
	if r.estado["tux01"].confirmado {
		t.Fatal("t=0: non debería estar confirmado aínda")
	}

	now = func() time.Time { return t0.Add(2 * time.Second) }
	r.Procesar(sesionsEn("alumno1", true))
	if r.estado["tux01"].confirmado {
		t.Fatal("t=2: non debería estar confirmado aínda (< 5s)")
	}

	now = func() time.Time { return t0.Add(6 * time.Second) }
	r.Procesar(sesionsEn("alumno1", true))
	if !r.estado["tux01"].confirmado {
		t.Fatal("t=6: debería estar confirmado (>= 5s) e ter xerado 'inicio'")
	}

	now = func() time.Time { return t0.Add(8 * time.Second) }
	r.Procesar(sesionsEn("alumno2", true))
	if r.estado["tux01"].usuario != "alumno2" || r.estado["tux01"].confirmado {
		t.Fatal("t=8: debería rastrexar a alumno2 sen confirmar (e pechar a alumno1)")
	}

	now = func() time.Time { return t0.Add(9 * time.Second) }
	r.Procesar(sesionsEn("", false))
	if _, existe := r.estado["tux01"]; existe {
		t.Fatal("t=9: tux01 debería deixar de rastrexarse")
	}

	ficheiro := dir + "/historial/" + t0.Format("2006-01-02") + ".jsonl"
	contido, err := os.ReadFile(ficheiro)
	if err != nil {
		t.Fatalf("non se atopa o ficheiro de historial: %v", err)
	}
	texto := string(contido)
	liñas := strings.Split(strings.TrimSpace(texto), "\n")
	if len(liñas) != 2 {
		t.Fatalf("esperaba exactamente 2 eventos (inicio+fin de alumno1), atopei %d:\n%s", len(liñas), texto)
	}
	if !strings.Contains(liñas[0], `"tipo":"inicio"`) || !strings.Contains(liñas[0], `"usuario":"alumno1"`) {
		t.Fatalf("primeira liña non é o 'inicio' de alumno1 esperado: %s", liñas[0])
	}
	if !strings.Contains(liñas[1], `"tipo":"fin"`) || !strings.Contains(liñas[1], `"usuario":"alumno1"`) {
		t.Fatalf("segunda liña non é o 'fin' de alumno1 esperado: %s", liñas[1])
	}
}
