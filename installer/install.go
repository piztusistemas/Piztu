package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed files
var piztuFiles embed.FS

//go:embed assets/logo.png
var logoPNG []byte

// destDir: mesmo cartafol para servidor único ou para instalacións en rede;
// Piztu (app.py/piztu) e o seu propio asistente xa asumen /opt/piztu.
const destDir = "/opt/piztu"

// getRealUser devolve o usuario e home de quen lanzou o instalador (non root)
func getRealUser() (string, string) {
	if uid := os.Getenv("PKEXEC_UID"); uid != "" {
		if u, err := user.LookupId(uid); err == nil {
			return u.Username, u.HomeDir
		}
	}
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			return u.Username, u.HomeDir
		}
	}
	if u, err := user.Current(); err == nil {
		return u.Username, u.HomeDir
	}
	return "usuario", "/home/usuario"
}

// fixConfigBaseDir garante que config.yaml aponta a destDir como base_dir.
// O config.yaml empaquetado tráeo do proxecto orixe (onde se compilou o
// instalador), non do cartafol de instalación final; sen isto, piztu
// podería ler hosts/piztu.db doutra instalación. Idempotente: corrixe tamén
// un config.yaml preservado dunha instalación previa que quedase mal.
func fixConfigBaseDir() error {
	cfgPath := filepath.Join(destDir, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "base_dir:") {
			lines[i] = "base_dir: \"" + destDir + "\""
			found = true
			break
		}
	}
	if !found {
		lines = append([]string{"base_dir: \"" + destDir + "\""}, lines...)
	}
	return os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")), 0644)
}

// fixConfigIdioma escribe en config.yaml o idioma escollido para a propia
// interface do instalador (gl/es/en/pt), para que Piztu e Tao arrinquen xa
// nesa lingua sen ter que editar config.yaml á man despois.
func fixConfigIdioma(idioma string) error {
	cfgPath := filepath.Join(destDir, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "idioma:") {
			lines[i] = "idioma: \"" + idioma + "\""
			found = true
			break
		}
	}
	if !found {
		lines = append([]string{"idioma: \"" + idioma + "\""}, lines...)
	}
	return os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")), 0644)
}

// fixDestDirOwnership pasa destDir ao usuario real: o instalador corre como
// root (sudo), pero piztu execútase despois como sesión de escritorio
// normal e precisa escribir alí (piztu.db, umbral.txt, tmp/, practicas/…).
func fixDestDirOwnership() error {
	realUser, _ := getRealUser()
	u, err := user.Lookup(realUser)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return filepath.WalkDir(destDir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}

// instalarSaltMasterServidor engade o repositorio APT de SaltProject
// (Debian 12 non trae salt-master nos repos oficiais) e instala/activa
// salt-master neste servidor.
func instalarSaltMasterServidor(log func(string)) error {
	const gpgKeyURL = "https://packages.broadcom.com/artifactory/api/security/keypair/SaltProjectKey/public"
	const sourcesURL = "https://github.com/saltstack/salt-install-guide/releases/latest/download/salt.sources"
	const keyringPath = "/etc/apt/keyrings/salt-archive-keyring.pgp"

	if err := os.MkdirAll("/etc/apt/keyrings", 0755); err != nil {
		return fmt.Errorf("creando /etc/apt/keyrings: %w", err)
	}

	if _, err := os.Stat(keyringPath); err != nil {
		curl := exec.Command("curl", "-fsSL", gpgKeyURL)
		gpg := exec.Command("gpg", "--dearmor", "-o", keyringPath)
		pipe, err := curl.StdoutPipe()
		if err != nil {
			return err
		}
		gpg.Stdin = pipe
		if err := gpg.Start(); err != nil {
			return fmt.Errorf("gpg --dearmor: %w", err)
		}
		if err := curl.Run(); err != nil {
			return fmt.Errorf("descargando chave GPG: %w", err)
		}
		if err := gpg.Wait(); err != nil {
			return fmt.Errorf("gpg --dearmor: %w", err)
		}
	}

	if err := runWithLog(exec.Command("curl", "-fsSL", "-o", "/etc/apt/sources.list.d/salt.sources", sourcesURL), log); err != nil {
		return fmt.Errorf("descargando repositorio APT: %w", err)
	}

	if err := runWithLog(exec.Command("apt-get", "update", "-q"), log); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}

	if err := runWithLog(exec.Command("apt-get", "install", "-y", "-q", "salt-master"), log); err != nil {
		return fmt.Errorf("instalando salt-master: %w", err)
	}

	// mDNS: o inventario da aula está cheo de nomes .local; sen avahi +
	// libnss-mdns enganchado ao resolvedor, nin o motor Ansible (SSH por
	// nome), nin a comprobación de equipos acendidos, nin o enderezo do
	// master que se lles dá aos minions resolven. Instálase co resto de
	// paquetes en doInstall(); aquí só nos aseguramos de que estea activo.
	exec.Command("systemctl", "enable", "--now", "avahi-daemon").Run()

	if err := permitirSaltSenSudo(); err != nil {
		return fmt.Errorf("configurando permisos de salt: %w", err)
	}

	if err := configurarSaltMasterPiztu(log); err != nil {
		return fmt.Errorf("configurando salt-master para Piztu: %w", err)
	}

	if err := runWithLog(exec.Command("systemctl", "enable", "--now", "salt-master"), log); err != nil {
		return err
	}
	// Reinicia para que colla o file_roots + autosign que se acaba de escribir
	// (enable --now non recarga se o servizo xa estaba activo).
	return runWithLog(exec.Command("systemctl", "restart", "salt-master"), log)
}

// configurarSaltMasterPiztu deixa o salt-master do servidor listo para Piztu no
// momento da instalación —o mesmo que fai piztu en ⚙ Aula
// (internal/saltsetup.ConfigurarMaster)— para que un servidor recén instalado
// non quede a medias:
//
//   - /etc/salt/master.d/piztu.conf co file_roots que apunta aos estados de
//     Piztu (destDir/salt). Sen isto, TODO state.apply falla con "No matching
//     sls found for '<estado>' in env 'base'" aínda que os minions respondan.
//   - autosign_file activo: no drop-in e, ademais, descomentado en
//     /etc/salt/master (o paquete tráeo como "#autosign_file: ...").
//   - /etc/salt/autosign.conf creado se non existe (0644 root:root). Os patróns
//     (o prefixo da aula + '*') engádeos piztu ao xerar a aula; no instalador
//     aínda non se coñecen.
func configurarSaltMasterPiztu(log func(string)) error {
	if err := os.MkdirAll("/etc/salt/master.d", 0755); err != nil {
		return err
	}
	dropIn := "# Xerado polo instalador de Piztu — non editar á man.\n" +
		"file_roots:\n" +
		"  base:\n" +
		"    - " + destDir + "/salt\n" +
		"autosign_file: /etc/salt/autosign.conf\n"
	if err := os.WriteFile("/etc/salt/master.d/piztu.conf", []byte(dropIn), 0644); err != nil {
		return err
	}

	// Só se crea se falta: non pisar patróns que piztu xa puidese escribir
	// nunha aula anterior.
	if _, err := os.Stat("/etc/salt/autosign.conf"); os.IsNotExist(err) {
		cabeceira := "# Xerado por Piztu — un patrón por liña (ex.: tux*).\n"
		if err := os.WriteFile("/etc/salt/autosign.conf", []byte(cabeceira), 0644); err != nil {
			return err
		}
	}

	// Descomentar (ou engadir) a liña autosign_file de /etc/salt/master.
	if data, err := os.ReadFile("/etc/salt/master"); err == nil {
		lines := strings.Split(string(data), "\n")
		achada := false
		for i, l := range lines {
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
			if strings.HasPrefix(t, "autosign_file:") {
				lines[i] = "autosign_file: /etc/salt/autosign.conf"
				achada = true
			}
		}
		if !achada {
			lines = append(lines, "autosign_file: /etc/salt/autosign.conf")
		}
		if err := os.WriteFile("/etc/salt/master", []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return err
		}
	}

	log("🧂 salt-master configurado para Piztu (file_roots → " + destDir + "/salt, autosign activo)")
	return nil
}

// permitirSaltSenSudo engade unha regra sudoers NOPASSWD limitada a
// /usr/bin/salt e /usr/bin/salt-cp para o usuario real (non root): son os
// dous binarios que piztu executa para controlar os minions, e o propio
// modelo de seguridade de Salt esixe ser root para falar cos sockets do
// master en /var/run/salt/master/. piztu corre como sesión de escritorio
// normal, así que sen isto calquera acción con motor Salt fallaría con
// "AuthenticationError" (non é un problema de rede nin de chaves).
func permitirSaltSenSudo() error {
	realUser, _ := getRealUser()
	contido := fmt.Sprintf(
		"# Xerado polo instalador de Piztu — non editar á man.\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/salt, /usr/bin/salt-cp\n",
		realUser,
	)
	ruta := "/etc/sudoers.d/piztu-salt"
	if err := os.WriteFile(ruta, []byte(contido), 0440); err != nil {
		return err
	}
	// Validar antes de deixala activa: unha regra mal formada podería
	// romper "sudo" no sistema enteiro.
	if err := exec.Command("visudo", "-c", "-f", ruta).Run(); err != nil {
		os.Remove(ruta)
		return fmt.Errorf("regra sudoers non válida (descartada): %w", err)
	}
	return nil
}

// fixConfigFilebrowser garda en config.yaml as credenciais de File Browser que
// acaba de crear o paso LAMP, para que o servizo de historial de sesións (ver
// instalarServizoHistorial) poida autenticarse na súa API. Se config.yaml xa
// tiña unha sección "filebrowser:" (instalación nova, molde xa actualizado),
// substitúe as dúas liñas; se non a tiña (actualización dunha instalación
// anterior a esta función), engade a sección enteira ao final.
func fixConfigFilebrowser(fbUser, fbPass string) error {
	cfgPath := filepath.Join(destDir, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	// Evitar YAML inválido se o contrasinal contén comiñas dobres.
	escapar := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }

	lines := strings.Split(string(data), "\n")
	tenSeccion, achouUsuario, achouContrasinal := false, false, false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "filebrowser:" {
			tenSeccion = true
		}
		if strings.HasPrefix(t, "admin_usuario:") {
			lines[i] = "  admin_usuario: \"" + escapar(fbUser) + "\""
			achouUsuario = true
		}
		if strings.HasPrefix(t, "admin_contrasinal:") {
			lines[i] = "  admin_contrasinal: \"" + escapar(fbPass) + "\""
			achouContrasinal = true
		}
	}
	if tenSeccion && achouUsuario && achouContrasinal {
		return os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	// Sección ausente ou incompleta (config.yaml doutra instalación anterior):
	// engadir unha sección completa e autocontida ao final do ficheiro.
	bloque := fmt.Sprintf(
		"\nfilebrowser:\n"+
			"  url: \"http://127.0.0.1:8080\"\n"+
			"  admin_usuario: \"%s\"\n"+
			"  admin_contrasinal: \"%s\"\n"+
			"  historial_dir: \"dixitalizacion/historial\"\n"+
			"  confirmar_seg: 300\n",
		escapar(fbUser), escapar(fbPass),
	)
	novo := strings.Join(lines, "\n") + bloque
	return os.WriteFile(cfgPath, []byte(novo), 0644)
}

// instalarServizoHistorial copia historial-daemon (xa extraído en destDir polo
// paso de Instalación) e o crea coma servizo systemd sempre activo, para que
// o historial de sesións siga completándose aínda que ninguén teña Piztu
// aberto. Espello, en Go, do mesmo paso feito á man en
// systemd/piztu-historial.service.
func instalarServizoHistorial(realUser string) error {
	binPath := filepath.Join(destDir, "historial-daemon")
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("non se atopa %s (o paso de Instalación non o extraeu?)", binPath)
	}
	os.Chmod(binPath, 0755)

	unidade := fmt.Sprintf(
		"[Unit]\n"+
			"Description=Piztu — historial de sesións (quen usou cada equipo e cando)\n"+
			"After=network.target\n\n"+
			"[Service]\n"+
			"User=%s\n"+
			"Group=%s\n"+
			"WorkingDirectory=%s\n"+
			"ExecStart=%s\n"+
			"Restart=on-failure\n\n"+
			"[Install]\n"+
			"WantedBy=multi-user.target\n",
		realUser, realUser, destDir, binPath,
	)
	const unidadePath = "/etc/systemd/system/piztu-historial.service"
	if err := os.WriteFile(unidadePath, []byte(unidade), 0644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "enable", "--now", "piztu-historial").Run(); err != nil {
		return err
	}
	// "enable --now" non fai nada se o servizo xa estaba activo: seguiría a
	// executar o binario vello (tras a substitución por rename, o proceso
	// mantén aberto o inodo anterior). Un restart explícito faino coller a
	// versión recén instalada.
	return exec.Command("systemctl", "restart", "piztu-historial").Run()
}

// ── Utilidades ────────────────────────────────────────────────────────────────

type logWriter struct{ fn func(string) }

func (w *logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.fn(line)
		}
	}
	return len(p), nil
}

func runWithLog(cmd *exec.Cmd, log func(string)) error {
	lw := &logWriter{fn: log}
	cmd.Stdout = lw
	cmd.Stderr = lw
	return cmd.Run()
}

// playbookDesinstalador devolve unha ruta executable ao playbook de
// desinstalación `nome` (ex.: "desinstalar_filebrowser.yaml"). Prefire a copia
// instalada en /opt/piztu/playbooks; se non está (caso típico: xa se
// desinstalou Piztu, que borra /opt/piztu enteiro, e as tres desinstalacións
// son independentes entre si — ver app.go), extrae a copia embebida no
// instalador a un ficheiro temporal. Hai que chamar a función devolta ao
// rematar: borra o temporal (no-op se se usou a copia do disco).
func playbookDesinstalador(nome string) (string, func(), error) {
	noop := func() {}
	noDisco := filepath.Join(destDir, "playbooks", nome)
	if _, err := os.Stat(noDisco); err == nil {
		return noDisco, noop, nil
	}
	data, err := piztuFiles.ReadFile("files/playbooks/" + nome)
	if err != nil {
		return "", noop, fmt.Errorf("o playbook %q non está nin instalado nin embebido: %w", nome, err)
	}
	tmp, err := os.CreateTemp("", "piztu-desinst-*.yaml")
	if err != nil {
		return "", noop, err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", noop, err
	}
	tmp.Close()
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

func extractFiles(log func(string)) error {
	const root = "files"
	return fs.WalkDir(piztuFiles, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Ruta relativa dentro do destino (quita o prefixo "files/")
		rel := strings.TrimPrefix(path, root+"/")
		if rel == root || rel == "" {
			return nil
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		// Conservar o que pertence á INSTALACIÓN e non ao paquete:
		//   config.yaml — ten ssh_key_file e os axustes do usuario.
		//   hosts       — é o inventario real do centro; o paquete só trae un
		//                 modelo baleiro (ver o target `files` do Makefile), así
		//                 que reinstalar por riba non lle debe deixar a aula sen
		//                 inventario.
		if rel == "config.yaml" || rel == "hosts" {
			if _, err := os.Stat(target); err == nil {
				log("  " + rel + " (conservado, xa existe)")
				return nil
			}
		}
		log("  " + rel)
		data, err := piztuFiles.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return escribirSubstituíndo(target, data, 0644)
	})
}

// escribirSubstituíndo escribe data en target aínda que target sexa un
// executable en uso. os.WriteFile abre o destino para escritura e iso falla
// con ETXTBSY ("text file busy") se o ficheiro é un binario en execución —
// pasaba ao reinstalar sobre unha instalación viva: historial-daemon corre
// como servizo systemd permanente, e piztu/tao-helper poden estar abertos.
// A saída é escribir a carón e renomear: rename só troca a entrada do
// directorio (o inodo vello segue vivo para quen o teña aberto) e ademais é
// atómico, así que nunca queda un binario a medias se algo falla no medio.
func escribirSubstituíndo(target string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".novo-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // non-op se o rename funcionou

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp crea con 0600; hai que poñer os permisos finais á man.
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

// createShortcuts escribe a icona e os accesos directos (.desktop) do
// sistema e do escritorio do usuario real.
func createShortcuts() error {
	iconPath := "/usr/share/pixmaps/piztu.png"
	if err := os.WriteFile(iconPath, logoPNG, 0644); err != nil {
		return fmt.Errorf("escribindo icona: %w", err)
	}

	desktop := strings.Join([]string{
		"[Desktop Entry]",
		"Version=1.0",
		"Type=Application",
		"Name=Piztu",
		"Name[gl_ES]=Piztu",
		"Comment=Sistema de xestión de aula de informática",
		"Comment[gl_ES]=Sistema de xestión de aula de informática",
		"Exec=" + filepath.Join(destDir, "lanzar_piztu.sh"),
		"Icon=" + iconPath,
		"Terminal=false",
		"StartupNotify=true",
		"Categories=Education;",
		"Keywords=aula;clase;ansible;piztu;",
		"",
	}, "\n")

	appPath := "/usr/share/applications/piztu.desktop"
	if err := os.WriteFile(appPath, []byte(desktop), 0644); err != nil {
		return fmt.Errorf("escribindo .desktop do sistema: %w", err)
	}

	_, home := getRealUser()
	for _, name := range []string{"Escritorio", "Escriptorio", "Desktop"} {
		d := filepath.Join(home, name)
		if _, err := os.Stat(d); err == nil {
			dst := filepath.Join(d, "piztu.desktop")
			os.WriteFile(dst, []byte(desktop), 0755)
			exec.Command("gio", "set", dst, "metadata::trusted", "true").Run()
			break
		}
	}

	exec.Command("update-desktop-database", "/usr/share/applications").Run()
	return nil
}

// ── Desinstalación completa ──────────────────────────────────────────────────

// expandirTil converte un "~/..." na ruta real do usuario que lanzou o
// instalador (non a de root: isto corre baixo pkexec). config.yaml admite o til
// en ssh_key_file — piztu/internal/config faino tamén ao cargalo.
func expandirTil(ruta string) string {
	if !strings.HasPrefix(ruta, "~") {
		return ruta
	}
	_, home := getRealUser()
	if home == "" {
		return ruta
	}
	return filepath.Join(home, strings.TrimPrefix(ruta, "~"))
}

// readConfigValue devolve o valor dunha clave de primeiro nivel de
// config.yaml (formato "clave: \"valor\"" ou "clave: valor"), ou "" se non
// existe. Mesmo parseo lineal a man ca fixConfigBaseDir/fixConfigIdioma: o
// resto do proxecto tampouco usa unha libraría YAML aquí.
func readConfigValue(clave string) string {
	data, err := os.ReadFile(filepath.Join(destDir, "config.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, clave+":") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(t, clave+":"))
		// Quitar o comentario inline: config.yaml documenta a maioría das
		// claves á dereita do valor ("ssh_key_file: \"...\"   # ruta á clave"),
		// e sen isto o valor devolto arrastraba o comentario enteiro.
		if strings.HasPrefix(v, `"`) {
			if i := strings.Index(v[1:], `"`); i >= 0 {
				v = v[:i+2]
			}
		} else if i := strings.Index(v, "#"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		return strings.Trim(v, `"`)
	}
	return ""
}

// desinstalarSaltMaster reverte instalarSaltMasterServidor: para e desactiva
// salt-master, elimina o paquete e o repositorio APT de SaltProject, e borra
// /etc/salt (inclúe pki/, coas chaves/certificados dos minions) e a caché e
// logs de salt.
func desinstalarSaltMaster(log func(string)) error {
	exec.Command("systemctl", "disable", "--now", "salt-master").Run()

	if err := runWithLog(exec.Command("apt-get", "remove", "-y", "-q", "--purge", "salt-master"), log); err != nil {
		log("⚠️  non se puido purgar o paquete salt-master: " + err.Error())
	}
	runWithLog(exec.Command("apt-get", "autoremove", "-y", "-q", "--purge"), log)

	for _, p := range []string{
		"/etc/salt",         // inclúe pki/master (chaves e certificados dos minions)
		"/var/cache/salt",
		"/var/log/salt",
		"/etc/apt/sources.list.d/salt.sources",
		"/etc/apt/keyrings/salt-archive-keyring.pgp",
	} {
		if err := os.RemoveAll(p); err != nil {
			log("⚠️  non se puido eliminar " + p + ": " + err.Error())
		}
	}
	return nil
}

// desinstalarAccesosDirectos reverte createShortcuts: icona, .desktop do
// sistema e do escritorio do usuario real.
func desinstalarAccesosDirectos() {
	os.Remove("/usr/share/pixmaps/piztu.png")
	os.Remove("/usr/share/applications/piztu.desktop")

	_, home := getRealUser()
	for _, name := range []string{"Escritorio", "Escriptorio", "Desktop"} {
		os.Remove(filepath.Join(home, name, "piztu.desktop"))
	}
	exec.Command("update-desktop-database", "/usr/share/applications").Run()
}

// desinstalarPiztuCompleto elimina Piztu por completo deste servidor: /opt/piztu,
// salt-master, o servizo de historial, a regra sudoers, a clave SSH, os accesos
// directos e os datos locais de Tao do usuario que lanza o instalador
// (~/.config/dixitalizacion/). Non toca LAMP nin File Browser: teñen os seus
// propios desinstaladores (playbooks/desinstalar_lamp.yaml e
// desinstalar_filebrowser.yaml).
func desinstalarPiztuCompleto(log func(string)) error {
	log("🩺 Deténdo o servizo de historial de sesións…")
	exec.Command("systemctl", "disable", "--now", "piztu-historial").Run()
	if err := os.Remove("/etc/systemd/system/piztu-historial.service"); err != nil && !os.IsNotExist(err) {
		log("⚠️  non se puido eliminar o servizo piztu-historial: " + err.Error())
	}
	exec.Command("systemctl", "daemon-reload").Run()

	log("🔑 Eliminando a regra sudoers de Salt…")
	if err := os.Remove("/etc/sudoers.d/piztu-salt"); err != nil && !os.IsNotExist(err) {
		log("⚠️  non se puido eliminar /etc/sudoers.d/piztu-salt: " + err.Error())
	}

	log("🧂 Desinstalando salt-master e as súas chaves/certificados…")
	if err := desinstalarSaltMaster(log); err != nil {
		log("⚠️  " + err.Error())
	}

	// A clave SSH lese de config.yaml ANTES de borrar destDir máis abaixo.
	if chave := expandirTil(readConfigValue("ssh_key_file")); chave != "" {
		log("🔐 Eliminando a clave SSH configurada (" + chave + ")…")
		os.Remove(chave)
		os.Remove(chave + ".pub")
	}

	// Datos locais de Tao (por-usuario, fóra de destDir): ~/.config/dixitalizacion/
	// — director.json, admin.json (co contrasinal de File Browser en claro), a CA e
	// os certificados de cliente, perfís e a auditoría da API. Ningún outro paso o
	// toca. Só se borra o do usuario que lanzou o instalador; se outros usuarios
	// abriron Tao, hai que limpar o seu $HOME/.config/dixitalizacion á man.
	if _, home := getRealUser(); home != "" {
		taoData := filepath.Join(home, ".config", "dixitalizacion")
		if _, err := os.Stat(taoData); err == nil {
			log("🧭 Eliminando os datos locais de Tao (" + taoData + ")…")
			if err := os.RemoveAll(taoData); err != nil {
				log("⚠️  non se puido eliminar " + taoData + ": " + err.Error())
			}
		}
	}

	log("🖥️  Eliminando accesos directos e icona…")
	desinstalarAccesosDirectos()

	log("📁 Eliminando " + destDir + "…")
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("eliminando %s: %w", destDir, err)
	}

	return nil
}
