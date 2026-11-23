// ssh_win.go: soporte de equipos Windows no motor SSH (aula Windows ou
// mixta). Os scripts de ssh.go son bash con `sudo`; nun Windows con OpenSSH
// Server execútase PowerShell no seu lugar (sshkey.EjecutarPowerShell), coa
// conta de ssh_user. As accións de administración só funcionan se esa conta é
// ADMINISTRADORA local; desde a 26.12.2 a conta da aula é estándar e o
// camiño recomendado para Windows é o motor Salt (ver psSoAdmin).
//
// Os scripts grandes (bloqueo, desbloqueo, aviso) son OS MESMOS .ps1 que os
// estados Salt mandan con `cmd.script salt://…` (recursos/salt/win_*.ps1):
// unha soa fonte para os dous motores. Corren coma administrador en vez de
// SYSTEM, que abonda para o que fan (schtasks /ru, firewall, ProgramData).
//
// Nome do ficheiro: NON "ssh_windows.go" — en Go o sufixo _windows.go
// compilaría isto SÓ cando o controlador é Windows, e isto ten que estar en
// calquera controlador (Linux, macOS ou Windows) que fale con clientes Windows.

package motor

import (
	"strings"
	"sync"

	"piztu/internal/sshkey"
	"piztu/recursos"
)

// psSoAdmin vai diante de cada script Windows que precisa permisos de
// administrador (todos menos psComprobarBloqueo, que só le). Desde a 26.12.2 a
// conta da aula en Windows é ESTÁNDAR e as accións van polo minion de Salt
// (SYSTEM); cunha conta estándar, os netsh/schtasks/ProgramData dos scripts
// fallarían en silencio (van a Out-Null) e a acción daríase por boa sen facer
// nada. Así o equipo responde co marcador e correrWindows explica a causa.
const psSoAdmin = "if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { 'PIZTU_SEN_ADMIN'; exit 3 }\n"

// marcaSenAdmin é a saída de psSoAdmin cando a conta non é administradora.
const marcaSenAdmin = "PIZTU_SEN_ADMIN"

// soAdmin antepón psSoAdmin a un script Windows ("" segue sendo "": sen
// equivalente).
func soAdmin(script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	return psSoAdmin + script
}

// erroWindows traduce a saída dun script fallido nun Windows a unha mensaxe
// para o profesorado.
func erroWindows(usuario, saida string) string {
	if strings.Contains(saida, marcaSenAdmin) {
		return "a conta '" + usuario + "' non é administradora neste equipo Windows (é o normal desde a 26.12.2): " +
			"os motores SSH e Ansible non poden facer esta acción en Windows. Usa o motor Salt, que a fai co minion coma SYSTEM " +
			"(ver clientes_windows/README.md)."
	}
	return saida
}

// Ordes curtas (cmd/PowerShell) para os equipos Windows. "" = sen equivalente.
const (
	psApagar    = "shutdown /s /t 0 /f\n"
	psReiniciar = "shutdown /r /t 0 /f\n"
	// Ver salt/durmirAula.sls: sen hibernación, SetSuspendState suspende.
	psDurmir           = "powercfg /h off; rundll32.exe powrprof.dll,SetSuspendState 0,1,0\n"
	psComprobarBloqueo = `if (Test-Path (Join-Path $env:ProgramData 'piztu_bloqueo.flag')) { 'BLOQUEADO:aviso' }
elseif (netsh advfirewall firewall show rule name=PIZTU_BLOQUEO 2>$null | Select-String -Quiet 'PIZTU_BLOQUEO') { 'BLOQUEADO:rede' }
else { 'LIBRE' }
`
)

// psScriptSalt le un dos win_*.ps1 embebidos; se non está (binario mal
// empaquetado) devolve "" e a acción falla cun erro claro en correrSO.
func psScriptSalt(nome string) string {
	s, err := recursos.ScriptSalt(nome)
	if err != nil {
		return ""
	}
	return s
}

// psBloqueo: porte da rama Windows de salt/bloqueoTotal.sls.
func psBloqueo(o map[string]bool) string {
	internet, ssh, son := o["internet"], o["ssh"], o["son"]
	rato, teclado := o["rato"], o["teclado"]

	var b strings.Builder
	b.WriteString("$ProgressPreference='SilentlyContinue'\n")
	if internet || ssh {
		b.WriteString("netsh advfirewall firewall delete rule name=PIZTU_BLOQUEO | Out-Null\n")
		b.WriteString("netsh advfirewall firewall delete rule name=PIZTU_BLOQUEO_SSH | Out-Null\n")
	}
	if internet {
		// Permite as redes privadas (RFC1918) para non cortar a propia aula.
		b.WriteString("netsh advfirewall firewall add rule name=PIZTU_BLOQUEO dir=out action=block " +
			"remoteip=1.0.0.0-9.255.255.255,11.0.0.0-172.15.255.255,172.32.0.0-192.167.255.255,192.169.0.0-223.255.255.255 | Out-Null\n")
	}
	if ssh {
		b.WriteString("netsh advfirewall firewall add rule name=PIZTU_BLOQUEO_SSH dir=out action=block protocol=TCP remoteport=22 | Out-Null\n")
	}
	if son || rato || teclado {
		b.WriteString(psScriptSalt("win_bloqueo.ps1"))
		b.WriteString("\n")
	}
	b.WriteString("'bloqueo aplicado'\n")
	return soAdmin(b.String())
}

// correrSO executa cada host co script do seu SO: `linux` (bash, como root,
// marcador @@USER@@) ou `windows` (PowerShell, como administrador). Se
// `windows` é "", os equipos Windows reciben un erro explicativo en vez de
// fallar cun bash inexistente.
func (m *MotorSSH) correrSO(hosts []string, linux, windows string, cb Callbacks) {
	sos := SOsDeHosts(m.cfg.HostsFile, hosts)
	var lin, win []string
	for _, h := range hosts {
		if sos[h] == SOWindows {
			win = append(win, h)
		} else {
			lin = append(lin, h)
		}
	}
	var wg sync.WaitGroup
	if len(lin) > 0 {
		wg.Add(1)
		go func() { defer wg.Done(); m.correr(lin, linux, cb) }()
	}
	if len(win) > 0 {
		wg.Add(1)
		go func() { defer wg.Done(); m.correrWindows(win, windows, cb) }()
	}
	wg.Wait()
}

func (m *MotorSSH) correrWindows(hosts []string, script string, cb Callbacks) {
	usuario, keyPath := m.usuario(), m.cfg.SSHKeyFile
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, h := range hosts {
		cb.inicio(h)
		if strings.TrimSpace(script) == "" {
			cb.erro(h, "esta acción aínda non ten equivalente Windows no motor SSH (proba co motor Salt)")
			continue
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := sshkey.EjecutarPowerShell(host, usuario, keyPath, script)
			if err != nil {
				cb.erro(host, erroWindows(usuario, err.Error()))
				return
			}
			cb.ok(host, out)
		}(h)
	}
	wg.Wait()
}
