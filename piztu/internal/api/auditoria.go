package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FicheiroAuditoria é onde queda o rexistro de quen executou que — deliberado
// á parte de calquera almacén de Tao (historial_tao): isto é conta de
// módulos administrativos (Xesta, futuros), non de alumnado. Ver
// docs/api-modulos.md §8.
const FicheiroAuditoria = "auditoria_execucions.jsonl"

type rexistroExecucion struct {
	Hora     string   `json:"hora"`
	ModuloID string   `json:"modulo_id"`
	Motor    string   `json:"motor"`
	Destinos []string `json:"destinos"`
	Resumo   string   `json:"resumo_contido"`
}

var auditoriaMu sync.Mutex

// rexistrarAuditoria engade unha liña JSON ao ficheiro de auditoría (append,
// nunca reescribe nin borra). Un erro aquí non aborta a execución que se
// acaba de facer — xa é tarde para iso — só se ignora en silencio; a
// execución en si xa respondeu ao módulo.
func (s *Servidor) rexistrarAuditoria(moduloID, motorNome string, destinos []string, contido string) {
	rexistro := rexistroExecucion{
		Hora:     time.Now().Format(time.RFC3339),
		ModuloID: moduloID,
		Motor:    motorNome,
		Destinos: destinos,
		Resumo:   primeiraLiña(contido),
	}
	data, err := json.Marshal(rexistro)
	if err != nil {
		return
	}
	auditoriaMu.Lock()
	defer auditoriaMu.Unlock()
	ruta := filepath.Join(s.cfg.BaseDir, FicheiroAuditoria)
	f, err := os.OpenFile(ruta, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// primeiraLiña resume o contido executado para a auditoría, sen gardalo
// enteiro (podería ser longo; abonda para recoñecer de que ía).
func primeiraLiña(contido string) string {
	liña, _, _ := strings.Cut(contido, "\n")
	liña = strings.TrimSpace(liña)
	const max = 200
	if len(liña) > max {
		liña = liña[:max] + "…"
	}
	return liña
}
