//go:build bindings

package main

// intentarElevar non fai nada cando o binario se compila co tag "bindings":
// `wails build` compila e executa unha copia deste programa con ese tag
// para inspeccionar por reflexión os métodos expostos de App (así xera
// frontend/wailsjs/go/main/App.js). Se aquí intentásemos reexecutarnos con
// pkexec coma no build normal, o proceso elevado perdería o directorio de
// traballo correcto (pkexec non conserva o cwd do chamador) e "wails build"
// fallaría ao non atopar wails.json. Ver elevate_normal.go para o
// comportamento real (produción).
func intentarElevar() (saiuOK, permisoDenegado bool) { return false, false }
