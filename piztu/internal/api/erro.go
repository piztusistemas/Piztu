package api

import (
	"encoding/json"
	"net/http"
)

// erroResposta é a envolvente de erro única de toda a API (ver
// docs/api-modulos.md §6) — sempre JSON, nunca a páxina HTML por defecto,
// nin sequera nun panic (ver recuperar en auth.go).
type erroResposta struct {
	Erro erroCorpo `json:"erro"`
}

type erroCorpo struct {
	Codigo  string `json:"codigo"`
	Mensaxe string `json:"mensaxe"`
}

func escribirErro(w http.ResponseWriter, status int, codigo, mensaxe string) {
	escribirJSON(w, status, erroResposta{Erro: erroCorpo{Codigo: codigo, Mensaxe: mensaxe}})
}

func escribirJSON(w http.ResponseWriter, status int, dato any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dato)
}
