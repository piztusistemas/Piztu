// MotorSSH controla os equipos por SSH directo (golang.org/x/crypto/ssh),
// sen Ansible nin Salt: cada acción é un script bash executado como root no
// cliente (`sudo -n`, aproveitando que a aula xa ten NOPASSWD). É o motor por
// defecto en macOS, onde Salt non pode ser master e Ansible require instalación
// aparte. Os scripts son o porte fiel dos estados salt/*.sls.
package motor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"piztu/internal/config"
	"piztu/internal/inventory"
	"piztu/internal/sshkey"
)

// MotorSSH executa contra os equipos con SSH+clave.
type MotorSSH struct{ cfg *config.Config }

func (m *MotorSSH) Nome() string { return "ssh" }

func (m *MotorSSH) GetEquipos() []string {
	return inventory.GetEquipos(m.cfg.HostsFile)
}

// usuario devolve o usuario alumno/SSH (o mesmo na aula), por defecto "usuario".
func (m *MotorSSH) usuario() string {
	if m.cfg.SSHUser != "" {
		return m.cfg.SSHUser
	}
	return "usuario"
}

// ExecutarAdhoc non ten equivalente no motor SSH: o contido que xera un
// módulo coma Xesta é sempre YAML de Ansible ou .sls de Salt, non un script de
// shell — non hai forma xenérica de traducilo. Non existe tampouco no porte
// Python (core/motores/ non ten "nativo.py": ese motor é só de piztu/macOS).
func (m *MotorSSH) ExecutarAdhoc(contido string, hosts []string, cb Callbacks) error {
	return &ErrMotor{Msg: "o motor SSH non soporta executar contido xerado en tempo real (require Ansible ou Salt)"}
}

func (m *MotorSSH) ExecutarAccion(accion string, hosts []string, cb Callbacks) error {
	switch accion {
	case "acender", "acenderAula":
		// Equipo apagado: só cabe Wake-on-LAN (compartido cos outros motores).
		enviarWoL(m.cfg, hosts, cb)
		return nil
	case "durmir", "durmirAula":
		m.correr(hosts, scriptDurmir, cb)
	case "apagar", "apagar_aula":
		m.correr(hosts, scriptApagar, cb)
	case "reiniciar", "reiniciarAula":
		m.correr(hosts, scriptReiniciar, cb)
	case "liberar", "desbloquear", "desbloquearAula":
		m.correr(hosts, scriptDesbloquear, cb)
	case "aviso_ruido", "avisoRuido":
		m.correr(hosts, scriptAvisoRuido, cb)
	case "comprobar_bloqueo":
		m.correr(hosts, scriptComprobarBloqueo, cb)
	default:
		// Accións que traen os módulos: o script que declaran execútase igual ca
		// os internos, incluída a substitución de @@USER@@. Os outros motores non
		// precisan nada aquí: resolven por ruta e cfg xa lles devolve a do módulo.
		a, ok := m.cfg.AccionModulo(accion)
		if !ok || a.Script == "" {
			return &ErrMotor{Msg: "acción non soportada no motor SSH: " + accion}
		}
		guion, err := os.ReadFile(a.Script)
		if err != nil {
			return &ErrMotor{Msg: "non se puido ler o script do módulo: " + err.Error()}
		}
		m.correr(hosts, string(guion), cb)
	}
	return nil
}

func (m *MotorSSH) ExecutarBloqueo(hosts []string, opcions map[string]bool, cb Callbacks) error {
	m.correr(hosts, scriptBloqueo(opcions), cb)
	return nil
}

// correr executa `scriptTmpl` (con marcador @@USER@@) en todos os hosts en
// paralelo, como root, e reporta por callbacks.
func (m *MotorSSH) correr(hosts []string, scriptTmpl string, cb Callbacks) {
	usuario := m.usuario()
	script := strings.ReplaceAll(scriptTmpl, "@@USER@@", usuario)
	keyPath := m.cfg.SSHKeyFile

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := sshkey.EjecutarComoRoot(host, usuario, keyPath, script)
			if err != nil {
				cb.erro(host, err.Error())
				return
			}
			cb.ok(host, out)
		}(h)
	}
	wg.Wait()
}

// ── Ficheiros (SFTP nativo) ──────────────────────────────────────────────────

// correrFicheiros executa `fn` por host en paralelo, reportando por callbacks.
func (m *MotorSSH) correrFicheiros(hosts []string, cb Callbacks, fn func(host string) error) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := fn(host); err != nil {
				cb.erro(host, err.Error())
				return
			}
			cb.ok(host, "")
		}(h)
	}
	wg.Wait()
}

func (m *MotorSSH) EnviarFicheiros(hosts []string, ficheiros []string, cb Callbacks) error {
	m.correrFicheiros(hosts, cb, func(host string) error {
		return sshkey.SubirPracticas(host, m.usuario(), m.cfg.SSHKeyFile, m.cfg.RemotePracticas, ficheiros)
	})
	return nil
}

func (m *MotorSSH) RecollerPracticas(hosts []string, cb Callbacks) error {
	m.correrFicheiros(hosts, cb, func(host string) error {
		localDir := filepath.Join(m.cfg.PracticasDir, host)
		// Limpeza local previa (mesmo layout ca o motor Ansible).
		if fis, err := os.ReadDir(localDir); err == nil {
			for _, fi := range fis {
				if !fi.IsDir() {
					os.Remove(filepath.Join(localDir, fi.Name()))
				}
			}
		}
		_, err := sshkey.RecollerPracticas(host, m.usuario(), m.cfg.SSHKeyFile, m.cfg.RemotePracticas, localDir)
		return err
	})
	return nil
}

func (m *MotorSSH) LimparPracticas(hosts []string, cb Callbacks) error {
	m.correrFicheiros(hosts, cb, func(host string) error {
		return sshkey.LimparPracticas(host, m.usuario(), m.cfg.SSHKeyFile, m.cfg.RemotePracticas)
	})
	return nil
}

// ── Scripts (porte fiel de salt/*.sls; @@USER@@ = usuario alumno) ─────────────

const scriptDurmir = "systemctl suspend\n"

const scriptApagar = "/sbin/shutdown -h now\n"

const scriptReiniciar = "/sbin/shutdown -r now\n"

// scriptDesbloquear reactiva rato/teclado, quita o aviso, restaura son e rede.
// Porte de salt/desbloquearAula.sls.
const scriptDesbloquear = `export DISPLAY=:0
export XAUTHORITY=/home/@@USER@@/.Xauthority
USER_ID=$(id -u @@USER@@)
export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus
XAUTH_REAL=$(ps -eo cmd | grep "[X]org" | grep -oP '(?<=-auth )\S+')
XAUTH_REAL=${XAUTH_REAL:-/home/@@USER@@/.Xauthority}

if command -v xinput > /dev/null; then
  for id in $(sudo -u @@USER@@ DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput list --id-only); do
    case "$id" in ''|*[!0-9]*) continue ;; esac
    if [ "$id" -gt 4 ]; then
      sudo -u @@USER@@ DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput enable "$id" || true
    fi
  done
fi

if [ -f /tmp/piztu_bloqueo.pid ]; then
  kill "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null || true
  rm -f /tmp/piztu_bloqueo.pid
fi
pkill -u @@USER@@ -x zenity 2>/dev/null || true

sudo -u @@USER@@ LC_ALL=C.UTF-8 DISPLAY=:0 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus \
  zenity --info --title="CONTROL DE AULA" \
  --text="<span size='xx-large' weight='bold'>EQUIPO DESBLOQUEADO</span>\n\nPodes continuar co teu traballo." \
  --timeout=5 --no-wrap &

amixer set Master unmute 2>/dev/null || true

for p in /usr/sbin/nft /sbin/nft /usr/bin/nft /bin/nft; do
  if [ -x "$p" ]; then "$p" flush chain inet piztu_bloqueo output 2>/dev/null || true; break; fi
done
for bin in iptables ip6tables; do
  for p in "/usr/sbin/$bin" "/sbin/$bin" "/usr/bin/$bin" "/bin/$bin"; do
    if [ -x "$p" ]; then "$p" -F PIZTU_BLOQUEO 2>/dev/null || true; break; fi
  done
done
`

// scriptBloqueo constrúe o script de bloqueo segundo as opcións marcadas.
// Porte de salt/bloqueoTotal.sls (os `{% if %}` de Jinja resólvense aquí en Go).
func scriptBloqueo(o map[string]bool) string {
	rato, teclado := o["rato"], o["teclado"]
	internet, ssh, son := o["internet"], o["ssh"], o["son"]

	var b strings.Builder
	b.WriteString(`export DISPLAY=:0
export XAUTHORITY=/home/@@USER@@/.Xauthority
USER_ID=$(id -u @@USER@@)
export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus
XAUTH_REAL=$(ps -eo cmd | grep "[X]org" | grep -oP '(?<=-auth )\S+')
XAUTH_REAL=${XAUTH_REAL:-/home/@@USER@@/.Xauthority}
`)

	if rato || teclado {
		b.WriteString(`if command -v xinput > /dev/null; then
  LISTAXINPUT=$(sudo -u @@USER@@ DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput list)
`)
		if rato {
			b.WriteString(`  echo "$LISTAXINPUT" | grep -i pointer | grep -oP 'id=\K[0-9]+' | while read -r id; do
    if [ "$id" -gt 4 ]; then
      sudo -u @@USER@@ DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput disable "$id" || true
    fi
  done
`)
		}
		if teclado {
			b.WriteString(`  echo "$LISTAXINPUT" | grep -i keyboard | grep -oP 'id=\K[0-9]+' | while read -r id; do
    if [ "$id" -gt 4 ]; then
      sudo -u @@USER@@ DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput disable "$id" || true
    fi
  done
`)
		}
		b.WriteString("fi\n\n")

		// Aviso persistente en segundo plano (só se rato/teclado bloqueados).
		b.WriteString(`if [ -f /tmp/piztu_bloqueo.pid ]; then
  kill "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null || true
fi
pkill -u @@USER@@ -x zenity 2>/dev/null || true
(
  while true; do
    sudo -u @@USER@@ LC_ALL=C.UTF-8 DISPLAY=:0 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus \
      zenity --warning --title="CONTROL DE AULA" \
      --text="<span size='xx-large' weight='bold'>EQUIPO BLOQUEADO</span>\n\nPresta atención ás indicacións do profesor." \
      --no-wrap
    sleep 1
  done
) < /dev/null > /dev/null 2>&1 &
echo $! > /tmp/piztu_bloqueo.pid
`)
	}

	if son {
		b.WriteString("amixer set Master mute 2>/dev/null || true\n")
	}

	if internet || ssh {
		b.WriteString("\n")
		b.WriteString(`localizar() {
  for p in "/usr/sbin/$1" "/sbin/$1" "/usr/bin/$1" "/bin/$1"; do
    if [ -x "$p" ]; then echo "$p"; return 0; fi
  done
  return 1
}
NFT=$(localizar nft) || NFT=""
if [ -n "$NFT" ]; then
  "$NFT" add table inet piztu_bloqueo 2>/dev/null
  "$NFT" 'add chain inet piztu_bloqueo output { type filter hook output priority 0 ; }' 2>/dev/null
  "$NFT" flush chain inet piztu_bloqueo output
  "$NFT" add rule inet piztu_bloqueo output oifname "lo" accept
  "$NFT" add rule inet piztu_bloqueo output ct state established,related accept
`)
		if ssh {
			b.WriteString(`  "$NFT" add rule inet piztu_bloqueo output tcp dport 22 drop
`)
		}
		if internet {
			b.WriteString(`  "$NFT" add rule inet piztu_bloqueo output ip daddr 10.0.0.0/8 accept
  "$NFT" add rule inet piztu_bloqueo output ip daddr 172.16.0.0/12 accept
  "$NFT" add rule inet piztu_bloqueo output ip daddr 192.168.0.0/16 accept
  "$NFT" add rule inet piztu_bloqueo output ip6 daddr fe80::/10 accept
  "$NFT" add rule inet piztu_bloqueo output ip6 daddr fc00::/7 accept
  "$NFT" add rule inet piztu_bloqueo output drop
`)
		}
		b.WriteString(`  exit 0
fi
IPT=$(localizar iptables) || IPT=""
IPT6=$(localizar ip6tables) || IPT6=""
if [ -z "$IPT" ] && [ -z "$IPT6" ]; then
  echo "Nin nft, nin iptables, nin ip6tables atopados; bloqueo de rede omitido"
  exit 0
fi
if [ -n "$IPT" ]; then
  "$IPT" -N PIZTU_BLOQUEO 2>/dev/null
  "$IPT" -F PIZTU_BLOQUEO
  "$IPT" -C OUTPUT -j PIZTU_BLOQUEO 2>/dev/null || "$IPT" -I OUTPUT -j PIZTU_BLOQUEO
  "$IPT" -A PIZTU_BLOQUEO -o lo -j RETURN
  "$IPT" -A PIZTU_BLOQUEO -m state --state ESTABLISHED,RELATED -j RETURN
`)
		if ssh {
			b.WriteString(`  "$IPT" -A PIZTU_BLOQUEO -p tcp --dport 22 -j DROP
`)
		}
		if internet {
			b.WriteString(`  "$IPT" -A PIZTU_BLOQUEO -d 10.0.0.0/8 -j RETURN
  "$IPT" -A PIZTU_BLOQUEO -d 172.16.0.0/12 -j RETURN
  "$IPT" -A PIZTU_BLOQUEO -d 192.168.0.0/16 -j RETURN
  "$IPT" -A PIZTU_BLOQUEO -j DROP
`)
		}
		b.WriteString(`fi
if [ -n "$IPT6" ]; then
  "$IPT6" -N PIZTU_BLOQUEO 2>/dev/null
  "$IPT6" -F PIZTU_BLOQUEO
  "$IPT6" -C OUTPUT -j PIZTU_BLOQUEO 2>/dev/null || "$IPT6" -I OUTPUT -j PIZTU_BLOQUEO
  "$IPT6" -A PIZTU_BLOQUEO -o lo -j RETURN
  "$IPT6" -A PIZTU_BLOQUEO -m state --state ESTABLISHED,RELATED -j RETURN
`)
		if ssh {
			b.WriteString(`  "$IPT6" -A PIZTU_BLOQUEO -p tcp --dport 22 -j DROP
`)
		}
		if internet {
			b.WriteString(`  "$IPT6" -A PIZTU_BLOQUEO -d fe80::/10 -j RETURN
  "$IPT6" -A PIZTU_BLOQUEO -d fc00::/7 -j RETURN
  "$IPT6" -A PIZTU_BLOQUEO -j DROP
`)
		}
		b.WriteString("fi\n")
	}

	return b.String()
}

// scriptComprobarBloqueo informa se o equipo ten un bloqueo activo.
// Porte de salt/comprobar_bloqueo.sls. A saída ("BLOQUEADO:…"/"LIBRE") vai ao log.
const scriptComprobarBloqueo = `if [ -f /tmp/piztu_bloqueo.pid ] && kill -0 "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null; then
  echo "BLOQUEADO:pid"
  exit 0
fi
for proc in xtrlock i3lock vlock gnome-screensaver light-locker xscreensaver dm-tool zenity; do
  if pgrep -x "$proc" > /dev/null 2>&1; then
    echo "BLOQUEADO:$proc"
    exit 0
  fi
done
echo "LIBRE"
`

// scriptAvisoRuido amosa un aviso emerxente de ruído excesivo (navegador en modo
// app, ou zenity de reserva). Porte de salt/avisoRuido.sls.
const scriptAvisoRuido = `export DISPLAY=:0
export XAUTHORITY=/home/@@USER@@/.Xauthority

cat > /tmp/aviso_ruido.html << 'HTMLEOF'
<!DOCTYPE html>
<html lang="gl">
<head>
<meta charset="UTF-8">
<meta http-equiv="refresh" content="6;url=about:blank">
<title>Aviso</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { background: #1a1a2e; display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100vh; font-family: 'Segoe UI', sans-serif; overflow: hidden; }
.tarxeta { background: #16213e; border: 2px solid #e53e3e; border-radius: 16px; padding: 40px 60px; text-align: center; box-shadow: 0 0 40px rgba(229,62,62,0.4); max-width: 520px; }
.icona { font-size: 64px; margin-bottom: 16px; }
.titulo { color: #e53e3e; font-size: 26px; font-weight: bold; margin-bottom: 12px; text-transform: uppercase; letter-spacing: 2px; }
.mensaxe { color: #e2e8f0; font-size: 16px; line-height: 1.6; margin-bottom: 20px; }
.aviso { color: #f6ad55; font-size: 13px; font-weight: bold; }
</style>
</head>
<body>
<div class="tarxeta">
<div class="icona">🔊</div>
<div class="titulo">⚠️ Demasiado Ruído</div>
<div class="mensaxe">Baixade o volume da aula.<br>Se o ruído continúa, os equipos <strong>bloquearanse automaticamente</strong>.</div>
<div class="aviso">Esta xanela pecharase en 6 segundos</div>
</div>
<script>setTimeout(function(){ window.close(); }, 6000);</script>
</body>
</html>
HTMLEOF

if command -v chromium-browser &>/dev/null; then
  sudo -u @@USER@@ chromium-browser --app=file:///tmp/aviso_ruido.html --window-size=600,400 --window-position=660,340 --disable-extensions --no-first-run --noerrdialogs &
elif command -v chromium &>/dev/null; then
  sudo -u @@USER@@ chromium --app=file:///tmp/aviso_ruido.html --window-size=600,400 --window-position=660,340 --disable-extensions --no-first-run --noerrdialogs &
elif command -v firefox &>/dev/null; then
  sudo -u @@USER@@ firefox --new-window file:///tmp/aviso_ruido.html &
else
  sudo -u @@USER@@ LC_ALL=C.UTF-8 zenity --info --title="Piztu.org — Aviso de Ruído" --text="⚠️ DEMASIADO RUÍDO NA AULA\n\nBaixade o volume ou os equipos bloquearanse." --timeout=6 &
fi
`
