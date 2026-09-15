package motor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"piztu/internal/config"
	"piztu/internal/inventory"
)

// MotorAnsible executa contra os equipos con ansible-playbook sobre SSH.
// Porte de core/ansible.py + core/motores/ansible.py.
type MotorAnsible struct{ cfg *config.Config }

func (m *MotorAnsible) Nome() string { return "ansible" }

func (m *MotorAnsible) GetEquipos() []string {
	return inventory.GetEquipos(m.cfg.HostsFile)
}

// ── Accións con nome (acender, bloqueo…) ─────────────────────────────────────

// comprobarAnsible verifica que `ansible-playbook` estea instalado. En macOS non
// vén de serie; con este aviso o profesor sabe que instalar (ou usar o Nativo).
func (m *MotorAnsible) comprobarAnsible() error {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		return &ErrMotor{Msg: "ansible-playbook non atopado. Instala Ansible (macOS: brew install ansible) ou usa o motor SSH."}
	}
	return nil
}

func (m *MotorAnsible) ExecutarAccion(nomeLogico string, hosts []string, cb Callbacks) error {
	// "acender" nunca é un playbook remoto (o equipo está apagado): é sempre
	// Wake-on-LAN nativo desde o controlador, igual que no motor Salt. O
	// frontend manda o nome xa resolto ("acenderAula"), non a clave lóxica —
	// hai que recoñecer os dous (por agora isto queda "tapado" porque tamén
	// existe un playbooks/acenderAula.yaml real, pero é o mesmo bo por
	// consistencia cos outros motores: WoL non debe depender de que ese
	// ficheiro siga existindo).
	if nomeLogico == "acender" || nomeLogico == "acenderAula" {
		enviarWoL(m.cfg, hosts, cb)
		return nil
	}
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	playbook := m.cfg.PlaybookPath(nomeLogico)
	if !existe(playbook) {
		playbook = filepath.Join(m.cfg.PlaybooksDir, nomeLogico+".yaml")
	}
	if !existe(playbook) {
		return &ErrMotor{Msg: "Playbook non atopado: " + nomeLogico + ".yaml"}
	}
	inv, err := m.inventarioTemporal(hosts)
	if err != nil {
		return err
	}
	defer os.Remove(inv)
	m.runParalelo(hosts, playbook, inv, cb)
	return nil
}

// ExecutarBloqueo executa o playbook "bloqueo" pasando as opcións coma
// extra-vars (-e '{"bloquear_internet":true,...}'), para que só bloquee o
// que estea marcado na configuración gardada (ver internal/db).
func (m *MotorAnsible) ExecutarBloqueo(hosts []string, opcions map[string]bool, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	playbook := m.cfg.PlaybookPath("bloqueo")
	if !existe(playbook) {
		playbook = filepath.Join(m.cfg.PlaybooksDir, "bloqueo.yaml")
	}
	if !existe(playbook) {
		return &ErrMotor{Msg: "Playbook non atopado: bloqueo.yaml"}
	}
	inv, err := m.inventarioTemporal(hosts)
	if err != nil {
		return err
	}
	defer os.Remove(inv)
	extraVars := extraVarsBloqueo(opcions)
	m.runParaleloComVars(hosts, playbook, inv, extraVars, cb)
	return nil
}

// InstalarTaoClientes despraza Tao (o mesmo binario que xa usa AbrirTao no
// servidor) a /opt/piztu en cada equipo cliente, cos accesos directos no
// menú e no escritorio do alumnado. Porte en quente da mesma acción que o
// instalador fai unha soa vez (Paso 8), para poder repetila cando faga
// falla (equipos novos, reinstalacións) sen volver pasar polo instalador.
func (m *MotorAnsible) InstalarTaoClientes(hosts []string, taoSrc, taoIconSrc string, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	playbook := filepath.Join(m.cfg.PlaybooksDir, "instalar_tao_clientes.yaml")
	if !existe(playbook) {
		return &ErrMotor{Msg: "Playbook non atopado: instalar_tao_clientes.yaml"}
	}
	inv, err := m.inventarioTemporal(hosts)
	if err != nil {
		return err
	}
	defer os.Remove(inv)

	extraVars, err := json.Marshal(map[string]string{
		"tao_src":      taoSrc,
		"tao_icon_src": taoIconSrc,
	})
	if err != nil {
		return err
	}
	m.runParaleloComVars(hosts, playbook, inv, string(extraVars), cb)
	return nil
}

// extraVarsBloqueo traduce as opcions ("internet","ssh","son","rato","teclado")
// aos nomes de variable que len playbooks/bloqueoTotal.yaml e salt/bloqueoTotal.sls.
func extraVarsBloqueo(opcions map[string]bool) string {
	data, _ := json.Marshal(map[string]bool{
		"bloquear_internet": opcions["internet"],
		"bloquear_ssh":      opcions["ssh"],
		"bloquear_son":      opcions["son"],
		"bloquear_rato":     opcions["rato"],
		"bloquear_teclado":  opcions["teclado"],
	})
	return string(data)
}

// ExecutarAdhoc escribe `contido` (texto YAML xa renderizado, ex. por unha IA)
// nun playbook temporal e execútao — sen pasar por PlaybookPath nin gardalo en
// PlaybooksDir. Porte de core/motores/ansible.py → executar_adhoc.
func (m *MotorAnsible) ExecutarAdhoc(contido string, hosts []string, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	play, err := m.playbookTemporalTexto(contido)
	if err != nil {
		return err
	}
	inv, err := m.inventarioTemporal(hosts)
	if err != nil {
		os.Remove(play)
		return err
	}
	defer os.Remove(play)
	defer os.Remove(inv)
	m.runParalelo(hosts, play, inv, cb)
	return nil
}

// playbookTemporalTexto é coma playbookTemporal pero recibe o YAML xa en texto
// (non unha estrutura Go que haxa que serializar) — o contido xa vén
// renderizado desde fóra (ver ExecutarAdhoc).
func (m *MotorAnsible) playbookTemporalTexto(contido string) (string, error) {
	if err := yamlValido(contido); err != nil {
		return "", &ErrMotor{Msg: "Playbook Ansible (YAML) non válido: " + err.Error()}
	}
	os.MkdirAll(m.cfg.TmpDir, 0o755)
	f, err := os.CreateTemp(m.cfg.TmpDir, "_adhoc_tmp_*.yaml")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(contido); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// ── Prácticas: enviar / limpar / recoller ────────────────────────────────────

func (m *MotorAnsible) EnviarFicheiros(hosts []string, ficheiros []string, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	tarefas := m.deteccionEscritorio()
	tarefas = append(tarefas, map[string]any{
		// loop sobre rutas_practicas: normalmente un só candidato, pero en
		// caso de dúbida (ver deteccionEscritorio) créanse os dous.
		"name": "Crear directorio(s) de practicas co propietario correcto",
		"file": map[string]any{
			"path": "{{ item }}", "state": "directory", "mode": "0755",
			"owner": m.cfg.SSHUser, "group": m.cfg.SSHUser,
		},
		"loop": "{{ rutas_practicas }}",
	})
	for _, ruta := range ficheiros {
		nome := filepath.Base(ruta)
		tarefas = append(tarefas, map[string]any{
			"name": "Copiar " + nome,
			"copy": map[string]any{
				"src": ruta, "dest": "{{ item }}/" + nome, "mode": "0644",
				"owner": m.cfg.SSHUser, "group": m.cfg.SSHUser,
			},
			"loop": "{{ rutas_practicas }}",
		})
	}
	play := []any{map[string]any{
		// become:false — actúa sobre os ficheiros do propio usuario; sen isto,
		// o ansible_become=yes do inventario forzaría sudo e pediría contrasinal.
		"name": "Enviar prácticas", "hosts": "all", "gather_facts": false, "become": false, "tasks": tarefas,
	}}
	return m.runPlaybookData(play, hosts, cb)
}

func (m *MotorAnsible) LimparPracticas(hosts []string, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	tarefas := m.deteccionEscritorio()
	tarefas = append(tarefas,
		map[string]any{
			// find sobre cada candidato de rutas_practicas; ignore_errors
			// cobre o caso de dúbida no que un dos dous aínda non existise.
			"name":          "Listar ficheiros a borrar en cada cartafol candidato",
			"find":          map[string]any{"paths": "{{ item }}", "file_type": "file"},
			"loop":          "{{ rutas_practicas }}",
			"register":      "achados_borrar",
			"ignore_errors": true,
		},
		map[string]any{
			"name": "Borrar ficheiros do(s) cartafol(es) practicas",
			"file": map[string]any{"path": "{{ item.path }}", "state": "absent"},
			"loop": "{{ achados_borrar.results | map(attribute='files', default=[]) | flatten }}",
		},
	)
	play := []any{map[string]any{
		"name": "Limpar cartafol practicas", "hosts": "all", "gather_facts": false, "become": false, "tasks": tarefas,
	}}
	return m.runPlaybookData(play, hosts, cb)
}

func (m *MotorAnsible) RecollerPracticas(hosts []string, cb Callbacks) error {
	if err := m.comprobarAnsible(); err != nil {
		return err
	}
	// Limpeza local previa (layout practicas/<host>/).
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
	practicas := m.cfg.PracticasDir
	tarefas := []any{map[string]any{
		"name": "Crear dir local",
		"file": map[string]any{
			"path":  filepath.Join(practicas, "{{ inventory_hostname }}"),
			"state": "directory", "mode": "0755",
		},
		"delegate_to": "localhost",
	}}
	for _, t := range m.deteccionEscritorio() {
		tarefas = append(tarefas, t)
	}
	tarefas = append(tarefas,
		map[string]any{
			"name":          "Listar ficheiros remotos en cada cartafol candidato",
			"find":          map[string]any{"paths": "{{ item }}", "file_type": "file"},
			"loop":          "{{ rutas_practicas }}",
			"register":      "achados_recoller",
			"ignore_errors": true,
		},
		map[string]any{
			// flat:true + mesmo nome de ficheiro nos dous candidatos (caso de
			// dúbida) → a segunda descarga simplemente sobrescribe a primeira
			// co mesmo contido; non hai risco de duplicado real.
			"name": "Descargar ficheiros",
			"fetch": map[string]any{
				"src":             "{{ item.path }}",
				"dest":            practicas + "/{{ inventory_hostname }}/",
				"flat":            true,
				"fail_on_missing": false,
			},
			"loop": "{{ achados_recoller.results | map(attribute='files', default=[]) | flatten }}",
		},
	)
	play := []any{map[string]any{
		"name": "Recoller prácticas", "hosts": "all", "gather_facts": false, "become": false, "tasks": tarefas,
	}}
	return m.runPlaybookData(play, hosts, cb)
}

// ── Helpers internos ─────────────────────────────────────────────────────────

// deteccionEscritorio resolve rutas_practicas (lista de 1 ou 2 cartafoles
// candidatos de prácticas, ver AnsibleRutasPracticasJinja2): comproba se
// existen Escritorio e Desktop no cliente para saber se hai dúbida.
func (m *MotorAnsible) deteccionEscritorio() []any {
	return []any{
		map[string]any{
			"name":     "Verificar se existe o cartafol Escritorio",
			"stat":     map[string]any{"path": m.cfg.RemoteDesktopGL},
			"register": "esc_stat",
		},
		map[string]any{
			"name":     "Verificar se existe o cartafol Desktop",
			"stat":     map[string]any{"path": m.cfg.RemoteDesktopEN},
			"register": "desk_stat",
		},
		map[string]any{
			"name":     "Determinar o(s) cartafol(es) de prácticas",
			"set_fact": map[string]any{"rutas_practicas": m.cfg.AnsibleRutasPracticasJinja2()},
		},
	}
}

func (m *MotorAnsible) runPlaybookData(play []any, hosts []string, cb Callbacks) error {
	inv, err := m.inventarioTemporal(hosts)
	if err != nil {
		return err
	}
	pb, err := m.playbookTemporal(play)
	if err != nil {
		os.Remove(inv)
		return err
	}
	defer os.Remove(inv)
	defer os.Remove(pb)
	// As operacións de ficheiros actúan sobre os ficheiros do propio usuario e
	// NON deben escalar a root. O `become: false` do play non abonda: a variable
	// `ansible_become=yes` do inventario ten máis precedencia ca o keyword do
	// play. Un extra-var (`-e`) si a supera (é a precedencia máis alta), así que
	// desactivamos become aquí de forma efectiva.
	m.runParaleloComVars(hosts, pb, inv, `{"ansible_become": false}`, cb)
	return nil
}

func (m *MotorAnsible) inventarioTemporal(destinos []string) (string, error) {
	return InventarioTemporal(m.cfg, destinos)
}

// InventarioTemporal xera un inventario temporal para `destinos` (exportado
// para que outros paquetes, ex. internal/ruido, poidan lanzar ansible-playbook
// directamente sen replicar a lóxica de credenciais SSH).
func InventarioTemporal(cfg *config.Config, destinos []string) (string, error) {
	m := &MotorAnsible{cfg: cfg}
	hv := inventory.LerHostsConVars(m.cfg.HostsFile)
	os.MkdirAll(m.cfg.TmpDir, 0o755)
	f, err := os.CreateTemp(m.cfg.TmpDir, "_inv_tmp_*.ini")
	if err != nil {
		return "", err
	}
	// Escribimos os hosts baixo o grupo da aula (non baixo [all] solto): os
	// playbooks reais apuntan a "hosts: aula" (cfg.Ansible.GrupoAula), e un
	// host que só pertence a [all] non casa con ese patrón. O grupo "all"
	// é implícito en Ansible — inclúe igualmente estes hosts sen declaralos
	// á parte, así que tamén serve para os playbooks con "hosts: all".
	var b strings.Builder
	b.WriteString("[" + m.cfg.Ansible.GrupoAula + "]\n")
	for _, h := range destinos {
		vars := hv[h]
		resolto := inventory.ResolverHostCached(h, inventory.ExtraerMac(vars))
		if resolto != h {
			vars = strings.TrimSpace("ansible_host=" + resolto + " " + vars)
		}
		b.WriteString(strings.TrimSpace(h+" "+vars) + "\n")
	}
	b.WriteString("\n[all:vars]\n" +
		"ansible_become=yes\n" +
		"ansible_become_method=sudo\n" +
		"ansible_python_interpreter=/usr/bin/python3\n" +
		"ansible_user=" + m.cfg.SSHUser + "\n" +
		"ansible_ssh_common_args='-o StrictHostKeyChecking=no'\n")
	if m.cfg.SSHKeyFile != "" {
		b.WriteString("ansible_ssh_private_key_file=" + m.cfg.SSHKeyFile + "\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

func (m *MotorAnsible) playbookTemporal(play []any) (string, error) {
	os.MkdirAll(m.cfg.TmpDir, 0o755)
	f, err := os.CreateTemp(m.cfg.TmpDir, "_play_tmp_*.yaml")
	if err != nil {
		return "", err
	}
	data, err := yaml.Marshal(play)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// runParalelo lanza unha goroutine por host (ansible-playbook --limit host).
func (m *MotorAnsible) runParalelo(hosts []string, playbook, inv string, cb Callbacks) {
	m.runParaleloComVars(hosts, playbook, inv, "", cb)
}

// runParaleloComVars é coma runParalelo pero admite extra-vars en formato
// JSON (pasadas con "-e"); extraVars="" equivale a non pasar "-e" ningún.
func (m *MotorAnsible) runParaleloComVars(hosts []string, playbook, inv, extraVars string, cb Callbacks) {
	dset := map[string]bool{}
	for _, h := range hosts {
		dset[h] = true
	}
	var wg sync.WaitGroup
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			out, rc := runHost(host, playbook, inv, extraVars)
			recap, hai := parsearRecap(out, dset)[host]
			if rc != 0 || !hai || recap.Failed > 0 || recap.Unreachable > 0 {
				cb.erro(host, out)
			} else {
				cb.ok(host, out)
			}
		}(h)
	}
	wg.Wait()
}

// RunAnsiblePlaybook lanza `ansible-playbook --limit host` e devolve a saída
// combinada e o código de saída. Exportado para internal/ruido (verificación
// de bloqueo), que necesita ler a saída crúa en vez do resultado ok/erro
// simplificado de ExecutarAccion.
func RunAnsiblePlaybook(host, playbook, inv string) (string, int) {
	return runHost(host, playbook, inv, "")
}

// runHost executa ansible-playbook contra un só host. Se extraVars non está
// baleiro, pásase coma JSON en "-e" (ver extraVarsBloqueo).
func runHost(host, playbook, inv, extraVars string) (string, int) {
	args := []string{"-i", inv, playbook, "--limit", host}
	if extraVars != "" {
		args = append(args, "-e", extraVars)
	}
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Dir = filepath.Dir(playbook)
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False", "PYTHONUNBUFFERED=1")
	out, _ := cmd.CombinedOutput()
	rc := 0
	if cmd.ProcessState != nil {
		rc = cmd.ProcessState.ExitCode()
	}
	return string(out), rc
}

type recap struct{ Failed, Unreachable int }

// parsearRecap extrae {host: {failed, unreachable}} do bloque PLAY RECAP.
func parsearRecap(texto string, destinos map[string]bool) map[string]recap {
	res := map[string]recap{}
	enRecap := false
	for _, line := range strings.Split(texto, "\n") {
		if strings.Contains(line, "PLAY RECAP") {
			enRecap = true
			continue
		}
		if !enRecap || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 || !destinos[parts[0]] {
			continue
		}
		v := func(prefix string) int {
			for _, p := range parts {
				if strings.HasPrefix(p, prefix) {
					n, _ := strconv.Atoi(strings.TrimPrefix(p, prefix))
					return n
				}
			}
			return 0
		}
		res[parts[0]] = recap{Failed: v("failed="), Unreachable: v("unreachable=")}
	}
	return res
}

func existe(ruta string) bool {
	_, err := os.Stat(ruta)
	return err == nil
}
