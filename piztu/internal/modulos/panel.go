package modulos

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Un módulo pode traer un panel de configuración propio, descrito nun YAML.
// Piztu debúxao co seu estilo: NON se executa código do módulo na interface.
//
// Descartouse deliberadamente cargar HTML/JS do módulo nun marco illado. Con
// tipos pechados o peor que pode facer un apeiro mal escrito é que un campo seu
// non se pinte; con código propio podería romper piztu ou ler o que non debe.
// A cambio, todos os paneis teñen o mesmo aspecto ca o resto da aplicación.

// Tipos de campo admitidos. Calquera outro ignórase ao ler o panel.
const (
	CampoTexto         = "texto"
	CampoNumero        = "numero"
	CampoLista         = "lista"
	CampoInterruptor   = "interruptor"
	CampoBoton         = "boton"
	CampoTextoEstatico = "info"
)

var tiposValidos = map[string]bool{
	CampoTexto: true, CampoNumero: true, CampoLista: true,
	CampoInterruptor: true, CampoBoton: true, CampoTextoEstatico: true,
}

// Campo é un control do panel.
type Campo struct {
	Tipo     string   `yaml:"tipo"     json:"tipo"`
	ID       string   `yaml:"id"       json:"id"`
	Etiqueta string   `yaml:"etiqueta" json:"etiqueta"`
	Axuda    string   `yaml:"axuda"    json:"axuda"`
	Defecto  string   `yaml:"defecto"  json:"defecto"`
	Opcions  []string `yaml:"opcions"  json:"opcions"`
	// Fonte enche unha lista con datos de piztu en vez de con `opcions`.
	// Único valor admitido de momento: "equipos".
	Fonte string `yaml:"fonte" json:"fonte"`
	// Accion é o id da acción que dispara un botón (das que declara o módulo).
	Accion string `yaml:"accion" json:"accion"`
}

// Panel é a descrición completa da interface dun módulo.
type Panel struct {
	Titulo string  `yaml:"titulo" json:"titulo"`
	Campos []Campo `yaml:"campos" json:"campos"`
}

// LerPanel carga o panel dun módulo. Devolve nil se non ten ningún.
//
// Tolerante coma o resto: un YAML ilexible non dá panel, e un campo cun tipo
// descoñecido ou sen identificar sáltase sen tirar o resto.
func LerPanel(m Modulo) *Panel {
	if m.Panel == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(m.Dir, m.Panel))
	if err != nil {
		return nil
	}
	var p Panel
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil
	}

	limpos := make([]Campo, 0, len(p.Campos))
	vistos := map[string]bool{}
	for _, c := range p.Campos {
		c.Tipo = strings.ToLower(strings.TrimSpace(c.Tipo))
		c.ID = strings.TrimSpace(c.ID)
		if !tiposValidos[c.Tipo] {
			continue
		}
		// Todo agás o texto estático precisa un id único: é a clave coa que se
		// garda o valor ou se identifica o botón.
		if c.Tipo != CampoTextoEstatico {
			if c.ID == "" || vistos[c.ID] {
				continue
			}
			vistos[c.ID] = true
		}
		if c.Tipo == CampoBoton && c.Accion == "" {
			continue // un botón que non dispara nada non serve
		}
		if c.Etiqueta == "" {
			c.Etiqueta = c.ID
		}
		limpos = append(limpos, c)
	}
	p.Campos = limpos

	if p.Titulo == "" {
		p.Titulo = m.Nome
	}
	if len(p.Campos) == 0 {
		return nil
	}
	return &p
}

// ClaveCampo é a clave coa que se garda o valor dun campo nos axustes.
func ClaveCampo(moduloID, campoID string) string {
	return "modulo_" + moduloID + "_" + campoID
}
