// Package config é o porte de core/settings.py: carga config.yaml nun struct
// tipado e deriva as rutas do sistema a partir de base_dir. Mantense agnóstico
// de Wails para poder usarse desde calquera punto de entrada.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"piztu/internal/modulos"
)

// baseDirPorDefecto devolve a base de datos por defecto segundo o SO. En Linux
// é /opt/piztu (a máquina do profesor instalada polo instalador). En macOS /opt
// non é escribible sen root, así que se usa a ruta estándar de aplicacións do
// usuario; alí piztu crea hosts, piztu.db, etc. Así o executable é
// autoconfigurable en Mac sen instalador.
func baseDirPorDefecto() string {
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "piztu")
		}
	}
	return "/opt/piztu"
}

// ── Sub-configuracións ───────────────────────────────────────────────────────

type Ruido struct {
	UmbralDefecto       int  `yaml:"umbral_defecto"`
	SegPromedio         int  `yaml:"seg_promedio"`
	SegBloqueo          int  `yaml:"seg_bloqueo"`
	WarmupCiclos        int  `yaml:"warmup_ciclos"`
	Rate                int  `yaml:"rate"`
	Channels            int  `yaml:"channels"`
	BytesPerSample      int  `yaml:"bytes_per_sample"`
	ChunkMs             int  `yaml:"chunk_ms"`
	VerificarDesbloqueo bool `yaml:"verificar_desbloqueo"`
	MaxReintentos       int  `yaml:"max_reintentos"`
	EsperaEntreIntentos int  `yaml:"espera_entre_intentos"`
	EsperaInicial       int  `yaml:"espera_inicial"`
}

// ChunkBytes replica a propiedade calculada de RuidoConfig.
func (r Ruido) ChunkBytes() int {
	rateMs := r.Rate / 1000
	return rateMs * r.ChunkMs * r.Channels * r.BytesPerSample
}

type Ansible struct {
	GrupoAula string            `yaml:"grupo_aula"`
	Playbooks map[string]string `yaml:"playbooks"`
}

type Salt struct {
	GrupoAula string            `yaml:"grupo_aula"`
	Estados   map[string]string `yaml:"estados"`
}

type UI struct {
	PingInterval int `yaml:"ping_interval"`
	PingTimeout  int `yaml:"ping_timeout"`
	// FlaskURL xa non o consome piztu (ver internal/api, que substituíu o
	// camiño Xesta→app.py→blueprints/xesta.py). Mantido no YAML por se
	// alguén arrinca app.py á man; non tocar sen mirar tamén
	// core/settings.py.
	FlaskURL string `yaml:"flask_url"`
}

// Tao é o espello de TaoConfig (core/settings.py): onde Tao
// publica quen está a usar cada equipo (ver internal/tao).
type Tao struct {
	FilebrowserRoot string `yaml:"filebrowser_root"`
	CaducidadeSeg   int    `yaml:"caducidade_seg"`
}

// Filebrowser é o espello de FilebrowserConfig (core/settings.py): credenciais
// e destino para o historial de sesións (ver internal/historial).
type Filebrowser struct {
	URL              string `yaml:"url"`
	AdminUsuario     string `yaml:"admin_usuario"`
	AdminContrasinal string `yaml:"admin_contrasinal"`
	HistorialDir     string `yaml:"historial_dir"`
	ConfirmarSeg     int    `yaml:"confirmar_seg"`
}

// Modulos é o espello de ModulosConfig (core/settings.py): onde atopar o
// catálogo de módulos descargables. CatalogoLocal só se usa como reserva
// mentres IndiceURL non responda (desenvolvemento, ou antes de que piztu.org
// estea montado en produción) — ver internal/modulos/instalar.go.
type Modulos struct {
	IndiceURL     string                             `yaml:"indice_url"`
	CatalogoLocal map[string]modulos.CatalogoEntrada `yaml:"catalogo_local"`
}

// ── Config principal ─────────────────────────────────────────────────────────

// Config é o espello de AppConfig. Os campos *Dir/*File derívanse de BaseDir se
// non se fixan explicitamente en config.yaml.
type Config struct {
	Idioma string `yaml:"idioma"`
	Motor  string `yaml:"motor"`

	BaseDir      string `yaml:"base_dir"`
	DBPath       string `yaml:"db_path"`
	HostsFile    string `yaml:"hosts_file"`
	UmbralFile   string `yaml:"umbral_file"`
	PracticasDir string `yaml:"practicas_dir"`
	PlaybooksDir string `yaml:"playbooks_dir"`
	SaltDir      string `yaml:"salt_dir"`
	ModulosDir   string `yaml:"modulos_dir"`
	MotorFile    string `yaml:"motor_file"`
	TmpDir       string `yaml:"tmp_dir"`

	SSHUser           string `yaml:"ssh_user"`
	SSHPort           int    `yaml:"ssh_port"`
	SSHKeyFile        string `yaml:"ssh_key_file"`
	SSHTimeoutSeconds int    `yaml:"ssh_timeout_seconds"`

	FlaskSecretKey string `yaml:"flask_secret_key"`
	FlaskHost      string `yaml:"flask_host"`
	FlaskPort      int    `yaml:"flask_port"`
	FlaskDebug     bool   `yaml:"flask_debug"`
	MaxUploadMB    int    `yaml:"max_upload_mb"`

	RemoteDesktopGL string `yaml:"remote_desktop_gl"`
	RemoteDesktopEN string `yaml:"remote_desktop_en"`
	RemotePracticas string `yaml:"remote_practicas"`

	// accionsModulo son as accións que traen os módulos, rexistradas en quente.
	// Non vén do YAML: é estado de execución.
	accionsModulo map[string]AccionExterna

	Ruido       Ruido       `yaml:"ruido"`
	Ansible     Ansible     `yaml:"ansible"`
	Salt        Salt        `yaml:"salt"`
	UI          UI          `yaml:"ui"`
	Tao         Tao         `yaml:"tao"`
	Filebrowser Filebrowser `yaml:"filebrowser"`
	Modulos     Modulos     `yaml:"modulos"`
}

const configEnv = "XESTION_AULA_CONFIG"

// Load busca e carga config.yaml, aplica valores por defecto e deriva rutas.
func Load() (*Config, error) {
	ruta := localizarConfig()
	c := porDefecto()
	if ruta != "" {
		data, err := os.ReadFile(ruta)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, err
		}
	}
	c.normalizar()
	return c, nil
}

// localizarConfig replica a busca de _load_yaml: env > ./config.yaml > ../config.yaml.
func localizarConfig() string {
	if v := os.Getenv(configEnv); v != "" {
		return v
	}
	for _, cand := range []string{"config.yaml", filepath.Join("..", "config.yaml")} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

func porDefecto() *Config {
	return &Config{
		Idioma:            "gl",
		Motor:             "salt",
		BaseDir:           baseDirPorDefecto(),
		SSHUser:           "usuario",
		SSHPort:           22,
		SSHTimeoutSeconds: 25,
		FlaskSecretKey:    "piztu_secret",
		FlaskHost:         "0.0.0.0",
		FlaskPort:         5000,
		FlaskDebug:        true,
		MaxUploadMB:       512,
		RemoteDesktopGL:   "/home/usuario/Escritorio",
		RemoteDesktopEN:   "/home/usuario/Desktop",
		RemotePracticas:   "practicas",
		Ruido: Ruido{
			UmbralDefecto: 50, SegPromedio: 30, SegBloqueo: 30, WarmupCiclos: 20,
			Rate: 44100, Channels: 1, BytesPerSample: 2, ChunkMs: 300,
			VerificarDesbloqueo: true, MaxReintentos: 5,
			EsperaEntreIntentos: 15, EsperaInicial: 20,
		},
		Ansible: Ansible{GrupoAula: "aula", Playbooks: map[string]string{
			"acender": "acenderAula", "durmir": "durmirAula", "bloqueo": "bloqueoTotal",
			"liberar": "desbloquearAula", "apagar": "apagar_aula",
			"aviso_ruido": "avisoRuido", "comprobar_bloqueo": "comprobar_bloqueo",
		}},
		Salt: Salt{GrupoAula: "aula", Estados: map[string]string{
			"acender": "acenderAula", "durmir": "durmirAula", "bloqueo": "bloqueoTotal",
			"liberar": "desbloquearAula", "apagar": "apagar_aula",
			"aviso_ruido": "avisoRuido", "comprobar_bloqueo": "comprobar_bloqueo",
		}},
		UI: UI{PingInterval: 5, PingTimeout: 1, FlaskURL: "http://127.0.0.1:5000"},
		Tao: Tao{
			FilebrowserRoot: "/var/www/html/arquivos",
			CaducidadeSeg:   600,
		},
		Filebrowser: Filebrowser{
			URL:          "http://127.0.0.1:8080",
			AdminUsuario: "admin",
			HistorialDir: "dixitalizacion/historial",
			ConfirmarSeg: 300,
		},
		Modulos: Modulos{
			IndiceURL: "https://piztu.org/modulos/modulos.json",
		},
	}
}

// normalizar deriva rutas baleiras de BaseDir e garante dirs escribibles,
// igual que AppConfig.__post_init__.
func (c *Config) normalizar() {
	c.Idioma = strings.ToLower(strings.TrimSpace(c.Idioma))
	if c.Idioma == "" {
		c.Idioma = "gl"
	}
	c.Motor = strings.ToLower(strings.TrimSpace(c.Motor))
	if c.Motor == "" {
		c.Motor = "salt"
	}

	deriva := func(dst *string, nome string) {
		if *dst == "" {
			*dst = filepath.Join(c.BaseDir, nome)
		}
	}
	deriva(&c.DBPath, "piztu.db")
	deriva(&c.HostsFile, "hosts")
	deriva(&c.UmbralFile, "umbral.txt")
	deriva(&c.PracticasDir, "practicas")
	deriva(&c.PlaybooksDir, "playbooks")
	deriva(&c.SaltDir, "salt")
	deriva(&c.ModulosDir, "modulos")
	deriva(&c.MotorFile, "motor.txt")
	deriva(&c.TmpDir, "tmp")

	c.TmpDir = dirEscribible(c.TmpDir, "tmp")
	c.PracticasDir = dirEscribible(c.PracticasDir, "practicas")

	if c.SSHKeyFile != "" {
		if strings.HasPrefix(c.SSHKeyFile, "~") {
			home, _ := os.UserHomeDir()
			c.SSHKeyFile = filepath.Join(home, c.SSHKeyFile[1:])
		}
	} else {
		c.SSHKeyFile = resolverSSHKey()
	}
}

// dirEscribible replica _dir_escribible: usa o preferido se se pode escribir,
// se non cae a ~/.local/share/piztu/<nome> e por último a un temporal.
func dirEscribible(preferido, nome string) string {
	home, _ := os.UserHomeDir()
	fallback := filepath.Join(home, ".local", "share", "piztu", nome)
	for _, cand := range []string{preferido, fallback} {
		if err := os.MkdirAll(cand, 0o755); err != nil {
			continue
		}
		proba := filepath.Join(cand, ".w_test")
		if f, err := os.Create(proba); err == nil {
			f.Close()
			os.Remove(proba)
			return cand
		}
	}
	tmp, _ := os.MkdirTemp("", "piztu_"+nome+"_")
	return tmp
}

// resolverSSHKey busca unha clave privada en ~/.ssh cando ssh_key_file está
// baleiro en config.yaml. A orde IMPORTA: primeiro as claves cuxo nome di que
// son de Piztu (id_rsa_piztu*, as que se xeran/distribúen para a aula), despois
// a id_rsa xenérica (a que reparte distribuirClave.sh) e só ao final calquera
// outra id_rsa_*.
//
// NON se prioriza id_rsa_<hostname>: nun servidor chamado p.ex. "tux0" iso
// colle ~/.ssh/id_rsa_tux0 — a clave PARA entrar no servidor, que os clientes
// non teñen autorizada — e todas as conexións fallan con "unable to
// authenticate, attempted methods [none publickey]" aínda habendo ao lado a
// clave boa da aula. Esa preferencia era o bug; a chave do propio host é
// precisamente a que menos probabilidades ten de valer nos clientes.
func resolverSSHKey() string {
	home, _ := os.UserHomeDir()
	sshdir := filepath.Join(home, ".ssh")

	var piztu, outras []string
	if globs, _ := filepath.Glob(filepath.Join(sshdir, "id_rsa_*")); globs != nil {
		sort.Strings(globs)
		for _, g := range globs {
			if strings.HasSuffix(g, ".pub") {
				continue
			}
			if strings.Contains(filepath.Base(g), "piztu") {
				piztu = append(piztu, g)
			} else {
				outras = append(outras, g)
			}
		}
	}
	candidatos := append(piztu, filepath.Join(sshdir, "id_rsa"))
	candidatos = append(candidatos, outras...)
	for _, c := range candidatos {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// ── Helpers de rutas (espello de playbook_path / salt_state_path) ────────────

// ── Accións que traen os módulos ─────────────────────────────────────────────

// AccionExterna son as rutas coas que un módulo implementa unha acción, unha por
// motor. Rexístranse no arranque (ver app.go) para que a resolución por nome que
// xa fan os motores atope tamén o que vive no cartafol dun módulo, sen que
// motor/ teña que saber que existen os módulos.
type AccionExterna struct{ Playbook, Estado, Script string }

// RexistrarAccionModulo asocia un nome lóxico ás rutas dun módulo.
func (c *Config) RexistrarAccionModulo(id string, a AccionExterna) {
	if c.accionsModulo == nil {
		c.accionsModulo = map[string]AccionExterna{}
	}
	c.accionsModulo[id] = a
}

// LimparAccionsModulo esquece as accións rexistradas, para volver a cargalas
// cando cambia o estado dos módulos.
func (c *Config) LimparAccionsModulo() { c.accionsModulo = nil }

// AccionModulo devolve as rutas rexistradas para `id`.
func (c *Config) AccionModulo(id string) (AccionExterna, bool) {
	a, ok := c.accionsModulo[id]
	return a, ok
}

func (c *Config) PlaybookPath(nome string) string {
	if a, ok := c.accionsModulo[nome]; ok && a.Playbook != "" {
		return a.Playbook
	}
	fich := c.Ansible.Playbooks[nome]
	if fich == "" {
		fich = nome
	}
	return filepath.Join(c.PlaybooksDir, fich+".yaml")
}

func (c *Config) SaltStatePath(nome string) string {
	if a, ok := c.accionsModulo[nome]; ok && a.Estado != "" {
		return a.Estado
	}
	return filepath.Join(c.SaltDir, c.SaltEstadoNome(nome)+".sls")
}

func (c *Config) SaltEstadoNome(nome string) string {
	if v := c.Salt.Estados[nome]; v != "" {
		return v
	}
	return nome
}

// SesionsDir devolve o cartafol onde Tao publica quen está a usar
// cada equipo (dixitalizacion/sesions/<equipo>.json dentro do File Browser
// compartido), espello de TaoConfig.sesions_dir.
func (c *Config) SesionsDir() string {
	return filepath.Join(c.Tao.FilebrowserRoot, "dixitalizacion", "sesions")
}

// AnsibleRutasPracticasJinja2 devolve unha expresión Jinja2 que resolve, en
// tempo de execución do playbook (a partir de esc_stat/desk_stat, ver
// deteccionEscritorio), a lista de cartafoles de prácticas candidatos:
//   - Existe só un dos dous (Escritorio/Desktop) → só ese, con confianza.
//   - Existen os dous, ou non existe ningún (dúbida real) → os dous, para non
//     arriscar deixar (nin buscar) as prácticas no cartafol que non toca.
func (c *Config) AnsibleRutasPracticasJinja2() string {
	gl := c.RemoteDesktopGL + "/" + c.RemotePracticas
	en := c.RemoteDesktopEN + "/" + c.RemotePracticas
	return "{{ ['" + gl + "'] if (esc_stat.stat.exists and not desk_stat.stat.exists)" +
		" else (['" + en + "'] if (desk_stat.stat.exists and not esc_stat.stat.exists)" +
		" else ['" + gl + "', '" + en + "']) }}"
}
