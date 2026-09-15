package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed idiomas
var idiomasFS embed.FS

const idiomaFallback = "gl"

// IdiomaInfo: código → nome amosado, exposto ao frontend para construír o
// selector de idioma (equivalente a idiomasDispo na versión Fyne).
type IdiomaInfo struct {
	Code string `json:"code"`
	Nome string `json:"nome"`
}

var idiomasDispo = []IdiomaInfo{
	{"gl", "Galego"},
	{"es", "Castellano"},
	{"en", "English"},
	{"pt", "Português"},
}

// Translator carga e cachea os dicionarios de idioma do instalador. Porte
// directo da versión Fyne (Lang/loadLang), pero engade All() para poder
// expoñer o dicionario enteiro ao frontend (traducir en cliente, coma en
// piztu/internal/i18n) e T(clave, args...) con formato printf para as
// liñas de log xeradas en Go.
type Translator struct {
	mu    sync.Mutex
	code  string
	cache map[string]map[string]string
}

func NewTranslator(code string) *Translator {
	if strings.TrimSpace(code) == "" {
		code = idiomaFallback
	}
	return &Translator{code: code, cache: map[string]map[string]string{}}
}

func (tr *Translator) SetIdioma(code string) {
	if strings.TrimSpace(code) == "" {
		code = idiomaFallback
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
// devolve a propia clave. Con args aplica formato estilo printf (as cadeas
// dos .lang xa usan %s/%d ao estilo Go).
func (tr *Translator) T(clave string, args ...any) string {
	valor, ok := tr.cargar(tr.Idioma())[clave]
	if !ok && tr.Idioma() != idiomaFallback {
		valor, ok = tr.cargar(idiomaFallback)[clave]
	}
	if !ok {
		valor = clave
	}
	if len(args) > 0 {
		valor = fmt.Sprintf(valor, args...)
	}
	return valor
}

// All devolve o dicionario completo (fallback + idioma activo mesturados),
// para que o frontend traduza os textos estáticos (botóns, títulos...).
func (tr *Translator) All() map[string]string {
	res := map[string]string{}
	for k, v := range tr.cargar(idiomaFallback) {
		res[k] = v
	}
	if tr.Idioma() != idiomaFallback {
		for k, v := range tr.cargar(tr.Idioma()) {
			res[k] = v
		}
	}
	return res
}

func (tr *Translator) cargar(code string) map[string]string {
	tr.mu.Lock()
	if m, ok := tr.cache[code]; ok {
		tr.mu.Unlock()
		return m
	}
	tr.mu.Unlock()

	data, err := lerIdioma(code)
	m := map[string]string{}
	if err == nil {
		m = parseLang(data)
	}
	tr.mu.Lock()
	tr.cache[code] = m
	tr.mu.Unlock()
	return m
}

// lerIdioma devolve o contido do ficheiro do idioma: ficheiro externo (ao
// carón do executable, editable polo usuario) se existe, senón o embebido.
func lerIdioma(code string) ([]byte, error) {
	if exe, err := os.Executable(); err == nil {
		ext := filepath.Join(filepath.Dir(exe), "idiomas", code+".lang")
		if data, err := os.ReadFile(ext); err == nil {
			return data, nil
		}
	}
	return idiomasFS.ReadFile("idiomas/" + code + ".lang")
}

func parseLang(data []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		clave := strings.TrimSpace(line[:eq])
		valor := strings.TrimSpace(line[eq+1:])
		valor = strings.ReplaceAll(valor, "\\n", "\n")
		m[clave] = valor
	}
	return m
}
