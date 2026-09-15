// Package api é a porta de entrada oficial para módulos con proceso propio
// (hoxe Xesta; calquera módulo futuro con GUI propia que precise falar con
// Piztu en marcha, non só ler a foto de arranque de ContextoModulo). Contrato
// completo documentado en docs/api-modulos.md — este paquete é a súa
// implementación de referencia, en Go nativo dentro de piztu (substitúe o
// que antes facía blueprints/xesta.py a través de app.py + garantirFlask).
//
// Transporte: HTTP sobre UN só socket Unix compartido por todos os módulos
// (tmp_dir/piztu-api.sock), non un por módulo. A identidade de quen chama non
// vén do socket (todos comparten o mesmo) senón do token — ver auth.go.
package api

import (
	"net"
	"net/http"
	"os"
	"time"

	"piztu/internal/config"
	"piztu/internal/modulos"
	"piztu/internal/motor"
)

// NomeSocket é o ficheiro do socket dentro de cfg.TmpDir.
const NomeSocket = "piztu-api.sock"

// VersionAPI identifica o contrato exposto (ver /api/v1/whoami). Sobe cando
// hai un cambio incompatible nas rutas actuais — un endpoint novo non a move.
const VersionAPI = "v1"

// TTLToken é canto dura un token emitido por EmitirToken antes de deixar de
// validar por si só (ver auth.go). Xeneroso a propósito: non hai renovación
// automática aínda, e un módulo pode quedar aberto toda unha xornada lectiva.
const TTLToken = 12 * time.Hour

// Servidor expón a API sobre un socket Unix. Unha soa instancia por proceso
// de Piztu, creada en startup() e pechada en shutdown() (ver app.go).
type Servidor struct {
	cfg      *config.Config
	axustes  modulos.Axustes // estado activo/inactivo dos módulos, ver GET /modulos
	sesions  *rexistroSesions
	eventos  *rexistroEventos // subscriptores SSE de GET /eventos, ver eventos.go
	listener net.Listener
}

// Novo crea o servidor (sen escoitar aínda: ver Escoitar). axustes é o
// almacén de axustes de Piztu (normalmente *internal/db.Store, xa satisfai
// modulos.Axustes de seu) — pode ser nil (ex. a BD non abriu), nese caso
// GET /modulos devolve só os módulos activos por defecto.
func Novo(cfg *config.Config, axustes modulos.Axustes) *Servidor {
	return &Servidor{cfg: cfg, axustes: axustes, sesions: novoRexistroSesions(), eventos: novoRexistroEventos()}
}

// Escoitar abre o socket Unix e arrinca a servir en fondo. Idempotente fronte
// a un peche anterior non limpo: bórrase calquera socket vello antes de crear
// o novo (senón net.Listen falla con "address already in use").
func (s *Servidor) Escoitar() error {
	ruta := s.RutaSocket()
	_ = os.Remove(ruta)
	l, err := net.Listen("unix", ruta)
	if err != nil {
		return err
	}
	// 0660: o mesmo criterio ca o socket de Xesta ata agora (chmod 660) —
	// primeira capa de defensa (só procesos do mesmo usuario/grupo poden
	// sequera conectar); a segunda é o token, ver auth.go.
	if err := os.Chmod(ruta, 0o660); err != nil {
		l.Close()
		return err
	}
	s.listener = l
	go func() {
		_ = http.Serve(l, s.mux())
	}()
	return nil
}

// Pechar deixa de escoitar e borra o socket. Chamado en shutdown().
func (s *Servidor) Pechar() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.RutaSocket())
}

// RutaSocket é a ruta completa do socket, para pasarlla a un módulo no seu
// ContextoModulo (ver escribirContexto en app.go).
func (s *Servidor) RutaSocket() string {
	return s.cfg.TmpDir + string(os.PathSeparator) + NomeSocket
}

// EmitirToken crea unha sesión nova para moduloID cos permisos indicados —
// chamado ao lanzar un módulo (AbrirModulo), cos permisos xa concedidos
// previamente en ⚙ Aula → Módulos (ver modulos.PermisosConcedidos).
func (s *Servidor) EmitirToken(moduloID string, permisos []string) string {
	return s.sesions.emitir(moduloID, permisos, TTLToken)
}

// Revogar esquece calquera token vivo de moduloID — chamado ao desactivar un
// módulo, para que deixe de poder chamar á API no acto, sen agardar a que
// caduque nin tocar o socket. Tamén é razoable chamalo cando un módulo con
// xanela propia pecha (ver shutdown de AbrirModulo, aínda non hai gancho para
// iso — pendente, ver nota en docs/api-modulos.md).
func (s *Servidor) Revogar(moduloID string) {
	s.sesions.revogarModulo(moduloID)
}

// PedirFoco manda o evento "foco" a moduloID por GET /api/v1/eventos (ver
// eventos.go) — chamado dende AbrirModulo (piztu/app.go) cando o
// profesorado preme outra vez o botón dun módulo que xa está aberto, en vez
// de lanzar un proceso novo. Devolve false se non había ningunha conexión
// SSE viva dese módulo (ex. versión vella sen esta función, ou aínda non
// conectou): non é un erro, só significa que o módulo non vai reaccionar.
func (s *Servidor) PedirFoco(moduloID string) bool {
	return s.eventos.emitir(moduloID, "foco")
}

func (s *Servidor) motor() motor.Motor {
	return motor.Get(s.cfg, "")
}

func (s *Servidor) mux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/whoami", s.recuperar(s.autenticar("", s.handleWhoami)))
	mux.Handle("GET /api/v1/motor/estado", s.recuperar(s.autenticar(PermisoMotorLer, s.handleMotorEstado)))
	mux.Handle("GET /api/v1/equipos", s.recuperar(s.autenticar(PermisoMotorLer, s.handleEquipos)))
	mux.Handle("POST /api/v1/motor/executar", s.recuperar(s.autenticar(PermisoMotorExecutar, s.handleMotorExecutar)))
	mux.Handle("GET /api/v1/modulos", s.recuperar(s.autenticar(PermisoModulosLer, s.handleModulos)))
	mux.Handle("GET /api/v1/eventos", s.recuperar(s.autenticar("", s.handleEventos)))
	return mux
}
