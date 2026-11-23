// Package recursos embebe os recursos de tempo de execución que antes despregaba
// o instalador (os playbooks de Ansible e os estados de Salt), para que piztu
// sexa autocontido tamén en macOS, onde base_dir
// (~/Library/Application Support/piztu) arranca baleiro. O motor SSH non
// precisa isto: leva os scripts inline.
//
// NOTA: recursos/playbooks/, recursos/salt/ e recursos/idiomas/ son copias das
// carpetas de /opt/piztu (go:embed só alcanza ficheiros dentro do módulo). Se
// cambian os orixinais, hai que recopialas aquí:
//
//	cp -r ../playbooks recursos/playbooks
//	cp -r ../salt      recursos/salt
//	cp -r ../idiomas   recursos/idiomas
package recursos

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:playbooks
var playbooksFS embed.FS

//go:embed all:salt
var saltFS embed.FS

//go:embed all:idiomas
var idiomasFS embed.FS

//go:embed tao-icon.png
var taoIconPNG []byte

// ScriptSalt devolve un ficheiro embebido de recursos/salt (ex.
// "win_bloqueo.ps1"). Úsao o motor SSH para os equipos Windows, que executa
// por SSH os mesmos scripts PowerShell que os estados Salt mandan por
// `cmd.script` — unha soa fonte para os dous motores.
func ScriptSalt(nome string) (string, error) {
	b, err := saltFS.ReadFile("salt/" + nome)
	return string(b), err
}

// IconaTao devolve os bytes da icona de Tao (a mesma que usa o instalador en
// installer/tao/icon.png), para InstalarTaoClientes escribila nun temporal e
// pasarllo ao playbook coma tao_icon_src.
func IconaTao() []byte { return taoIconPNG }

// DesplegarPlaybooks extrae os playbooks embebidos a destDir se aínda non hai
// ningún alí (primeira execución). Necesarios para o motor Ansible.
func DesplegarPlaybooks(destDir string) (int, error) {
	return desplegar(playbooksFS, "playbooks", destDir, ".yaml")
}

// DesplegarEstadosSalt extrae os estados .sls embebidos a destDir se aínda non
// hai ningún alí. Necesarios para os motores Salt e Salt-SSH: sen isto, en macOS
// (onde base_dir arranca baleiro) calquera acción falla con "Estado Salt non
// atopado", porque os .sls quedan no repositorio e non no base_dir real.
//
// Os scripts PowerShell (win_*.ps1) que usan as ramas Windows dos estados
// (`cmd.script salt://win_….ps1`) cópianse SEMPRE que falten, aínda que xa
// houbese .sls: nunha instalación anterior á aula mixta, sen isto, o master
// nunca os serviría e o bloqueo de Windows fallaría. Nunca se pisa un que xa
// exista (pode estar editado).
func DesplegarEstadosSalt(destDir string) (int, error) {
	n, err := desplegar(saltFS, "salt", destDir, ".sls")
	if err != nil {
		return n, err
	}
	m, err := desplegarFaltantes(saltFS, "salt", destDir, ".ps1")
	return n + m, err
}

// desplegarFaltantes copia de `raiz` a destDir só os ficheiros con extensión
// `ext` que aínda non existen alí (non entra en subcartafoles).
func desplegarFaltantes(fsys embed.FS, raiz, destDir, ext string) (int, error) {
	entradas, err := fsys.ReadDir(raiz)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, err
	}
	escritos := 0
	for _, e := range entradas {
		if e.IsDir() || filepath.Ext(e.Name()) != ext {
			continue
		}
		destino := filepath.Join(destDir, e.Name())
		if _, err := os.Stat(destino); err == nil {
			continue
		}
		data, err := fsys.ReadFile(raiz + "/" + e.Name())
		if err != nil {
			return escritos, err
		}
		if err := os.WriteFile(destino, data, 0o644); err != nil {
			return escritos, err
		}
		escritos++
	}
	return escritos, nil
}

// DesplegarIdiomas extrae os ficheiros de idioma a destDir se aínda non hai
// ningún alí.
//
// Sen isto, en macOS a aplicación non atopaba NINGUNHA tradución: lánzase dende
// Finder co directorio de traballo en "/", así que nin "idiomas" nin
// "../idiomas" existen, e base_dir arranca baleiro. A interface disimulábao
// porque case todos os textos teñen unha reserva escrita no código, pero a
// pantalla "Sobre Piztu" non as tiña e amosaba as claves en cru
// (sobre.version, sobre.autor…).
func DesplegarIdiomas(destDir string) (int, error) {
	return desplegar(idiomasFS, "idiomas", destDir, ".lang")
}

// desplegar copia `raiz` do sistema de ficheiros embebido a destDir. Non toca
// nada se xa hai algún ficheiro coa extensión `ext`: o profesor pode telos
// editado e non queremos pisarlle os cambios.
func desplegar(fsys embed.FS, raiz, destDir, ext string) (int, error) {
	if entradas, err := os.ReadDir(destDir); err == nil {
		for _, e := range entradas {
			if !e.IsDir() && filepath.Ext(e.Name()) == ext {
				return 0, nil
			}
		}
	}

	escritos := 0
	err := fs.WalkDir(fsys, raiz, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// "<raiz>/x.yaml" → "<destDir>/x.yaml"
		rel, err := filepath.Rel(raiz, ruta)
		if err != nil {
			return err
		}
		destino := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(destino, 0o755)
		}
		data, err := fsys.ReadFile(ruta)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destino, data, 0o644); err != nil {
			return err
		}
		escritos++
		return nil
	})
	return escritos, err
}
