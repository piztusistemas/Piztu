package motor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"piztu/internal/config"
	"piztu/internal/inventory"
	"piztu/internal/sshkey"
	"piztu/internal/wol"
)

// MotorSalt executa contra os minions con `salt`/`salt-cp`.
// Porte de core/salt.py + core/motores/salt.py. Require salt-master no servidor
// e salt-minion (id == hostname) en cada equipo, coa chave aceptada.
type MotorSalt struct{ cfg *config.Config }

func (m *MotorSalt) Nome() string { return "salt" }

func (m *MotorSalt) GetEquipos() []string {
	// Mesmo inventario que Ansible para que o mapa sexa idéntico.
	return inventory.GetEquipos(m.cfg.HostsFile)
}

// ── Accións con nome (state.apply) ───────────────────────────────────────────

// comprobarMaster rexeita o motor onde non pode funcionar. `salt` fala cos
// sockets do master local, non por SSH: sen master, `sudo -n salt` falla cun
// "sudo: a password is required" que non lle di nada ao profesor. En macOS
// nunca hai master (ver saltsetup.ErrMasterNonSoportado).
func (m *MotorSalt) comprobarMaster() error {
	if !HaiSaltMaster() {
		return &ErrMotor{Msg: "o motor Salt precisa un salt-master, que non existe en macOS nin en Windows (usa o motor SSH)."}
	}
	if _, err := exec.LookPath("salt"); err != nil {
		return &ErrMotor{Msg: "o binario `salt` non está instalado neste equipo"}
	}
	return nil
}

func (m *MotorSalt) ExecutarAccion(nomeLogico string, hosts []string, cb Callbacks) error {
	if nomeLogico == "acender" || nomeLogico == "acenderAula" {
		// Wake-on-LAN sae do controlador, non do master: non precisa comprobación.
		// O frontend manda o nome xa resolto ("acenderAula", ver config.yaml →
		// ansible.playbooks.acender), non a clave lóxica — hai que recoñecer os
		// dous ou isto cae no state.apply de abaixo e falla porque non existe
		// (nin debe) un "acenderAula.sls": un equipo apagado non ten minion vivo.
		enviarWoL(m.cfg, hosts, cb)
		return nil
	}
	if err := m.comprobarMaster(); err != nil {
		return err
	}
	estadoSls := m.cfg.SaltStatePath(nomeLogico)
	if !existe(estadoSls) {
		return &ErrMotor{Msg: "Estado Salt non atopado: " + filepath.Base(estadoSls)}
	}
	estado := m.cfg.SaltEstadoNome(nomeLogico)
	m.runParaleloSalt(hosts, []string{"state.apply", estado}, cb)
	return nil
}

// ExecutarBloqueo executa o estado "bloqueo" pasando as opcións coma pillar
// (pillar='{"bloquear_internet":true,...}'), para que só bloquee o que estea
// marcado na configuración gardada (ver internal/db).
func (m *MotorSalt) ExecutarBloqueo(hosts []string, opcions map[string]bool, cb Callbacks) error {
	if err := m.comprobarMaster(); err != nil {
		return err
	}
	estadoSls := m.cfg.SaltStatePath("bloqueo")
	if !existe(estadoSls) {
		return &ErrMotor{Msg: "Estado Salt non atopado: " + filepath.Base(estadoSls)}
	}
	estado := m.cfg.SaltEstadoNome("bloqueo")
	m.runParaleloSalt(hosts, []string{"state.apply", estado, "pillar=" + extraVarsBloqueo(opcions)}, cb)
	return nil
}

// ExecutarAdhoc aplica `contido` (texto .sls xa renderizado, ex. por unha IA)
// directamente con state.template_str — NON se escribe un .sls temporal en
// cfg.SaltDir e chama a state.apply: o backend "roots" do fileserver de Salt
// cachea a listaxe de ficheiros (fileserver_list_cache_time, 30s por
// defecto) e pode non ver aínda un ficheiro creado hai un instante — Salt
// devolvería "No matching sls found" sen que saltOk o detecte coma erro
// (bug real, visto en produción co porte Python: a IA xeraba contido
// correcto pero non chegaba a aplicarse). state.template_str renderiza e
// aplica o contido pasado coma argumento, sen tocar o fileserver.
// Porte de core/motores/salt.py → executar_adhoc.
func (m *MotorSalt) ExecutarAdhoc(contido string, hosts []string, cb Callbacks) error {
	if err := m.comprobarMaster(); err != nil {
		return err
	}
	// Só se valida como YAML plano se non leva Jinja: un .sls con {% … %}
	// (ex. ramas por SO nunha aula mixta) renderízao Salt por minion antes
	// de parsealo — validalo aquí como YAML rexeitaríao en falso.
	if !pareceXinja(contido) {
		if err := yamlValido(contido); err != nil {
			return &ErrMotor{Msg: "Estado Salt (.sls) non válido: " + err.Error()}
		}
	}
	m.runParaleloSalt(hosts, []string{"state.template_str", contido}, cb)
	return nil
}

// enviarWoL emite o paquete máxico (Wake-on-LAN) a cada host en paralelo, de
// forma nativa (internal/wol), sen o binario `wakeonlan`. Un equipo apagado non
// ten minion Salt nin SSH vivo, así que "acender" nunca pode ser un state.apply
// nin un playbook remoto: é sempre broadcast desde o controlador. Compárteno os
// dous motores (Salt e Ansible).
func enviarWoL(cfg *config.Config, hosts []string, cb Callbacks) {
	vars := inventory.LerHostsConVars(cfg.HostsFile)
	var wg sync.WaitGroup
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			mac := macDe(vars[host])
			if mac == "" {
				cb.erro(host, "sen MAC no inventario para "+host+" (escanea a rede primeiro)")
				return
			}
			if err := wol.Enviar(mac); err != nil {
				cb.erro(host, err.Error())
				return
			}
			cb.ok(host, "paquete máxico enviado a "+mac)
		}(h)
	}
	wg.Wait()
}

// macDe extrae "mac=xx:xx:xx:xx:xx:xx" das vars dun host do inventario.
func macDe(varsHost string) string {
	for _, campo := range strings.Fields(varsHost) {
		if v, ok := strings.CutPrefix(campo, "mac="); ok {
			return v
		}
	}
	return ""
}

// ── Prácticas ────────────────────────────────────────────────────────────────
//
// EnviarFicheiros/LimparPracticas/RecollerPracticas van por SFTP directo (SSH),
// igual que o motor SSH — NON por salt-cp/cmd.run. salt-cp le o ficheiro
// enteiro en memoria, codificado en base64 (+33% de tamaño), e envíao como un
// único argumento de traballo por ZeroMQ: con ficheiros dunha ducia de MB en
// adiante isto adoita quedar colgado sen máis (o proceso `salt-cp` nunca
// remata, aínda co timeout `-t 120` — ese só cubría o caso en que SI remataba
// pero tarde). SFTP fai streaming real, sen cargar o ficheiro enteiro en
// memoria nin inflalo, así que non ten ese límite. A clave SSH xa a require
// o propio motor Salt (Paso 2, compartida con Ansible), así que non engade
// ningunha dependencia nova.

// usuario devolve o usuario alumno/SSH (o mesmo na aula), por defecto "usuario".
func (m *MotorSalt) usuario() string {
	if m.cfg.SSHUser != "" {
		return m.cfg.SSHUser
	}
	return "usuario"
}

// practicasWindows devolve, para o conxunto de hosts, cales son Windows. As
// prácticas envíanse por SFTP igual que en Linux; só cambia a resolución da
// ruta de destino no cliente (escritorio real vía PowerShell en vez do probe
// bash Escritorio/Desktop) — ver sshkey.rutasPracticas.
func practicasWindows(hostsFile string, hosts []string) map[string]bool {
	w := make(map[string]bool, len(hosts))
	for h, so := range SOsDeHosts(hostsFile, hosts) {
		w[h] = so == SOWindows
	}
	return w
}

func (m *MotorSalt) EnviarFicheiros(hosts []string, ficheiros []string, cb Callbacks) error {
	usuario, keyPath := m.usuario(), m.cfg.SSHKeyFile
	win := practicasWindows(m.cfg.HostsFile, hosts)
	correrEnParalelo(hosts, 6, cb, func(host string) (string, bool) {
		if err := sshkey.SubirPracticas(host, usuario, keyPath, m.cfg.RemotePracticas, ficheiros, win[host]); err != nil {
			return err.Error(), false
		}
		return "", true
	})
	return nil
}

func (m *MotorSalt) LimparPracticas(hosts []string, cb Callbacks) error {
	usuario, keyPath := m.usuario(), m.cfg.SSHKeyFile
	win := practicasWindows(m.cfg.HostsFile, hosts)
	correrEnParalelo(hosts, 6, cb, func(host string) (string, bool) {
		if err := sshkey.LimparPracticas(host, usuario, keyPath, m.cfg.RemotePracticas, win[host]); err != nil {
			return err.Error(), false
		}
		return "", true
	})
	return nil
}

func (m *MotorSalt) RecollerPracticas(hosts []string, cb Callbacks) error {
	usuario, keyPath := m.usuario(), m.cfg.SSHKeyFile
	win := practicasWindows(m.cfg.HostsFile, hosts)
	for _, h := range hosts {
		dir := filepath.Join(m.cfg.PracticasDir, h)
		if fis, err := os.ReadDir(dir); err == nil {
			for _, fi := range fis {
				if !fi.IsDir() {
					os.Remove(filepath.Join(dir, fi.Name()))
				}
			}
		}
	}
	correrEnParalelo(hosts, 6, cb, func(host string) (string, bool) {
		localDir := filepath.Join(m.cfg.PracticasDir, host)
		n, err := sshkey.RecollerPracticas(host, usuario, keyPath, m.cfg.RemotePracticas, localDir, win[host])
		if err != nil {
			return err.Error(), false
		}
		return fmt.Sprintf("%d ficheiro(s)", n), true
	})
	return nil
}

// ── Instalar Tao nos clientes Windows ────────────────────────────────────────

// psInstalarTaoWin: corre no minion (SYSTEM). Move o tao.exe subido por SFTP
// dende o temporal do usuario a C:\Program Files\Piztu e crea os atallos a
// nivel de máquina: menú Inicio de todos, Escritorio público, e ademais o
// Escritorio de cada perfil de usuario real (o público non sempre se ve na
// sesión do alumno se ten os iconos do sistema ocultos). @@TMP@@ substitúese
// pola ruta real do temporal.
//
// TEN QUE SER UNHA SOA LIÑA: `salt cmd.run ... shell=powershell` con Salt 3008
// en Windows NON executa un script de varias liñas (devolve retcode 0 e saída
// baleira, sen mover nada — e saltOk daríao coma éxito). Cun só comando de
// varias sentenzas separadas por ';' vai ben. O marcador final "Tao instalado
// en" úsao InstalarTaoClientesWindows para confirmar que de verdade correu.
const psInstalarTaoWin = `$ErrorActionPreference='Stop'; $dst='C:\Program Files\Piztu'; New-Item -ItemType Directory -Force -Path $dst | Out-Null; Move-Item -Force '@@TMP@@' (Join-Path $dst 'tao.exe'); $w=New-Object -ComObject WScript.Shell; $sis=@('Public','Default','Default User','All Users','WDAGUtilityAccount'); $lnks=New-Object System.Collections.ArrayList; [void]$lnks.Add((Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs\Tao.lnk')); [void]$lnks.Add('C:\Users\Public\Desktop\Tao.lnk'); Get-ChildItem 'C:\Users' -Directory -ErrorAction SilentlyContinue | Where-Object { $sis -notcontains $_.Name } | ForEach-Object { $d=Join-Path $_.FullName 'Desktop'; if (Test-Path $d) { [void]$lnks.Add((Join-Path $d 'Tao.lnk')) } }; foreach ($lnk in $lnks) { New-Item -ItemType Directory -Force -Path (Split-Path $lnk) | Out-Null; $s=$w.CreateShortcut($lnk); $s.TargetPath=(Join-Path $dst 'tao.exe'); $s.WorkingDirectory=$dst; $s.IconLocation=(Join-Path $dst 'tao.exe')+',0'; $s.Description='Tao - rexistro diario e informes asinados do alumnado'; $s.Save() }; Write-Output ('Tao instalado en '+(Join-Path $dst 'tao.exe'))`

// InstalarTaoClientesWindows instala Tao nos equipos Windows: sobe tao.exe por
// SFTP (como o usuario, sen elevación) a un temporal, e logo un cmd.run no
// minion (SYSTEM) móveo a C:\Program Files\Piztu e crea os atallos. Faino
// este motor —non o Ansible— porque só o minion coma SYSTEM pode escribir en
// Program Files, e o resto do soporte Windows de Piztu xa vai por Salt.
func (m *MotorSalt) InstalarTaoClientesWindows(hosts []string, taoExeSrc string, cb Callbacks) error {
	if err := m.comprobarMaster(); err != nil {
		return err
	}
	if _, err := os.Stat(taoExeSrc); err != nil {
		return &ErrMotor{Msg: "non se atopa tao.exe no servidor: " + err.Error()}
	}
	usuario, keyPath := m.usuario(), m.cfg.SSHKeyFile
	tmpWin := `C:\Users\` + usuario + `\AppData\Local\Temp\piztu_tao_setup.exe`
	tmpSFTP := "C:/Users/" + usuario + "/AppData/Local/Temp/piztu_tao_setup.exe"
	ps := strings.ReplaceAll(psInstalarTaoWin, "@@TMP@@", tmpWin)
	// Mesmo papel ca o tao-servidor.conf do playbook de Linux: a URL do File
	// Browser que Tao propón na pantalla de acceso. Tao léao ao lado do seu
	// executable (tao/servidor.go), aquí C:\Program Files\Piztu. Vai antes do
	// marcador final. ASCII e non UTF8: o UTF8 de PowerShell 5.1 mete BOM e
	// Tao non recoñecería a liña de comentario. SÓ comiñas simples (dobradas por dentro): unha comiña
	// dobre nun `cmd.run shell=powershell` de Salt en Windows rompe o paso do
	// argumento e o comando acaba baixo cmd.exe.
	if url := m.cfg.URLPublicaFileBrowser(); url != "" {
		lit := func(t string) string { return "'" + strings.ReplaceAll(t, "'", "''") + "'" }
		ps = strings.Replace(ps, "Write-Output ('Tao instalado en",
			"Set-Content -Encoding ASCII -Path (Join-Path $dst 'tao-servidor.conf') -Value ("+
				lit("# Xerado por Piztu ao instalar Tao. URL do servidor de ficheiros (File Browser).")+
				",("+lit(url)+")); Write-Output ('Tao instalado en", 1)
	}

	m.paraleloCustom(hosts, cb, func(h string) (string, int) {
		if err := sshkey.SubirFicheiro(h, usuario, keyPath, taoExeSrc, tmpSFTP); err != nil {
			return "SFTP tao.exe: " + err.Error(), 1
		}
		out, rc := runSaltT(h, 300, []string{"cmd.run", ps, "shell=powershell"})
		// saltOk daría por bo un cmd.run con saída baleira (retcode 0). O
		// script remata cun marcador explícito: se non está, non correu.
		if rc == 0 && !strings.Contains(out, "Tao instalado en") {
			return "o script non confirmou a instalación (saída: " + strings.TrimSpace(out) + ")", 1
		}
		return out, rc
	})
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// runSalt executa "salt" vía sudo: os sockets do master en
// /var/run/salt/master/ só son lexibles por root/usuario "salt" (é o propio
// modelo de seguridade de Salt), e piztu corre como sesión de escritorio
// normal. Require a regra sudoers NOPASSWD que instala o Paso 5 do
// instalador (ver installer/main.go: instalarSaltMasterServidor).
//
// Usa un timeout curto (30 s): abonda para grains/test.ping e evita que unha
// consulta a un minion apagado quede colgada. Para operacións longas
// (state.apply, state.template_str, instalación de paquetes) usa runSaltT.
func runSalt(host string, args []string) (string, int) {
	return runSaltT(host, 30, args)
}

// runSaltT é runSalt cun timeout explícito en segundos (`salt -t`). Unha
// instalación de programa (choco/apt/msi) pode levar varios minutos; co
// timeout por defecto de 5 s do master, `salt` devolvería "No return
// received" aínda que o minion siga traballando e remate ben.
func runSaltT(host string, segundos int, args []string) (string, int) {
	base := []string{"-n", "salt", "--out=json", "--static", "-t", strconv.Itoa(segundos), host}
	cmd := exec.Command("sudo", append(base, args...)...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	out, _ := cmd.CombinedOutput()
	rc := 0
	if cmd.ProcessState != nil {
		rc = cmd.ProcessState.ExitCode()
	}
	return string(out), rc
}

// saltOk decide se a execución nun host foi correcta (state.apply: todos os
// "result" True; cmd.run/test.ping: rc==0 e host presente sen False).
func saltOk(host, stdout string, rc int) bool {
	if rc != 0 {
		return false
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return false
	}
	ret, ok := data[host]
	if !ok {
		return false
	}
	// Cando o .sls non compila (ex.: YAML cun path de Windows con '\' dentro
	// dunha cadea entre comiñas dobres → "found unknown escape character") ou o
	// minion lanza unha excepción, Salt devolve `data[host]` coma unha CADEA co
	// traceback, non coma o mapa de estados. Sen isto, o `return true` do final
	// daría por boa unha execución que non correu nada.
	if s, ok := ret.(string); ok {
		low := strings.ToLower(s)
		for _, marca := range []string{"traceback (most recent call last)", "caused an exception", "rendererror", "rendering sls", "not a valid", "no minions matched"} {
			if strings.Contains(low, marca) {
				return false
			}
		}
	}
	if estados, ok := ret.(map[string]any); ok {
		houbo := false
		for _, v := range estados {
			if paso, ok := v.(map[string]any); ok {
				if r, existe := paso["result"]; existe {
					houbo = true
					if b, _ := r.(bool); !b {
						return false
					}
				}
			}
		}
		if houbo {
			return true
		}
	}
	// Sen bloques con "result": correcto se non é explícitamente False.
	if b, ok := ret.(bool); ok {
		return b
	}
	return true
}

func (m *MotorSalt) runParaleloSalt(hosts []string, args []string, cb Callbacks) {
	// 20 min por host: state.apply / state.template_str poden incluír a
	// instalación de paquetes, que non entra nos 30 s de runSalt.
	m.paraleloCustom(hosts, cb, func(h string) (string, int) {
		return runSaltT(h, 1200, args)
	})
}

// paraleloCustom executa fn(host)->(stdout,rc) por host en paralelo, con callbacks.
func (m *MotorSalt) paraleloCustom(hosts []string, cb Callbacks, fn func(string) (string, int)) {
	var wg sync.WaitGroup
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			out, rc := fn(host)
			if saltOk(host, out, rc) {
				cb.ok(host, out)
			} else {
				cb.erro(host, out)
			}
		}(h)
	}
	wg.Wait()
}
