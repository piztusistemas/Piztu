// Comando tao-helper — o único binario de Piztu que corre setgid www-data.
//
// piztu non pode ler directamente dixitalizacion/sesions/ (permisos 750
// www-data:www-data, escrito por Tao a través de File Browser) porque
// marcar o propio executable de piztu setuid/setgid rompe GTK ("Refusing
// to initialize GTK+ ...") — webkit2gtk, usado internamente por Wails, nega
// inicializarse en calquera proceso con eses bits. A solución é un segundo
// binario, sen interface gráfica ningunha, que só fai unha cousa: ler eses
// ficheiros e imprimilos. Ver internal/tao/tao.go → Listar, que é quen o
// invoca (exec.Command) e parsea a súa saída.
//
// Instalación (ver installer/app.go): cópiase canda piztu, e só el —
// nunca piztu — recibe chown ao grupo www-data + chmod g+s.
//
// Uso: tao-helper <cartafol-de-sesións>
// Saída (stdout): {"<ficheiro>.json": <contido cru do ficheiro>, ...}
//
// Tolerante a propósito: un cartafol inexistente ou baleiro imprime "{}" e
// sae con éxito (é o estado normal xusto despois de instalar, antes de que
// ningún equipo publique sesión); un ficheiro individual que non sexa JSON
// válido sáltase sen afectar aos demais — nunca se deixa que un ficheiro
// corrompido tire abaixo a listaxe enteira (por iso se valida aquí con
// json.Valid antes de reenviar o contido cru, en vez de confiar nel tal
// cal: un json.RawMessage inválido invalidaría o obxecto exterior enteiro
// cando internal/tao.Listar fixese o seu propio Unmarshal).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: tao-helper <cartafol-de-sesións>")
		os.Exit(1)
	}
	dir := os.Args[1]

	resultado := map[string]json.RawMessage{}
	entradas, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entradas {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			datos, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil || !json.Valid(datos) {
				continue // ficheiro individual corrompido: sáltase, non tira todo
			}
			resultado[e.Name()] = json.RawMessage(datos)
		}
	}
	// err != nil (cartafol inexistente/sen permiso): resultado queda baleiro,
	// imprímese "{}" igual — internal/tao.Listar interprétao coma "sen sesións".

	saida, err := json.Marshal(resultado)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro codificando a saída:", err)
		os.Exit(1)
	}
	os.Stdout.Write(saida)
}
