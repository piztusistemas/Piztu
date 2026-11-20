package sshkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// rutasPracticas resolve no cliente o(s) cartafol(es) de prácticas do
// usuario, sen asumir se o escritorio está en galego (Escritorio) ou en
// inglés (Desktop): pregunta directamente ao cliente cales dos dous existen.
//
//   - Existe só un dos dous → ese, con confianza.
//   - Existen os dous, ou non existe ningún (perfil aínda sen abrir sesión
//     gráfica, por exemplo) → dúbida real: devólvense os dous, para que
//     EnviarFicheiros/LimparPracticas/RecollerPracticas actúen sobre ambos e
//     non arrisquen deixar (ou buscar) as prácticas no cartafol que non toca.
func rutasPracticas(client *ssh.Client, usuario, sub string, windows bool) ([]string, error) {
	if windows {
		return rutasPracticasWindows(client, sub)
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	cmd := fmt.Sprintf(
		`H=$(getent passwd %q | cut -d: -f6); H=${H:-/home/%s}; echo "$H"; `+
			`[ -d "$H/Escritorio" ] && echo 1 || echo 0; `+
			`[ -d "$H/Desktop" ] && echo 1 || echo 0`,
		usuario, usuario)
	out, err := sess.Output(cmd)
	if err != nil {
		return nil, err
	}
	liñas := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(liñas) != 3 {
		return nil, fmt.Errorf("non se puido resolver o cartafol de prácticas")
	}
	home := strings.TrimSpace(liñas[0])
	if home == "" {
		return nil, fmt.Errorf("non se puido resolver o cartafol de prácticas")
	}
	existeEsc := strings.TrimSpace(liñas[1]) == "1"
	existeDesk := strings.TrimSpace(liñas[2]) == "1"

	switch {
	case existeEsc && !existeDesk:
		return []string{home + "/Escritorio/" + sub}, nil
	case existeDesk && !existeEsc:
		return []string{home + "/Desktop/" + sub}, nil
	default:
		// Os dous existen ou non existe ningún: dúbida, van os dous.
		return []string{home + "/Escritorio/" + sub, home + "/Desktop/" + sub}, nil
	}
}

// rutasPracticasWindows resolve o cartafol de prácticas nun cliente Windows.
// A diferenza de Linux non hai ambigüidade Escritorio/Desktop (hai un só
// escritorio), pero pode estar redirixido a OneDrive (típico con conta
// Microsoft), así que se lle pregunta a PowerShell pola ruta real en vez de
// asumir %USERPROFILE%\Desktop. Devólvese unha soa entrada, con "/" en lugar
// de "\": tanto o cliente SFTP coma o sftp-server de OpenSSH en Windows
// traballan con rutas estilo "C:/Users/...".
func rutasPracticasWindows(client *ssh.Client, sub string) ([]string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	// A shell por defecto do usuario en OpenSSH pode ser cmd.exe ou powershell:
	// pásase o script como -EncodedCommand (base64 de UTF-16LE) para non
	// depender do escapado de comiñas de ningunha das dúas.
	script := `$d=[Environment]::GetFolderPath('DesktopDirectory'); ` +
		`if(-not $d){$d=Join-Path $env:USERPROFILE 'Desktop'}; Write-Output $d`
	ps := "powershell -NoProfile -NonInteractive -EncodedCommand " + base64UTF16LE(script)
	out, err := sess.Output(ps)
	if err != nil {
		return nil, fmt.Errorf("resolvendo o escritorio de Windows: %w", err)
	}
	escritorio := ""
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			escritorio = l // a última liña non baleira é a ruta
		}
	}
	if escritorio == "" {
		return nil, fmt.Errorf("non se puido resolver o escritorio de Windows")
	}
	escritorio = strings.TrimRight(strings.ReplaceAll(escritorio, "\\", "/"), "/")
	return []string{escritorio + "/" + sub}, nil
}

// base64UTF16LE codifica `s` como espera PowerShell en -EncodedCommand:
// base64 dos bytes UTF-16 little-endian.
func base64UTF16LE(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[i*2] = byte(r)
		b[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// SubirPracticas sobe por SFTP os ficheiros locais ao(s) cartafol(es) de
// prácticas do usuario no host (créao(s) se non existe(n)). Conéctase por
// clave (como usuario, sen root: os ficheiros quedan co propietario
// correcto). `windows` escolle a resolución da ruta (PowerShell vs. bash).
func SubirPracticas(host, usuario, keyPath, sub string, locais []string, windows bool) error {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	destinos, err := rutasPracticas(client, usuario, sub, windows)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sc.Close()
	for _, remoteDir := range destinos {
		if err := sc.MkdirAll(remoteDir); err != nil {
			return fmt.Errorf("creando %s: %w", remoteDir, err)
		}
		for _, local := range locais {
			if err := copiarASftp(sc, local, remoteDir+"/"+filepath.Base(local)); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecollerPracticas baixa por SFTP os ficheiros do(s) cartafol(es) de
// prácticas do cliente a localDir (sen duplicar un mesmo nome se estaba nos
// dous candidatos). Devolve o nº de ficheiros baixados.
func RecollerPracticas(host, usuario, keyPath, sub, localDir string, windows bool) (int, error) {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return 0, fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	orixes, err := rutasPracticas(client, usuario, sub, windows)
	if err != nil {
		return 0, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		return 0, fmt.Errorf("abrindo SFTP: %w", err)
	}
	defer sc.Close()
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return 0, fmt.Errorf("creando dir local %s: %w", localDir, err)
	}
	vistos := map[string]bool{}
	n := 0
	for _, remoteDir := range orixes {
		infos, err := sc.ReadDir(remoteDir)
		if err != nil {
			// Cartafol candidato inexistente: pasa ao seguinte, non é un fallo.
			if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "does not exist") {
				continue
			}
			return n, fmt.Errorf("listando %s: %w", remoteDir, err)
		}
		for _, fi := range infos {
			if fi.IsDir() || vistos[fi.Name()] {
				continue
			}
			if err := copiarDeSftp(sc, remoteDir+"/"+fi.Name(), filepath.Join(localDir, fi.Name())); err != nil {
				return n, fmt.Errorf("descargando %s: %w", fi.Name(), err)
			}
			vistos[fi.Name()] = true
			n++
		}
	}
	return n, nil
}

// LimparPracticas borra os ficheiros do(s) cartafol(es) de prácticas do
// cliente.
func LimparPracticas(host, usuario, keyPath, sub string, windows bool) error {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	destinos, err := rutasPracticas(client, usuario, sub, windows)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sc.Close()
	for _, remoteDir := range destinos {
		infos, err := sc.ReadDir(remoteDir)
		if err != nil {
			continue // nada que limpar nese candidato
		}
		for _, fi := range infos {
			if !fi.IsDir() {
				_ = sc.Remove(remoteDir + "/" + fi.Name())
			}
		}
	}
	return nil
}

// SubirFicheiro sobe un só ficheiro local a `remoto` (ruta absoluta no host)
// por SFTP, autenticándose por clave (como `usuario`, sen root). `remoto`
// pode ser estilo POSIX (/home/…) ou Windows con barras (`C:/Users/…`): o
// sftp-server de OpenSSH en Windows acepta as dúas. Créanse os directorios
// intermedios que falten.
func SubirFicheiro(host, usuario, keyPath, local, remoto string) error {
	client, err := dialClave(host, usuario, keyPath, 30*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	sc, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sc.Close()
	if dir := path.Dir(remoto); dir != "" && dir != "." && dir != "/" {
		_ = sc.MkdirAll(dir)
	}
	return copiarASftp(sc, local, remoto)
}

func copiarASftp(sc *sftp.Client, local, remoto string) error {
	src, err := os.Open(local)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := sc.Create(remoto)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func copiarDeSftp(sc *sftp.Client, remoto, local string) error {
	src, err := sc.Open(remoto)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(local)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// EjecutarPowerShell executa `script` (PowerShell) nun cliente Windows con
// OpenSSH Server, autenticándose por clave como `usuario`. En Windows non hai
// sudo: o usuario ten que ser ADMINISTRADOR local (OpenSSH dálle a unha conta
// de Administradores un token elevado, sen UAC), igual que en Linux se lle pide
// NOPASSWD. Úsao o motor SSH para os equipos Windows.
//
// O script sóbese por SFTP a un .ps1 no perfil do usuario e lánzase con
// `-File` en vez de -EncodedCommand: os scripts de bloqueo pasan dos 8 KB que
// admite a liña de comandos de cmd.exe (a shell por defecto de OpenSSH). O
// .ps1 bórrase ao rematar. Devolve a saída combinada (stdout+stderr).
func EjecutarPowerShell(host, usuario, keyPath, script string) (string, error) {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return "", fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()

	sc, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("sftp: %w", err)
	}
	home, err := sc.Getwd()
	if err != nil || home == "" {
		sc.Close()
		return "", fmt.Errorf("non se puido saber o cartafol do usuario: %v", err)
	}
	// O sftp-server de Windows devolve "/C:/Users/x": quítase a "/" inicial
	// para pasarllo a PowerShell; SFTP acepta as dúas formas.
	remoto := strings.TrimRight(home, "/") + fmt.Sprintf("/piztu_accion_%d.ps1", time.Now().UnixNano())
	f, err := sc.Create(remoto)
	if err != nil {
		sc.Close()
		return "", fmt.Errorf("subindo o script: %w", err)
	}
	// BOM UTF-8: sen el, Windows PowerShell 5.1 le o .ps1 coma ANSI e estraga
	// os textos con acentos.
	_, err = f.Write(append([]byte{0xEF, 0xBB, 0xBF}, []byte(script)...))
	f.Close()
	if err != nil {
		sc.Close()
		return "", fmt.Errorf("subindo o script: %w", err)
	}
	defer func() { _ = sc.Remove(remoto); sc.Close() }()

	rutaWin := strings.TrimPrefix(remoto, "/")
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var buf strings.Builder
	sess.Stdout = &buf
	sess.Stderr = &buf
	orde := `powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "` + rutaWin + `"`
	if err := sess.Run(orde); err != nil {
		return buf.String(), fmt.Errorf("%v: %s", err, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}

// SondarSO pregunta a un equipo polo seu SO por SSH, sen saber de antemán que
// shell ten. Dúas ordes en sesións separadas, NON `uname -s || ver` nunha soa:
// en Windows a shell por defecto de OpenSSH pode ser cmd.exe OU PowerShell
// (DefaultShell no rexistro, o preparador pona en PowerShell), e no Windows
// PowerShell 5.1 `||` é un erro de sintaxe — a sonda devolvía só ese erro, o
// equipo quedaba coma SODescoñecido e tratábase coma Linux (visto en bac01:
// "Bloquear" mandaba bash con sudo e non facía nada).
//
//  1. `uname -s`    → responde en Linux/macOS.
//  2. `cmd /c ver`  → "Microsoft Windows [Versión …]" tanto desde cmd.exe
//     coma desde PowerShell.
//
// Devolve a saída en bruto; interprétaa motor.parsearSondaSO. Timeout curto:
// corre antes dunha acción.
func SondarSO(host, usuario, keyPath string, timeout time.Duration) (string, error) {
	client, err := dialClave(host, usuario, keyPath, timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()
	orde := func(cmd string) (string, error) {
		sess, err := client.NewSession()
		if err != nil {
			return "", err
		}
		defer sess.Close()
		out, _ := sess.CombinedOutput(cmd)
		return string(out), nil
	}
	out, err := orde("uname -s")
	if err != nil {
		return "", err
	}
	if o := strings.ToLower(out); strings.Contains(o, "linux") || strings.Contains(o, "darwin") {
		return out, nil
	}
	ver, err := orde("cmd /c ver")
	if err != nil {
		return out, nil
	}
	return out + "\n" + ver, nil
}
