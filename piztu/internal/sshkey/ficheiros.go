package sshkey

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
func rutasPracticas(client *ssh.Client, usuario, sub string) ([]string, error) {
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

// SubirPracticas sobe por SFTP os ficheiros locais ao(s) cartafol(es) de
// prácticas do usuario no host (créao(s) se non existe(n)). Conéctase por
// clave (como usuario, sen root: os ficheiros quedan co propietario
// correcto).
func SubirPracticas(host, usuario, keyPath, sub string, locais []string) error {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	destinos, err := rutasPracticas(client, usuario, sub)
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
func RecollerPracticas(host, usuario, keyPath, sub, localDir string) (int, error) {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return 0, fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	orixes, err := rutasPracticas(client, usuario, sub)
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
func LimparPracticas(host, usuario, keyPath, sub string) error {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	destinos, err := rutasPracticas(client, usuario, sub)
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
