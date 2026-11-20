// clientes_win.go: xera o preparador dos equipos Windows do alumnado xa
// enchido cos datos desta aula (enderezo do servidor, clave pública, usuario),
// para que o profesorado só teña que copialo a unha memoria USB e facer dobre
// clic en cada equipo. Ver clientes_windows/preparar_cliente_windows.ps1.
//
// (Nome do ficheiro: non "…_windows.go", que Go só compilaría en Windows.)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"piztu/internal/saltsetup"
	"piztu/internal/sshkey"
	"piztu/recursos"
)

// PrepararClientesWindows escribe en <base_dir>/clientes_windows/ o .ps1 (cos
// datos da aula) e o .bat que o lanza coma administrador, abre ese cartafol
// no xestor de ficheiros e devolve a súa ruta. Se a clave da aula aínda non
// existe, créaa (a mesma que reparte DistribuirClaveSSH aos Linux).
func (a *App) PrepararClientesWindows() (string, error) {
	keyPath := a.claveKeyPath()
	if _, err := sshkey.Xerar(keyPath); err != nil {
		return "", fmt.Errorf("non se puido crear a clave da aula: %w", err)
	}
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return "", fmt.Errorf("non se puido ler a clave pública: %w", err)
	}

	cfg := a.GetAulaConfig()
	servidor := strings.TrimSpace(cfg.SaltMaster)
	if servidor == "" {
		servidor = saltsetup.EnderezoMaster(cfg.Dominio)
	}
	usuario := strings.TrimSpace(cfg.SSHUser)
	if usuario == "" {
		usuario = "usuario"
	}
	ps1, bat, err := recursos.PreparadorWindows()
	if err != nil {
		return "", err
	}
	// Literais de PowerShell entre comiñas simples: só hai que dobrar as que
	// traian os propios valores.
	lit := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	texto := strings.NewReplacer(
		"@@SERVIDOR@@", lit(servidor),
		"@@CLAVE_PUBLICA@@", lit(strings.TrimSpace(string(pub))),
		"@@USUARIO@@", lit(usuario),
	).Replace(string(ps1))

	dir := filepath.Join(a.cfg.BaseDir, "clientes_windows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "preparar_cliente_windows.ps1"), []byte(texto), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "preparar_cliente_windows.bat"), bat, 0o644); err != nil {
		return "", err
	}
	_ = exec.Command("xdg-open", dir).Start()
	return dir, nil
}
