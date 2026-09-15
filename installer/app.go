package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App é o núcleo do instalador: expón métodos chamables desde o frontend
// (bindings, xerados automaticamente por `wails build` en
// frontend/wailsjs/go/main/App.js) e emite eventos de progreso durante os
// pasos longos (Instalar/InstalarLamp), igual que fai piztu co resto de
// Piztu.
type App struct {
	ctx context.Context
	tr  *Translator

	// permisoDenegado: true cando main() non puido reexecutarse como root
	// (pkexec non dispoñible ou cancelado) en Linux. O frontend, ao arrincar,
	// comproba PermisoDenegado() e amosa só a pantalla de instrucións.
	permisoDenegado bool
}

func NewApp() *App {
	return &App{tr: NewTranslator(idiomaFallback)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ── Idioma ────────────────────────────────────────────────────────────────────

func (a *App) Idioma() string           { return a.tr.Idioma() }
func (a *App) SetIdioma(code string)    { a.tr.SetIdioma(code) }
func (a *App) Idiomas() []IdiomaInfo    { return idiomasDispo }
func (a *App) Traducions() map[string]string { return a.tr.All() }

// ── Estado xeral ──────────────────────────────────────────────────────────────

func (a *App) EsRoot() bool           { return os.Getuid() == 0 }
func (a *App) PermisoDenegado() bool  { return a.permisoDenegado }
func (a *App) DestDir() string        { return destDir }
func (a *App) Version() string        { return strings.TrimSpace(versionInstalador) }

// InstalacionDesc/CompletadoDesc: textos estáticos que levan o cartafol de
// instalación substituído en Go (equivalente ao fmt.Sprintf(...) que facía a
// versión Fyne antes de pasarllo ao widget).
func (a *App) InstalacionDesc() string { return fmt.Sprintf(a.tr.T("inst.desc"), destDir) }
func (a *App) CompletadoDesc() string  { return fmt.Sprintf(a.tr.T("fin.desc"), destDir) }

// ── Paso "Instalación" ──────────────────────────────────────────────────────

// Instalar executa a instalación completa en segundo plano e emite cada
// liña por "inst_log"; ao rematar emite "inst_completo" con {ok, erro}.
func (a *App) Instalar() {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "inst_log", strings.Trim(line, "\n"))
		}
		if err := a.doInstall(emit); err != nil {
			erro := fmt.Sprintf(a.tr.T("inst.erro"), err.Error())
			emit(erro)
			wruntime.EventsEmit(a.ctx, "inst_completo", map[string]any{"ok": false, "erro": erro})
			return
		}
		emit(a.tr.T("inst.ok"))
		wruntime.EventsEmit(a.ctx, "inst_completo", map[string]any{"ok": true, "erro": ""})
	}()
}

func (a *App) doInstall(log func(string)) error {
	log(fmt.Sprintf(a.tr.T("inst.log.extraendo"), destDir))
	if err := extractFiles(log); err != nil {
		return fmt.Errorf("extraendo ficheiros: %w", err)
	}
	if err := fixConfigBaseDir(); err != nil {
		log("⚠️ non se puido axustar base_dir en config.yaml: " + err.Error())
	}
	if err := fixConfigIdioma(a.tr.Idioma()); err != nil {
		log("⚠️ non se puido axustar idioma en config.yaml: " + err.Error())
	}
	if err := fixDestDirOwnership(); err != nil {
		log("⚠️ non se puido axustar o propietario de " + destDir + ": " + err.Error())
	}
	log(a.tr.T("inst.log.extraidos"))

	log(a.tr.T("inst.log.apt"))
	if err := runWithLog(exec.Command("apt-get", "update", "-q"), log); err != nil {
		log(a.tr.T("inst.log.aptfail"))
	}

	// python3-wakeonlan non existe en Debian 12 (a funcionalidade está en wakeonlan)
	// sshpass é necesario para que Ansible use autenticación por contrasinal
	// curl e gnupg son necesarios para engadir o repositorio APT de SaltProject
	pkgs := []string{
		"ansible", "sshpass", "wakeonlan",
		"alsa-utils",     // arecord, para o monitor de ruído (integrado en piztu)
		"openssh-client", // conexión SSH cos clientes
		"curl", "gnupg",  // repositorio e chave GPG de SaltProject
		// mDNS: o inventario da aula usa nomes .local; libnss-mdns engancha
		// avahi ao resolvedor do sistema (sen el, getent/ssh non ven os .local).
		"avahi-daemon", "avahi-utils", "libnss-mdns",
		// salt-master NON vai aquí: Debian non o trae nos repos oficiais,
		// instálase á parte máis abaixo tras engadir o repositorio de SaltProject.
		"python3-flask", "python3-flask-socketio", "python3-paramiko", "python3-yaml",
		"python3-requests", "python3-packaging",
		// Dependencias de app.py (Flask) — xa non as usa Xesta (agora é a
		// súa propia app Wails e fala con piztu/internal/api vía
		// piztuclient); quedan só para uso manual/local de app.py.
	}
	log(a.tr.T("inst.log.paquetes"))
	cmd := exec.Command("apt-get", append([]string{"install", "-y", "-q"}, pkgs...)...)
	if err := runWithLog(cmd, log); err != nil {
		// Se falla en bloque (p.ex. un paquete non está no repositorio), instalar
		// un a un para non abortar todo por unha soa dependencia ausente.
		log(a.tr.T("inst.log.paquetes_individual"))
		for _, p := range pkgs {
			c := exec.Command("apt-get", "install", "-y", "-q", p)
			if err := runWithLog(c, log); err != nil {
				log(fmt.Sprintf(a.tr.T("inst.log.paquete_fallou"), p))
			}
		}
	}
	log(a.tr.T("inst.log.paquetesok"))

	// Salt: motor de execución por defecto de Piztu (ver config.yaml → motor).
	// Só se activa o master neste servidor; os minions dos equipos cliente
	// instálanse individualmente desde Piztu ao engadir cada equipo.
	log(a.tr.T("inst.log.salt"))
	if err := instalarSaltMasterServidor(log); err != nil {
		log(fmt.Sprintf(a.tr.T("inst.log.saltfail"), err.Error()))
	} else {
		log(a.tr.T("inst.log.saltok"))
	}

	log(a.tr.T("inst.log.permisos"))
	for _, s := range []string{"lanzar_piztu.sh", "distribuirClave.sh", "xerar_inventario.sh", "piztu", "tao-helper", "historial-daemon"} {
		os.Chmod(destDir+"/"+s, 0755)
	}
	// tao-helper (non piztu!) execútase sempre co grupo www-data
	// aplicado (bit setgid), para poder ler dixitalizacion/sesions/
	// (permisos 750 www-data:www-data) sen depender de que o usuario peche
	// sesión e volva entrar tras engadilo a ese grupo (os grupos
	// suplementarios cárganse só no login). setgid vai nun binario auxiliar
	// SEN interface gráfica, nunca en piztu: GTK (usado internamente por
	// webkit2gtk/Wails) recusa inicializarse en calquera proceso
	// setuid/setgid ("Refusing to initialize GTK+"), así que marcar o
	// propio piztu rompería a aplicación enteira.
	if g, err := user.LookupGroup("www-data"); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			helperPath := destDir + "/tao-helper"
			if err := os.Chown(helperPath, -1, gid); err == nil {
				os.Chmod(helperPath, 0755|os.ModeSetgid)
			}
		}
	}
	log(a.tr.T("inst.log.permisosok"))

	return nil
}

// ── Paso "LAMP" (opcional) ───────────────────────────────────────────────────

// InstalarLamp instala Apache+MariaDB+PHP neste servidor co contrasinal de
// MySQL introducido polo usuario (nunca valores fixos no código: vai como
// variable ao playbook nun ficheiro temporal). É opcional e independente de
// File Browser: quen xa teña un LAMP instalado previamente pode omitir este
// paso e pasar directamente ao seguinte. Emite progreso por "lamp_log" e
// remata con "lamp_completo" {ok, erro}.
func (a *App) InstalarLamp(dbPass string) {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "lamp_log", strings.Trim(line, "\n"))
		}
		falla := func(erro string) {
			emit(erro)
			wruntime.EventsEmit(a.ctx, "lamp_completo", map[string]any{"ok": false, "erro": erro})
		}

		playbook := destDir + "/playbooks/instalar_lamp.yaml"
		if _, err := os.Stat(playbook); err != nil {
			falla(fmt.Sprintf(a.tr.T("lamp.erroplaybook"), playbook))
			return
		}

		// Variable nun ficheiro temporal (non vai na liña de comandos)
		varsFile, err := os.CreateTemp("", "piztu_lamp_*.yml")
		if err != nil {
			falla(fmt.Sprintf(a.tr.T("lamp.errovars"), err.Error()))
			return
		}
		defer os.Remove(varsFile.Name())
		fmt.Fprintf(varsFile, "mysql_root_pass: %q\n", dbPass)
		varsFile.Close()
		os.Chmod(varsFile.Name(), 0600)

		emit(a.tr.T("lamp.log.iniciando"))
		cmd := exec.Command("ansible-playbook", playbook, "-e", "@"+varsFile.Name())
		cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
		if err := runWithLog(cmd, emit); err != nil {
			falla(fmt.Sprintf(a.tr.T("lamp.erro"), err.Error()))
			return
		}
		emit(a.tr.T("lamp.ok"))
		wruntime.EventsEmit(a.ctx, "lamp_completo", map[string]any{"ok": true, "erro": ""})
	}()
}

// ── Paso "File Browser" (opcional) ───────────────────────────────────────────

// InstalarFileBrowser instala File Browser neste servidor cos contrasinais
// introducidos polo usuario. Require un servidor web xa activo en
// /var/www/html (instalado co paso "LAMP" ou previamente por outra vía) e o
// usuario/grupo www-data (de fábrica en calquera Debian); é independente do
// paso "LAMP" e pode omitirse igualmente. Emite progreso por
// "filebrowser_log" e remata con "filebrowser_completo" {ok, erro}.
func (a *App) InstalarFileBrowser(fbUser, fbPass string) {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "filebrowser_log", strings.Trim(line, "\n"))
		}
		falla := func(erro string) {
			emit(erro)
			wruntime.EventsEmit(a.ctx, "filebrowser_completo", map[string]any{"ok": false, "erro": erro})
		}

		playbook := destDir + "/playbooks/instalar_filebrowser.yaml"
		if _, err := os.Stat(playbook); err != nil {
			falla(fmt.Sprintf(a.tr.T("filebrowser.erroplaybook"), playbook))
			return
		}

		// Variables nun ficheiro temporal (non van na liña de comandos)
		varsFile, err := os.CreateTemp("", "piztu_filebrowser_*.yml")
		if err != nil {
			falla(fmt.Sprintf(a.tr.T("filebrowser.errovars"), err.Error()))
			return
		}
		defer os.Remove(varsFile.Name())
		// piztu_os_user: engádese ao grupo www-data para que Piztu poida ler
		// o cartafol compartido (dixitalizacion/, permisos 750 www-data:www-data)
		// onde Tao publica quen está a usar cada equipo.
		realUser, _ := getRealUser()
		fmt.Fprintf(varsFile,
			"filebrowser_admin_user: %q\n"+
				"filebrowser_admin_pass: %q\n"+
				"piztu_os_user: %q\n",
			fbUser, fbPass, realUser)
		varsFile.Close()
		os.Chmod(varsFile.Name(), 0600)

		emit(a.tr.T("filebrowser.log.iniciando"))
		cmd := exec.Command("ansible-playbook", playbook, "-e", "@"+varsFile.Name())
		cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
		if err := runWithLog(cmd, emit); err != nil {
			falla(fmt.Sprintf(a.tr.T("filebrowser.erro"), err.Error()))
			return
		}
		emit(fmt.Sprintf(a.tr.T("filebrowser.ok"), fbUser))
		emit(fmt.Sprintf(a.tr.T("filebrowser.log.grupo"), realUser))

		// Historial de sesións (quen usou cada equipo e cando): precisa
		// as mesmas credenciais de File Browser que se acaban de crear.
		if err := fixConfigFilebrowser(fbUser, fbPass); err != nil {
			emit(fmt.Sprintf(a.tr.T("filebrowser.errofilebrowsercfg"), err.Error()))
		}
		if err := instalarServizoHistorial(realUser); err != nil {
			emit(fmt.Sprintf(a.tr.T("filebrowser.historial.erro"), err.Error()))
		} else {
			emit(a.tr.T("filebrowser.historial.ok"))
		}

		wruntime.EventsEmit(a.ctx, "filebrowser_completo", map[string]any{"ok": true, "erro": ""})
	}()
}

// ── Paso "Completado" ────────────────────────────────────────────────────────

// CrearAccesosDirectos escribe a icona e os accesos directos (.desktop) do
// sistema e do escritorio.
func (a *App) CrearAccesosDirectos() error {
	return createShortcuts()
}

// ── Zona de perigo: desinstalación ──────────────────────────────────────────
//
// Tres accións independentes entre si (cada unha comproba o seu propio
// playbook/estado e non asume que as outras se executasen antes):
//   - DesinstalarLamp: só Apache/MariaDB/PHP.
//   - DesinstalarFileBrowser: só File Browser.
//   - DesinstalarPiztu: Piztu completo (/opt/piztu, salt-master, servizo de
//     historial, sudoers, clave SSH, accesos directos). Non toca LAMP nin
//     File Browser: desinstálanse á parte cos botóns anteriores se se quere.

// DesinstalarLamp executa playbooks/desinstalar_lamp.yaml. Emite progreso por
// "desinstlamp_log" e remata con "desinstlamp_completo" {ok, erro}.
func (a *App) DesinstalarLamp() {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "desinstlamp_log", strings.Trim(line, "\n"))
		}
		falla := func(erro string) {
			emit(erro)
			wruntime.EventsEmit(a.ctx, "desinstlamp_completo", map[string]any{"ok": false, "erro": erro})
		}

		playbook, limpar, err := playbookDesinstalador("desinstalar_lamp.yaml")
		if err != nil {
			falla(fmt.Sprintf(a.tr.T("desinst.lamp.erroplaybook"), err.Error()))
			return
		}
		defer limpar()

		emit(a.tr.T("desinst.lamp.log.iniciando"))
		cmd := exec.Command("ansible-playbook", playbook)
		cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
		if err := runWithLog(cmd, emit); err != nil {
			falla(fmt.Sprintf(a.tr.T("desinst.lamp.erro"), err.Error()))
			return
		}
		emit(a.tr.T("desinst.lamp.ok"))
		wruntime.EventsEmit(a.ctx, "desinstlamp_completo", map[string]any{"ok": true, "erro": ""})
	}()
}

// DesinstalarFileBrowser executa playbooks/desinstalar_filebrowser.yaml (que
// agora borra tamén /var/www/html/arquivos: todos os datos do servidor) e, a
// maiores, elimina os datos locais de Tao do usuario que lanza o instalador
// (~/.config/dixitalizacion/: director.json, admin.json co contrasinal en
// claro, a CA, os certificados, perfís e a auditoría da API). Emite progreso
// por "desinstfb_log" e remata con "desinstfb_completo" {ok, erro}.
func (a *App) DesinstalarFileBrowser() {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "desinstfb_log", strings.Trim(line, "\n"))
		}
		falla := func(erro string) {
			emit(erro)
			wruntime.EventsEmit(a.ctx, "desinstfb_completo", map[string]any{"ok": false, "erro": erro})
		}

		playbook, limpar, err := playbookDesinstalador("desinstalar_filebrowser.yaml")
		if err != nil {
			falla(fmt.Sprintf(a.tr.T("desinst.fb.erroplaybook"), err.Error()))
			return
		}
		defer limpar()

		emit(a.tr.T("desinst.fb.log.iniciando"))
		cmd := exec.Command("ansible-playbook", playbook)
		cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
		if err := runWithLog(cmd, emit); err != nil {
			falla(fmt.Sprintf(a.tr.T("desinst.fb.erro"), err.Error()))
			return
		}

		// Datos locais de Tao (por-usuario, non os toca o playbook): só se borra
		// o do usuario que lanzou o instalador; se outros usuarios abriron Tao,
		// hai que limpar o seu $HOME/.config/dixitalizacion á man.
		if _, home := getRealUser(); home != "" {
			taoData := filepath.Join(home, ".config", "dixitalizacion")
			if _, err := os.Stat(taoData); err == nil {
				emit(fmt.Sprintf(a.tr.T("desinst.fb.datoslocais"), taoData))
				if err := os.RemoveAll(taoData); err != nil {
					emit("⚠️  " + err.Error())
				}
			}
		}

		emit(a.tr.T("desinst.fb.ok"))
		wruntime.EventsEmit(a.ctx, "desinstfb_completo", map[string]any{"ok": true, "erro": ""})
	}()
}

// DesinstalarPiztu elimina Piztu por completo deste servidor: salt-master
// (paquete, repositorio APT e chaves/certificados en /etc/salt/pki), a regra
// sudoers que lle permite executalo sen contrasinal, o servizo de historial
// de sesións, a clave SSH usada para os equipos cliente (se está configurada
// en config.yaml), os datos locais de Tao do usuario que lanza o instalador
// (~/.config/dixitalizacion/), os accesos directos/icona e por último
// /opt/piztu. NON toca LAMP nin File Browser (ver DesinstalarLamp/
// DesinstalarFileBrowser).
// Emite progreso por "desinstpiztu_log" e remata con "desinstpiztu_completo"
// {ok, erro}.
func (a *App) DesinstalarPiztu() {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "desinstpiztu_log", strings.Trim(line, "\n"))
		}
		if err := desinstalarPiztuCompleto(emit); err != nil {
			erro := fmt.Sprintf(a.tr.T("desinst.piztu.erro"), err.Error())
			emit(erro)
			wruntime.EventsEmit(a.ctx, "desinstpiztu_completo", map[string]any{"ok": false, "erro": erro})
			return
		}
		emit(a.tr.T("desinst.piztu.ok"))
		wruntime.EventsEmit(a.ctx, "desinstpiztu_completo", map[string]any{"ok": true, "erro": ""})
	}()
}
