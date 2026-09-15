//go:build !bindings

package main

import (
	"os"
	"os/exec"
	"runtime"
)

// intentarElevar reexecuta o proceso como root vía pkexec cando fai falta
// (Linux, non root). Se saiuOK é true, xa se reexecutou con éxito e main()
// debe saír sen abrir xanela ningunha (a instancia root xestionou todo). Se
// non, permisoDenegado indica se pkexec fallou/foi cancelado (para amosar a
// pantalla de instrucións no frontend en vez do asistente).
//
// En macOS non existe pkexec e unha GUI executada como root (sudo) non pode
// conectar co WindowServer do usuario. A escalada de privilexios só ten
// sentido en Linux (a máquina real do profesor); en macOS arrincamos
// directos á interface para poder probar/ver o instalador (as operacións
// que precisan root fallarán, pero permite revisar a UI).
func intentarElevar() (saiuOK, permisoDenegado bool) {
	if runtime.GOOS == "darwin" || os.Getuid() == 0 {
		return false, false
	}

	// pkexec non propaga DISPLAY por defecto: usamos "env" para pasalo explicitamente
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command("pkexec", "env",
			"DISPLAY="+os.Getenv("DISPLAY"),
			"XAUTHORITY="+os.Getenv("XAUTHORITY"),
			exe,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Run() == nil {
			return true, false // a instancia root xestionou todo
		}
	}
	// pkexec non dispoñible ou cancelado: seguimos igualmente, pero o
	// frontend só amosará a pantalla de instrucións (ver App.PermisoDenegado).
	return false, true
}
