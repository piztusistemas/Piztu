// Package sshkey xera e distribúe a clave SSH compartida da aula SEN depender de
// sshpass nin Ansible: xera co ssh-keygen do sistema e empúrraa a cada cliente
// por SSH nativo (golang.org/x/crypto/ssh), executando as tarefas con sudo.
// Substitúe distribuirClave.yaml + distributeSSHKey do instalador, para que
// piztu configure a aula por si mesmo (tamén desde macOS, onde non hai
// sshpass no sistema).
package sshkey

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"piztu/internal/inventory"

	"golang.org/x/crypto/ssh"
)

// Log é o callback de progreso. host == "" indica unha mensaxe xeral.
type Log func(host, linha string)

// reMac valida unha MAC "aa:bb:cc:dd:ee:ff".
var reMac = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

// cmdMac imprime a MAC da interface de rede por defecto do cliente (a mesma que
// ansible_default_ipv4.macaddress): a interface da ruta por defecto.
const cmdMac = `cat /sys/class/net/$(ip route show default | awk '/default/{print $5; exit}')/address`

// authClave constrúe a autenticación SSH por clave privada (keyPath).
func authClave(keyPath string) (ssh.AuthMethod, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

// LerMac conéctase ao host con autenticación por clave (sen contrasinal, xa que
// a clave se distribuíu no Paso 3) e devolve a MAC da súa interface por defecto.
func LerMac(host, usuario, keyPath string) (string, error) {
	client, err := dialClave(host, usuario, keyPath, 8*time.Second)
	if err != nil {
		return "", err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output(cmdMac)
	if err != nil {
		return "", err
	}
	mac := strings.ToLower(strings.TrimSpace(string(out)))
	if !reMac.MatchString(mac) {
		return "", fmt.Errorf("MAC non válida: %q", mac)
	}
	return mac, nil
}

// Escanear conéctase en paralelo a todos os hosts e devolve {host: mac} dos que
// responderon (equipos acendidos e xa configurados coa clave). Emite progreso
// por `log`.
func Escanear(hosts []string, usuario, keyPath string, log Log) map[string]string {
	res := map[string]string{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mac, err := LerMac(host, usuario, keyPath)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log(host, "⚫ sen resposta")
				return
			}
			res[host] = mac
			log(host, "🟢 acendido — "+mac)
		}(h)
	}
	wg.Wait()
	return res
}

// Xerar crea unha parella de claves RSA en keyPath (+ .pub) se aínda non existe.
// Devolve true se a xerou agora, false se xa existía. Usa o ssh-keygen do
// sistema (dispoñible tanto en Linux como en macOS).
func Xerar(keyPath string) (bool, error) {
	if _, err := os.Stat(keyPath); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return false, err
	}
	cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-N", "", "-f", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("ssh-keygen: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// Distribuir empurra a clave compartida a todos os hosts en paralelo, con sudo.
// Emite progreso por `log`. Non aborta se un equipo falla: cada un é independente.
func Distribuir(hosts []string, usuario, contrasinal, keyPath string, log Log) {
	priv, err := os.ReadFile(keyPath)
	if err != nil {
		log("", "❌ non se puido ler a clave privada: "+err.Error())
		return
	}
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		log("", "❌ non se puido ler a clave pública: "+err.Error())
		return
	}
	script := construirScript(usuario, priv, pub)

	var wg sync.WaitGroup
	var mu sync.Mutex
	safeLog := func(h, l string) { mu.Lock(); log(h, l); mu.Unlock() }

	sem := make(chan struct{}, 8) // limita as conexións simultáneas
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := distribuirNun(host, usuario, contrasinal, script); err != nil {
				safeLog(host, "❌ "+err.Error())
				return
			}
			safeLog(host, "✅ clave instalada")
		}(h)
	}
	wg.Wait()
}

// Credencial é o usuario/contrasinal dun host individual, para
// DistribuirCredenciais (contrasinais distintos por equipo, fronte ao único
// contrasinal compartido de Distribuir()).
type Credencial struct {
	Usuario     string
	Contrasinal string
}

// DistribuirCredenciais é como Distribuir(), pero cada host ten o seu propio
// usuario/contrasinal (por exemplo, equipos atopados por escaneo de rede que
// aínda non seguen a convención da aula). Devolve os hosts nos que tivo éxito.
func DistribuirCredenciais(credenciais map[string]Credencial, keyPath string, log Log) []string {
	priv, err := os.ReadFile(keyPath)
	if err != nil {
		log("", "❌ non se puido ler a clave privada: "+err.Error())
		return nil
	}
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		log("", "❌ non se puido ler a clave pública: "+err.Error())
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var exitosos []string
	safeLog := func(h, l string) { mu.Lock(); log(h, l); mu.Unlock() }

	sem := make(chan struct{}, 8) // limita as conexións simultáneas
	for host, cred := range credenciais {
		wg.Add(1)
		go func(host string, cred Credencial) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			script := construirScript(cred.Usuario, priv, pub)
			if err := distribuirNun(host, cred.Usuario, cred.Contrasinal, script); err != nil {
				safeLog(host, "❌ "+err.Error())
				return
			}
			mu.Lock()
			exitosos = append(exitosos, host)
			mu.Unlock()
			safeLog(host, "✅ clave instalada")
		}(host, cred)
	}
	wg.Wait()
	return exitosos
}

// enderezo resolve `host` a unha IP e engádelle o porto 22 por defecto.
//
// Resólvese aquí, e non se deixa o nome para que o faga o Dial, porque os
// equipos da aula son nomes .local (mDNS) e a resolución adoita devolver
// primeiro a IPv6 link-local (fe80::…%en0). Dial("tcp", …) escolle esa, e coa
// veciñanza aínda sen resolver falla con "no route to host" — o típico "a
// primeira orde tras abrir piztu non vai, a segunda si". inventory.ResolverHost
// prefire sempre a IPv4, que é a que a aula usa de verdade.
func enderezo(host string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return net.JoinHostPort(inventory.ResolverHostCached(host, ""), "22")
}

// dialClave conéctase ao host con autenticación por CLAVE (a compartida da aula,
// xa distribuída no Paso 3).
func dialClave(host, usuario, keyPath string, timeout time.Duration) (*ssh.Client, error) {
	auth, err := authClave(keyPath)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            usuario,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	return ssh.Dial("tcp", enderezo(host), cfg)
}

// runSudoScript executa `script` como root nunha sesión do cliente: `sudo -S` le
// o contrasinal da primeira liña de stdin e o resto (o script) chégalle a
// `bash -s`. Así corremos as tarefas como root sen sshpass nin TTY.
func runSudoScript(client *ssh.Client, contrasinal, script string) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdin = strings.NewReader(contrasinal + "\n" + script)
	var stderr strings.Builder
	sess.Stderr = &stderr
	if err := sess.Run("sudo -S -k -p '' bash -s"); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("sudo: %s", msg)
	}
	return nil
}

// EjecutarSudoConClave conéctase con clave e executa `script` como root (sudo).
// Reutilízao a instalación de Salt: a clave xa existe, o contrasinal só se usa
// para elevar a root no cliente.
func EjecutarSudoConClave(host, usuario, keyPath, contrasinal, script string) error {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	return runSudoScript(client, contrasinal, script)
}

// EjecutarComoUsuario executa `orde` como o usuario normal (SEN sudo) e devolve
// a súa saída estándar. Úsano os recolectores dos módulos: ler un dato do equipo
// non debe precisar privilexios, e así un módulo de terceiros non pode usar o
// recolector para executar nada como root.
func EjecutarComoUsuario(host, usuario, keyPath, orde string) (string, error) {
	client, err := dialClave(host, usuario, keyPath, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output(orde)
	return string(out), err
}

// EjecutarComoRoot conéctase con clave e executa `script` como root SEN
// contrasinal (`sudo -n`), asumindo que o cliente xa ten NOPASSWD configurado
// para o usuario. Úsao o motor SSH. Devolve a saída combinada (stdout+stderr).
func EjecutarComoRoot(host, usuario, keyPath, script string) (string, error) {
	client, err := dialClave(host, usuario, keyPath, 20*time.Second)
	if err != nil {
		return "", fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	sess.Stdin = strings.NewReader(script)
	var buf strings.Builder
	sess.Stdout = &buf
	sess.Stderr = &buf
	if err := sess.Run("sudo -n bash -s"); err != nil {
		return buf.String(), fmt.Errorf("%v: %s", err, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}

// distribuirNun distribúe a clave (Paso 3) autenticándose por CONTRASINAL, xa
// que a clave aínda non está no cliente. Non usa dialClave (que vai por clave).
func distribuirNun(host, usuario, contrasinal, script string) error {
	cfg := &ssh.ClientConfig{
		User:            usuario,
		Auth:            []ssh.AuthMethod{ssh.Password(contrasinal)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}
	client, err := ssh.Dial("tcp", enderezo(host), cfg)
	if err != nil {
		return fmt.Errorf("conexión: %w", err)
	}
	defer client.Close()
	return runSudoScript(client, contrasinal, script)
}

// construirScript devolve o script bash que corre como root en cada cliente.
// Replica distribuirClave.yaml: a clave PRIVADA vai a /etc/skel e ao home do
// usuario (é unha clave compartida da aula), e a PÚBLICA engádese a
// authorized_keys de ambos, sen duplicar. A privada pásase en base64 nunha
// única liña para evitar problemas de comiñas ou saltos de liña.
func construirScript(usuario string, priv, pub []byte) string {
	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubLine := strings.TrimSpace(string(pub))
	return fmt.Sprintf(`set -e
U=%q
UHOME=$(getent passwd "$U" | cut -d: -f6)
UHOME=${UHOME:-/home/$U}
PUB=%q
umask 077

install_priv() {
  mkdir -p "$1"; chmod 700 "$1"
  printf '%%s' %q | base64 -d > "$1/id_rsa"; chmod 600 "$1/id_rsa"
}
add_pub() {
  touch "$1"; chmod 600 "$1"
  grep -qF "$PUB" "$1" || printf '%%s\n' "$PUB" >> "$1"
}

# /etc/skel: novos usuarios herdarán a clave
install_priv /etc/skel/.ssh
add_pub /etc/skel/.ssh/authorized_keys

# usuario actual
install_priv "$UHOME/.ssh"
add_pub "$UHOME/.ssh/authorized_keys"
chown -R "$U":"$U" "$UHOME/.ssh"
`, usuario, pubLine, privB64)
}
