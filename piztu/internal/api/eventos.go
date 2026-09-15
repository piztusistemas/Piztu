package api

import (
	"fmt"
	"net/http"
	"sync"
)

// Package-level: GET /api/v1/eventos — Server-Sent Events de Piztu cara a un
// módulo en marcha. É o oco que docs/api-modulos.md §7 xa deixaba pensado
// ("candidato v1.1"): hoxe só se implementa un evento, "foco" (ver PedirFoco
// en api.go, chamado dende AbrirModulo en piztu/app.go cando o profesorado
// preme outra vez o botón dun módulo que xa está aberto). O resto do
// catálogo que menciona o doc (cambios de motor, equipos conectados) segue
// pendente — engadirase reutilizando este mesmo mecanismo cando haxa unha
// falla real que o precise.

// rexistroEventos garda, por moduloID, os subscriptores SSE activos. Un
// módulo pode ter máis dunha conexión aberta á vez (ex. reconectando tras un
// corte) — por iso é unha lista, non un único canle.
type rexistroEventos struct {
	mu  sync.Mutex
	por map[string][]chan string
}

func novoRexistroEventos() *rexistroEventos {
	return &rexistroEventos{por: map[string][]chan string{}}
}

// subscribir rexistra un canle novo para moduloID e devolve unha función
// para anular a subscrición (chamada cando remata a conexión HTTP).
func (r *rexistroEventos) subscribir(moduloID string) (canle chan string, anular func()) {
	c := make(chan string, 4)
	r.mu.Lock()
	r.por[moduloID] = append(r.por[moduloID], c)
	r.mu.Unlock()
	return c, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		lista := r.por[moduloID]
		for i, existente := range lista {
			if existente == c {
				r.por[moduloID] = append(lista[:i], lista[i+1:]...)
				break
			}
		}
		close(c)
	}
}

// emitir manda `evento` a todos os subscriptores vivos de moduloID. Devolve
// false se non había ningún — quen chama (PedirFoco) úsao para saber se
// realmente chegou a algures, sen que iso sexa un erro.
func (r *rexistroEventos) emitir(moduloID, evento string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	lista := r.por[moduloID]
	for _, c := range lista {
		select {
		case c <- evento:
		default: // subscriptor lento/atascado: non bloquear os demais
		}
	}
	return len(lista) > 0
}

// handleEventos serve GET /api/v1/eventos: mantén a conexión aberta e vai
// escribindo cada evento coma unha liña SSE ("data: <evento>\n\n") ata que o
// módulo peche a conexión (r.Context().Done()). Sen permiso concreto: un
// token válido abonda para subscribirse aos seus propios eventos, non é
// información sensible.
func (s *Servidor) handleEventos(w http.ResponseWriter, r *http.Request, ses sesion) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		escribirErro(w, http.StatusInternalServerError, "sen_streaming", "o servidor non soporta streaming")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	canle, anular := s.eventos.subscribir(ses.moduloID)
	defer anular()

	for {
		select {
		case <-r.Context().Done():
			return
		case evento := <-canle:
			fmt.Fprintf(w, "data: %s\n\n", evento)
			flusher.Flush()
		}
	}
}
