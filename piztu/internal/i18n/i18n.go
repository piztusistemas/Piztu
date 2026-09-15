// Package i18n é o porte de core/idiomas.py: le idiomas/<code>.lang
// (formato `clave = valor`, admite \n e comentarios #) e traduce cadeas con
// fallback ao galego.
package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const fallback = "gl"

// Translator carga e cachea os dicionarios de idioma.
type Translator struct {
	dirs   []string
	code   string
	mu     sync.Mutex
	cache  map[string]map[string]string
}

// New crea un Translator que busca .lang en `dirs`, co idioma activo `code`.
func New(dirs []string, code string) *Translator {
	if code = strings.TrimSpace(code); code == "" {
		code = fallback
	}
	return &Translator{dirs: dirs, code: code, cache: map[string]map[string]string{}}
}

// SetIdioma cambia o idioma activo en tempo de execución.
func (tr *Translator) SetIdioma(code string) {
	if code = strings.TrimSpace(code); code == "" {
		code = fallback
	}
	tr.mu.Lock()
	tr.code = code
	tr.mu.Unlock()
}

func (tr *Translator) Idioma() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.code
}

// T traduce `clave` co idioma activo; se falta, proba galego e por último
// devolve a propia clave. Con args aplica formato estilo printf.
func (tr *Translator) T(clave string, args ...any) string {
	code := tr.Idioma()
	valor, ok := tr.cargar(code)[clave]
	if !ok && code != fallback {
		valor, ok = tr.cargar(fallback)[clave]
	}
	if !ok {
		valor = clave
	}
	if len(args) > 0 {
		valor = fmt.Sprintf(printfDesdePy(valor), args...)
	}
	return valor
}

// All devolve o dicionario completo do idioma activo (mesturado co fallback),
// para expoñelo ao frontend e traducir en cliente.
func (tr *Translator) All() map[string]string {
	res := map[string]string{}
	for k, v := range tr.cargar(fallback) {
		res[k] = v
	}
	if tr.Idioma() != fallback {
		for k, v := range tr.cargar(tr.Idioma()) {
			res[k] = v
		}
	}
	return res
}

func (tr *Translator) cargar(code string) map[string]string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if m, ok := tr.cache[code]; ok {
		return m
	}
	m := parse(tr.ruta(code))
	tr.cache[code] = m
	return m
}

func (tr *Translator) ruta(code string) string {
	for _, d := range tr.dirs {
		f := filepath.Join(d, code+".lang")
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

func parse(path string) map[string]string {
	m := map[string]string{}
	if path == "" {
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || !strings.Contains(line, "=") {
			continue
		}
		clave, valor, _ := strings.Cut(line, "=")
		m[strings.TrimSpace(clave)] = strings.ReplaceAll(strings.TrimSpace(valor), "\\n", "\n")
	}
	return m
}

// printfDesdePy converte %d de Python a %v (Go é estrito cos tipos; %v acepta
// calquera). Mantén %s. Abonda para as cadeas de Piztu.
func printfDesdePy(s string) string {
	return strings.ReplaceAll(s, "%d", "%v")
}
