package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Catálogo de capacidades — o único sitio da implementación que decide que
// permiso protexe cada endpoint (ver mux() en api.go). Documentadas en
// docs/api-modulos.md §5; un módulo que declare un nome distinto destes en
// modulo.json non concorda con ningún, e queda sen acceso a nada (mesmo
// criterio "descártase en silencio" ca o resto do sistema de módulos).
const (
	PermisoMotorLer      = "motor.ler"
	PermisoMotorExecutar = "motor.executar"
	PermisoModulosLer    = "modulos.ler"
)

// sesion é o que sabe Piztu dun token emitido: de que módulo é e que pode
// facer. Vive só en memoria — pechar Piztu esquece todas as sesións, e cada
// módulo pide unha nova ao arrincar (ver EmitirToken).
type sesion struct {
	moduloID string
	permisos map[string]bool
	expira   time.Time
}

func (s sesion) ten(permiso string) bool {
	return permiso == "" || s.permisos[permiso]
}

// rexistroSesions garda os tokens vivos, protexido por mutex (chámase desde
// goroutines distintas: unha por petición HTTP).
type rexistroSesions struct {
	mu  sync.Mutex
	por map[string]sesion
}

func novoRexistroSesions() *rexistroSesions {
	return &rexistroSesions{por: map[string]sesion{}}
}

func (r *rexistroSesions) emitir(moduloID string, permisos []string, ttl time.Duration) string {
	pmap := make(map[string]bool, len(permisos))
	for _, p := range permisos {
		pmap[p] = true
	}
	token := novoToken()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.por[token] = sesion{moduloID: moduloID, permisos: pmap, expira: time.Now().Add(ttl)}
	return token
}

func (r *rexistroSesions) validar(token string) (sesion, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ses, ok := r.por[token]
	if !ok {
		return sesion{}, false
	}
	if time.Now().After(ses.expira) {
		delete(r.por, token)
		return sesion{}, false
	}
	return ses, true
}

func (r *rexistroSesions) revogarModulo(moduloID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for token, ses := range r.por {
		if ses.moduloID == moduloID {
			delete(r.por, token)
		}
	}
}

func novoToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "pz_" + hex.EncodeToString(b)
}

// handlerAutenticado é coma http.HandlerFunc pero recibe xa a sesión validada
// — os handlers non tocan cabeceiras nin tokens, só len ses.moduloID cando o
// precisan (ex. para a auditoría de motor.executar).
type handlerAutenticado func(w http.ResponseWriter, r *http.Request, ses sesion)

// autenticar esixe un token Bearer válido e, se `permiso` non é "", que a
// sesión o teña concedido. 401 sen token válido, 403 sen ese permiso concreto
// — nunca 404: que a ruta exista non é segredo (ver docs/api-modulos.md §6).
func (s *Servidor) autenticar(permiso string, next handlerAutenticado) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extraerBearer(r.Header.Get("Authorization"))
		if token == "" {
			escribirErro(w, http.StatusUnauthorized, "sen_token", "falta a cabeceira Authorization: Bearer <token>")
			return
		}
		ses, ok := s.sesions.validar(token)
		if !ok {
			escribirErro(w, http.StatusUnauthorized, "token_invalido", "token descoñecido, caducado ou revogado")
			return
		}
		if !ses.ten(permiso) {
			escribirErro(w, http.StatusForbidden, "sen_permiso", "o módulo "+ses.moduloID+" non ten o permiso "+permiso)
			return
		}
		next(w, r, ses)
	}
}

func extraerBearer(cabeceira string) string {
	const prefixo = "Bearer "
	if !strings.HasPrefix(cabeceira, prefixo) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(cabeceira, prefixo))
}

// recuperar captura un panic en calquera handler e devolve JSON (nunca a
// páxina HTML por defecto de Go) — un módulo que fala JSON non debe ter que
// saber parsear HTML para saber que algo foi mal.
func (s *Servidor) recuperar(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				escribirErro(w, http.StatusInternalServerError, "erro_interno", "erro interno inesperado")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
