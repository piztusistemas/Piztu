// Package saltsetup instala e configura Salt de forma nativa (sen Ansible),
// integrando no propio piztu o que antes facía o Paso 5 do instalador:
//   - salt-master no controlador (só Linux; en macOS non é practicable),
//   - salt-minion en cada cliente por SSH con clave (idempotente),
//   - aceptación das chaves dos minions no master.
//
// As operacións privilexiadas execútanse con `sudo -S` alimentando o contrasinal
// por stdin, porque piztu corre como sesión de escritorio normal (non root).
package saltsetup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"piztu/internal/sshkey"
)

// Log é o callback de progreso. host == "" indica unha mensaxe xeral.
type Log func(host, linha string)

// ErrMasterNonSoportado devólvese ao tentar instalar o master fóra de Linux.
var ErrMasterNonSoportado = errors.New("o salt-master só se pode instalar en Linux")

// URLs do repositorio de SaltProject (as mesmas que usa o instalador).
const (
	gpgKeyURL  = "https://packages.broadcom.com/artifactory/api/security/keypair/SaltProjectKey/public"
	sourcesURL = "https://github.com/saltstack/salt-install-guide/releases/latest/download/salt.sources"
	keyringF   = "/etc/apt/keyrings/salt-archive-keyring.pgp"
)

// MasterInstalado indica se este equipo xa ten salt-master (ou as ferramentas
// do master) dispoñibles.
func MasterInstalado() bool {
	for _, bin := range []string{"salt-master", "salt-key"} {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}

// DesinstalarMaster para e desinstala salt-master neste equipo (só Linux), para
// partir de cero antes de (re)instalar: unha instalación vella ou a medio
// configurar (repositorio distinto, file_roots obsoleto...) é máis doado de
// depurar borrándoa que tentando adiviñar en que estado quedou. Idempotente:
// se non estaba instalado, os comandos simplemente non fan nada (non é un erro).
func DesinstalarMaster(contrasinal string, log Log) error {
	if runtime.GOOS == "darwin" {
		return ErrMasterNonSoportado
	}
	return runLocalSudo(contrasinal, desinstalarMasterScript, log)
}

// InstalarMaster instala e activa salt-master neste equipo (só Linux), engade o
// repositorio de SaltProject e a regra sudoers NOPASSWD que o motor Salt precisa
// para `sudo -n salt`. Porte de instalarSaltMasterServidor + permitirSaltSenSudo.
func InstalarMaster(contrasinal string, log Log) error {
	if runtime.GOOS == "darwin" {
		return ErrMasterNonSoportado
	}
	usuario := usuarioActual()
	script := strings.ReplaceAll(masterScript, "@@USER@@", usuario)
	return runLocalSudo(contrasinal, script, log)
}

// OpcionsMaster son os axustes que piztu impón ao salt-master, todos nun
// drop-in propio de /etc/salt/master.d/ (nunca en /etc/salt/master, que é do
// paquete e apt sobrescribiría).
type OpcionsMaster struct {
	// SaltDir é o cartafol de estados de Piztu (cfg.SaltDir): a raíz principal
	// do fileserver.
	SaltDir string
	// Extras son raíces adicionais do fileserver (o cartafol de módulos e o de
	// cada .sls que traen). Ver ConfigurarMaster.
	Extras []string
	// Autosign son os patróns de id de minion que se aceptan sen intervención
	// (ex. "tux*"). Baleiro = sen autosign (só `salt-key -A` manual).
	Autosign []string
}

// ConfigurarMaster deixa o salt-master listo para Piztu. Fai dúas cousas que
// non viñan no porte orixinal de instalarSaltMasterServidor e que, faltando,
// deixan o motor Salt inservible ou incomodísimo:
//
//  1. file_roots → cfg.SaltDir (+ Extras). Sen isto o master serve cero
//     ficheiros e CALQUERA state.apply falla cun "No matching sls found for
//     '<estado>' in env 'base'" aínda que os minions respondan a test.ping (bug
//     real, visto en produción: /etc/salt/master trae file_roots todo comentado
//     e o defecto, /srv/salt, nin sequera existe).
//  2. autosign_file → /etc/salt/autosign.conf cos patróns de Autosign, para que
//     un equipo reinstalado volva entrar só, sen `salt-key -a` á man.
//
// É idempotente e vai á parte de InstalarMaster a propósito: hai que aplicalo
// tamén nos equipos que XA tiñan salt-master instalado (ver app.go), que son
// precisamente os que quedaron rotos.
func ConfigurarMaster(o OpcionsMaster, contrasinal string, log Log) error {
	if runtime.GOOS == "darwin" {
		return ErrMasterNonSoportado
	}
	if !filepath.IsAbs(o.SaltDir) {
		return errors.New("cartafol de estados Salt non válido: " + o.SaltDir)
	}
	raices := raicesValidas(o.SaltDir, o.Extras)
	var b strings.Builder
	for _, r := range raices {
		// %q e non o valor cru: unha ruta con espazos ou dous puntos rompería
		// o YAML do drop-in. Vai dentro dun heredoc entrecomiñado ('CFG'), así
		// que a shell non expande nada.
		fmt.Fprintf(&b, "    - %q\n", r)
	}

	patróns := patronsValidos(o.Autosign)
	// O drop-in de master.d/ xa fixa autosign_file (e ten prioridade), pero
	// deixámola tamén descomentada en /etc/salt/master: é o que o profesor
	// espera ver aí e o paquete tráea comentada ("#autosign_file: ...").
	masterAutosign := descomentarAutosignMaster
	if len(patróns) == 0 {
		masterAutosign = "true  # sen patróns de autosign: non se toca /etc/salt/master"
	}
	script := strings.NewReplacer(
		"@@ROOTS@@", strings.TrimRight(b.String(), "\n"),
		"@@RESUMO@@", strings.Join(raices, ", "),
		"@@AUTOSIGN@@", strings.Join(patróns, "\n"),
		"@@AUTOSIGN_RESUMO@@", strings.Join(patróns, " "),
		"@@MASTER_AUTOSIGN@@", masterAutosign,
	).Replace(fileRootsScript)
	if len(patróns) == 0 {
		// Sen patróns non se escribe autosign_file: apuntar a un ficheiro baleiro
		// non fai dano, pero deixa no master unha opción que non significa nada.
		script = strings.ReplaceAll(script, autosignBloque, "")
	}
	return runLocalSudo(contrasinal, script, log)
}

// patronsValidos filtra os patróns de autosign a caracteres de nome de host máis
// os comodíns de fnmatch. É saneamento, non cosmética: o patrón vén dun campo de
// texto da interface (o prefixo da aula) e escríbese cru nun ficheiro que
// decide QUE máquinas entran soas na aula — unha liña colada aí (un "\n*") sería
// unha porta aberta a calquera minion.
func patronsValidos(patróns []string) []string {
	var fóra []string
	vistos := map[string]bool{}
	for _, p := range patróns {
		p = strings.TrimSpace(p)
		if p == "" || p == "*" || vistos[p] {
			continue // "*" aceptaría literalmente calquera minion: nunca.
		}
		if strings.IndexFunc(p, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
				r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' ||
				r == '*' || r == '?')
		}) >= 0 {
			continue
		}
		vistos[p] = true
		fóra = append(fóra, p)
	}
	return fóra
}

// raicesValidas normaliza a lista de file_roots: saltDir primeiro (é onde viven
// os estados propios de Piztu e ten prioridade se un módulo repite nome), logo
// os extras que sexan absolutos e existan de verdade, sen repetidos. Un
// file_root inexistente non é un erro para Salt, pero enche o log do master de
// avisos en cada petición do fileserver.
func raicesValidas(saltDir string, extras []string) []string {
	fóra := []string{saltDir}
	vistas := map[string]bool{saltDir: true}
	for _, e := range extras {
		e = strings.TrimSpace(e)
		if e == "" || vistas[e] || !filepath.IsAbs(e) {
			continue
		}
		if fi, err := os.Stat(e); err != nil || !fi.IsDir() {
			continue
		}
		vistas[e] = true
		fóra = append(fóra, e)
	}
	return fóra
}

// InstalarMinions instala/configura salt-minion en cada host en paralelo, por
// SSH con clave (a compartida da aula). Idempotente: se salt-minion xa está, non
// reinstala o paquete, só reescribe a configuración e reinicia o servizo.
func InstalarMinions(hosts []string, usuario, keyPath, masterAddr, contrasinal string, log Log) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	safe := func(h, l string) { mu.Lock(); log(h, l); mu.Unlock() }

	sem := make(chan struct{}, 6)
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			script := strings.NewReplacer(
				"@@MASTER@@", masterAddr,
				"@@ID@@", host,
			).Replace(minionScript)
			if err := sshkey.EjecutarSudoConClave(host, usuario, keyPath, contrasinal, script); err != nil {
				safe(host, "❌ "+err.Error())
				return
			}
			safe(host, "✅ salt-minion configurado (master="+masterAddr+")")
		}(h)
	}
	wg.Wait()
}

// DesinstalarMinions para e desinstala salt-minion en cada host en paralelo,
// por SSH con clave, antes de reinstalar (mesmo motivo que DesinstalarMaster:
// partir de cero evita arrastrar configuracións vellas). Idempotente e
// tolerante a fallos: un host que non responda non frea os demais, só queda
// rexistrado no log — a instalación posterior xa fallará ese host se segue sen
// responder.
func DesinstalarMinions(hosts []string, usuario, keyPath, contrasinal string, log Log) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	safe := func(h, l string) { mu.Lock(); log(h, l); mu.Unlock() }

	sem := make(chan struct{}, 6)
	for _, h := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := sshkey.EjecutarSudoConClave(host, usuario, keyPath, contrasinal, desinstalarMinionScript); err != nil {
				safe(host, "⚠️ non se puido limpar a instalación anterior: "+err.Error())
				return
			}
			safe(host, "🧹 salt-minion anterior eliminado")
		}(h)
	}
	wg.Wait()
}

// AceptarChaves acepta no master as chaves dos minions (só Linux). Agarda uns
// segundos a que os minions recén instalados contacten co master.
func AceptarChaves(contrasinal string, log Log) error {
	if runtime.GOOS == "darwin" {
		return ErrMasterNonSoportado
	}
	time.Sleep(8 * time.Second)
	return runLocalSudo(contrasinal, "salt-key -A -y", log)
}

// ErrAnsibleNonSoportado devólvese ao tentar instalar Ansible por apt fóra de Linux.
var ErrAnsibleNonSoportado = errors.New("instala Ansible con Homebrew en macOS")

// AnsibleInstalado di se este equipo xa ten o binario ansible-playbook.
func AnsibleInstalado() bool {
	_, err := exec.LookPath("ansible-playbook")
	return err == nil
}

// InstalarAnsible instala o paquete `ansible` con apt neste equipo (só Linux).
func InstalarAnsible(contrasinal string, log Log) error {
	if runtime.GOOS == "darwin" {
		return ErrAnsibleNonSoportado
	}
	return runLocalSudo(contrasinal, ansibleScript, log)
}

// ── Utilidades ───────────────────────────────────────────────────────────────

func usuarioActual() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "usuario"
}

// runLocalSudo executa `script` como root NESTE equipo con `sudo -S`, e vai
// enviando a saída liña a liña ao log.
func runLocalSudo(contrasinal, script string, log Log) error {
	cmd := exec.Command("sudo", "-S", "-k", "-p", "", "bash", "-s")
	cmd.Stdin = strings.NewReader(contrasinal + "\n" + script)
	w := &logWriter{log: log}
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// logWriter reenvía a saída dun proceso ao Log, liña a liña.
type logWriter struct {
	log Log
	buf []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		linha := strings.TrimRight(string(w.buf[:i]), "\r")
		if strings.TrimSpace(linha) != "" {
			w.log("", linha)
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// ── Scripts ──────────────────────────────────────────────────────────────────

// desinstalarMasterScript para e purga salt-master neste equipo antes de
// reinstalar (execútase como root). Non toca /etc/apt/keyrings nin o
// repositorio de SaltProject: volveríanse escribir igual en masterScript.
//
// Bórrase tamén /etc/salt/pki/master: "apt purge" leva o paquete e o usuario
// de sistema "salt" (post-purge), pero as chaves de /etc/salt/pki NON son
// conffiles e quedan no disco coa propiedade do UID vello. Ao reinstalar,
// apt crea un "salt" novo con outro UID e o master non pode ler as súas
// propias chaves → "PermissionError" ao arrincar (visto en produción). É
// máis seguro rexenerar as chaves de cero ca arrastrar esa propiedade
// desincronizada; os clientes xa aceptados volven aceptarse en AceptarChaves.
const desinstalarMasterScript = `set -e
systemctl stop salt-master 2>/dev/null || true
systemctl disable salt-master 2>/dev/null || true
apt-get purge -y -q salt-master 2>/dev/null || true
rm -rf /etc/salt/pki/master
echo "instalación anterior de salt-master eliminada (se había)"
`

// masterScript instala salt-master e a regra sudoers (execútase como root). O
// marcador @@USER@@ substitúese polo usuario actual.
const masterScript = `set -e
export DEBIAN_FRONTEND=noninteractive
mkdir -p /etc/apt/keyrings
if [ ! -f ` + keyringF + ` ]; then
  curl -fsSL ` + gpgKeyURL + ` | gpg --dearmor -o ` + keyringF + `
fi
curl -fsSL -o /etc/apt/sources.list.d/salt.sources ` + sourcesURL + `
apt-get update -q
# avahi: sen mDNS non resolve ningún nome .local, e o inventario da aula
# está cheo deles — falla o motor Ansible (SSH por nome), a comprobación
# de equipos acendidos e ata o enderezo do master que se lle dá aos
# minions. libnss-mdns é o que engancha avahi ao resolvedor do sistema:
# sen el, avahi está vivo pero getent/ssh seguen sen ver os .local.
apt-get install -y -q salt-master avahi-daemon avahi-utils libnss-mdns
systemctl enable --now avahi-daemon
cat > /etc/sudoers.d/piztu-salt <<'SUDO'
# Xerado por piztu — non editar á man.
@@USER@@ ALL=(root) NOPASSWD: /usr/bin/salt, /usr/bin/salt-cp
SUDO
chmod 440 /etc/sudoers.d/piztu-salt
if ! visudo -c -f /etc/sudoers.d/piztu-salt; then
  rm -f /etc/sudoers.d/piztu-salt
  echo "regra sudoers non válida; descartada" >&2
  exit 1
fi
systemctl enable --now salt-master
echo "salt-master instalado e activo"
`

// fileRootsScript escribe o file_roots do master nun drop-in propio de
// /etc/salt/master.d/ (non tocando /etc/salt/master, que é do paquete) e
// reinicia o servizo para que colla o cambio. @@ROOTS@@ substitúese pola lista
// de raíces xa formatada en YAML (ver ConfigurarFileRoots).
// autosignBloque é o anaco de fileRootsScript que activa o autosign, illado nunha
// constante para poder eliminalo cando non hai patróns (ver ConfigurarMaster).
const autosignBloque = `autosign_file: /etc/salt/autosign.conf
`

// descomentarAutosignMaster deixa activa a liña autosign_file en /etc/salt/master
// (o paquete tráea como "#autosign_file: /etc/salt/autosign.conf"); se non hai
// ningunha, engádea ao final. Idempotente: reescribir unha liña xa activa
// déixaa igual. É complementario ao drop-in, que segue mandando.
const descomentarAutosignMaster = `if grep -qE '^[[:space:]]*#?[[:space:]]*autosign_file:' /etc/salt/master; then
  sed -i -E 's;^[[:space:]]*#?[[:space:]]*autosign_file:.*;autosign_file: /etc/salt/autosign.conf;' /etc/salt/master
else
  echo 'autosign_file: /etc/salt/autosign.conf' >> /etc/salt/master
fi`

const fileRootsScript = `set -e
mkdir -p /etc/salt/master.d
cat > /etc/salt/master.d/piztu.conf <<'CFG'
# Xerado por piztu - non editar a man.
file_roots:
  base:
@@ROOTS@@
autosign_file: /etc/salt/autosign.conf
CFG
chmod 644 /etc/salt/master.d/piztu.conf
cat > /etc/salt/autosign.conf <<'CFG'
# Xerado por piztu - non editar a man.
@@AUTOSIGN@@
CFG
# 0644 root:root: o master corre como usuario "salt" e Salt IGNORA o
# autosign_file (cun aviso no log, sen fallar) se calquera pode escribir nel.
chmod 644 /etc/salt/autosign.conf
@@MASTER_AUTOSIGN@@
systemctl restart salt-master
echo "file_roots do master: @@RESUMO@@"
echo "autosign: @@AUTOSIGN_RESUMO@@"
`

// desinstalarMinionScript para e purga salt-minion nun cliente antes de
// reinstalar (execútase como root vía sudo). Non toca avahi nin o repositorio
// de SaltProject: minionScript xa os deixa (ou os volve deixar) coma cómpre.
//
// Bórrase tamén /etc/salt/pki/minion, mesmo motivo có master (ver
// desinstalarMasterScript): evita chaves orfas dun UID "salt" vello e, de
// paso, forza o minion a confiar de novo no master (necesario se o master
// tamén se reinstalou e ten identidade nova).
const desinstalarMinionScript = `set -e
systemctl stop salt-minion 2>/dev/null || true
systemctl disable salt-minion 2>/dev/null || true
apt-get purge -y -q salt-minion 2>/dev/null || true
rm -rf /etc/salt/pki/minion
echo "instalación anterior de salt-minion eliminada (se había)"
`

// minionScript instala (se falta) e configura salt-minion nun cliente. Os
// marcadores @@MASTER@@ e @@ID@@ substitúense polo enderezo do master e o nome
// do equipo no inventario (o id do minion ten que coincidir exactamente).
const minionScript = `set -e
export DEBIAN_FRONTEND=noninteractive
if ! command -v salt-minion >/dev/null 2>&1; then
  apt-get install -y -q curl gnupg
  mkdir -p /etc/apt/keyrings
  if [ ! -f ` + keyringF + ` ]; then
    curl -fsSL ` + gpgKeyURL + ` | gpg --dearmor -o ` + keyringF + `
  fi
  curl -fsSL -o /etc/apt/sources.list.d/salt.sources ` + sourcesURL + `
  apt-get update -q
  apt-get install -y -q salt-minion
fi
# O cliente tamén precisa avahi: publica o seu propio nome .local (é así
# como o servidor o atopa) e resolve o do master. Compróbase co paquete
# instalado, non con "command -v": avahi-daemon vive en /usr/sbin, que
# non adoita estar no PATH dunha shell non interactiva baixo sudo.
if ! dpkg-query -W -f='${Status}' avahi-daemon 2>/dev/null | grep -q "ok installed"; then
  apt-get install -y -q avahi-daemon avahi-utils libnss-mdns
fi
systemctl enable --now avahi-daemon
mkdir -p /etc/salt/minion.d
cat > /etc/salt/minion.d/piztu.conf <<CFG
master: @@MASTER@@
id: @@ID@@
CFG
systemctl enable salt-minion
systemctl restart salt-minion
`

// ansibleScript instala o paquete `ansible` do repositorio de Debian
// (execútase como root).
const ansibleScript = `set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q ansible
echo "ansible instalado"
`
