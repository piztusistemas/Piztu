package modulos

import (
	"os"
	"path/filepath"
	"testing"
)

func escribirPanel(t *testing.T, dir, contido string) Modulo {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "panel.yaml"), []byte(contido), 0o644); err != nil {
		t.Fatal(err)
	}
	return Modulo{ID: "proba", Nome: "Proba", Dir: dir, Panel: "panel.yaml"}
}

// TestPanelIgnoraCamposDescoñecidos é a garantía do enfoque declarativo: un
// panel escrito contra unha versión futura de piztu segue sendo utilizable, e un
// campo mal escrito non tira o resto.
func TestPanelIgnoraCamposDescoñecidos(t *testing.T) {
	m := escribirPanel(t, t.TempDir(), `
titulo: Cisterna
campos:
  - {tipo: texto, id: litros, etiqueta: Litros}
  - {tipo: holograma3d, id: futuro, etiqueta: Do futuro}
  - {tipo: numero, id: presion, etiqueta: Presión}
`)
	p := LerPanel(m)
	if p == nil {
		t.Fatal("o panel debería lerse")
	}
	if len(p.Campos) != 2 {
		t.Fatalf("agardábanse 2 campos válidos, hai %d: %+v", len(p.Campos), p.Campos)
	}
	for _, c := range p.Campos {
		if c.Tipo == "holograma3d" {
			t.Error("colouse un tipo descoñecido")
		}
	}
}

func TestPanelRexeitaCamposInvalidos(t *testing.T) {
	m := escribirPanel(t, t.TempDir(), `
campos:
  - {tipo: texto, etiqueta: Sen id}
  - {tipo: texto, id: repe, etiqueta: Primeira}
  - {tipo: texto, id: repe, etiqueta: Repetida}
  - {tipo: boton, id: b1, etiqueta: Sen accion}
  - {tipo: boton, id: b2, etiqueta: Con accion, accion: baleirar}
`)
	p := LerPanel(m)
	if p == nil {
		t.Fatal("o panel debería lerse")
	}
	if len(p.Campos) != 2 {
		t.Fatalf("agardábanse 2 campos, hai %d: %+v", len(p.Campos), p.Campos)
	}
	if p.Campos[0].ID != "repe" || p.Campos[0].Etiqueta != "Primeira" {
		t.Errorf("debería quedar a primeira aparición: %+v", p.Campos[0])
	}
	if p.Campos[1].ID != "b2" {
		t.Errorf("o botón sen acción debería descartarse: %+v", p.Campos[1])
	}
}

func TestPanelYamlRotoNonTiraNada(t *testing.T) {
	if p := LerPanel(escribirPanel(t, t.TempDir(), "isto: [non é\n  yaml válido")); p != nil {
		t.Error("un YAML roto non debería dar panel")
	}
}

func TestPanelSenCamposUtilesNonSeOfrece(t *testing.T) {
	if p := LerPanel(escribirPanel(t, t.TempDir(), "titulo: Baleiro\ncampos: []")); p != nil {
		t.Error("un panel sen campos non debería ofrecerse")
	}
}

func TestPanelUsaONomeDoModuloSenTitulo(t *testing.T) {
	m := escribirPanel(t, t.TempDir(), "campos:\n  - {tipo: texto, id: x, etiqueta: X}")
	p := LerPanel(m)
	if p == nil || p.Titulo != "Proba" {
		t.Errorf("sen título debería usarse o nome do módulo: %+v", p)
	}
}

func TestClaveCampoNonColideEntreModulos(t *testing.T) {
	if ClaveCampo("a", "x") == ClaveCampo("b", "x") {
		t.Error("dous módulos co mesmo campo pisaríanse os valores")
	}
}
