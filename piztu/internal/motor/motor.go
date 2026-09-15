// Package motor é o porte da capa de abstracción de core/motor.py: unha
// interface común (Motor) con dous backends —Ansible e Salt— e unha fábrica que
// escolle o activo segundo motor.txt (en quente) ou config.yaml (por defecto).
package motor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"piztu/internal/config"
)

// yamlValido comproba que `texto` é YAML sintacticamente correcto, sen
// interpretar o seu contido — usado por ExecutarAdhoc (Ansible e Salt) antes
// de escribir/executar contido que chega xa renderizado desde fóra (ex. unha
// IA), para dar un erro claro en vez dun fallo confuso do binario externo.
func yamlValido(texto string) error {
	var calquera any
	return yaml.Unmarshal([]byte(texto), &calquera)
}

// MotoresValidos e o motor por defecto.
var MotoresValidos = []string{"ansible", "salt", "ssh"}

// Etiquetas son os nomes que se amosan na interface.
var Etiquetas = map[string]string{
	"ansible": "Ansible",
	"salt":    "Salt",
	"ssh":     "SSH",
}

// aliasHerdado traduce nomes de motor antigos aos actuais: o motor "nativo"
// (SSH en Go, sen Salt nin Ansible) renomeouse a "ssh" por ser máis claro. Un
// motor.txt ou config.yaml doutra versión pode traer aínda o nome vello.
func aliasHerdado(nome string) string {
	if nome == "nativo" {
		return "ssh"
	}
	return nome
}

// MotorDefecto depende do SO: en macOS non hai salt-master (nin, de serie,
// ansible), así que o motor SSH (directo, en Go) é o defecto sensato; noutros
// sistemas mantense Salt como ata agora.
var MotorDefecto = motorDefecto()

func motorDefecto() string {
	if runtime.GOOS == "darwin" {
		return "ssh"
	}
	return "salt"
}

// ErrMotor é un erro recuperable dun motor (ex.: playbook/estado non atopado).
type ErrMotor struct{ Msg string }

func (e *ErrMotor) Error() string { return e.Msg }

// Callbacks de progreso (mesma semántica que os callbacks de Python).
type Callbacks struct {
	OnInicio func(host string)
	OnOk     func(host, stdout string)
	OnErro   func(host, stdout string)
}

func (c Callbacks) inicio(h string) {
	if c.OnInicio != nil {
		c.OnInicio(h)
	}
}
func (c Callbacks) ok(h, s string) {
	if c.OnOk != nil {
		c.OnOk(h, s)
	}
}
func (c Callbacks) erro(h, s string) {
	if c.OnErro != nil {
		c.OnErro(h, s)
	}
}

// Motor é o contrato que implementan MotorAnsible e MotorSalt. Todos os métodos
// de execución son bloqueantes: retornan cando remata en todos os hosts.
type Motor interface {
	Nome() string
	GetEquipos() []string
	ExecutarAccion(nomeLogico string, hosts []string, cb Callbacks) error
	// ExecutarBloqueo é coma ExecutarAccion("bloqueo", ...) pero parametrizado:
	// opcions marca que bloquear (claves "internet", "ssh", "son", "rato",
	// "teclado"). Ver playbooks/bloqueoTotal.yaml e salt/bloqueoTotal.sls.
	ExecutarBloqueo(hosts []string, opcions map[string]bool, cb Callbacks) error
	EnviarFicheiros(hosts []string, ficheiros []string, cb Callbacks) error
	LimparPracticas(hosts []string, cb Callbacks) error
	RecollerPracticas(hosts []string, cb Callbacks) error
	// ExecutarAdhoc executa contido xerado en tempo real (playbook YAML de
	// Ansible ou estado .sls de Salt, en texto) nos hosts indicados, sen que
	// exista previamente coma ficheiro fixo en playbooks_dir/salt_dir. Porte
	// de core/motores/{ansible,salt}.py → executar_adhoc. Usado pola API de
	// módulos (internal/api) para o módulo Xesta (asistente de IA): o
	// contido chega xa renderizado desde fóra e non se garda de forma
	// permanente. Devolve ErrMotor se o contido non é válido ou o motor non
	// soporta esta operación (ex.: SSH).
	ExecutarAdhoc(contido string, hosts []string, cb Callbacks) error
}

// correrEnParalelo executa fn(host) -> (saída, ok) en todos os hosts á vez (con
// tope `maxSimul`) e traduce o resultado a callbacks. Compárteno os motores que
// non teñen unha forma propia de paralelizar.
func correrEnParalelo(hosts []string, maxSimul int, cb Callbacks, fn func(string) (string, bool)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxSimul)
	for _, h := range hosts {
		cb.inicio(h)
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, ok := fn(host)
			if ok {
				cb.ok(host, out)
			} else {
				cb.erro(host, out)
			}
		}(h)
	}
	wg.Wait()
}

// ── Persistencia do motor activo (motor.txt) ─────────────────────────────────

func fallbackMotorFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "piztu", "motor.txt")
}

func lerMotorTxt(cfg *config.Config) string {
	for _, ruta := range []string{cfg.MotorFile, fallbackMotorFile()} {
		data, err := os.ReadFile(ruta)
		if err != nil {
			continue
		}
		v := aliasHerdado(strings.ToLower(strings.TrimSpace(string(data))))
		if Soportado(v) {
			return v
		}
	}
	return ""
}

// MotorActivo: motor.txt > config.yaml > defecto.
func MotorActivo(cfg *config.Config) string {
	if v := lerMotorTxt(cfg); v != "" {
		return v
	}
	if m := aliasHerdado(cfg.Motor); Soportado(m) {
		return m
	}
	return MotorDefecto
}

// SetMotor persiste o motor activo en motor.txt. Devolve o nome gardado.
func SetMotor(cfg *config.Config, nome string) (string, error) {
	nome = aliasHerdado(strings.ToLower(strings.TrimSpace(nome)))
	if !Soportado(nome) {
		return "", fmt.Errorf("motor non dispoñible neste sistema: %q", nome)
	}
	var ultimo error
	for _, ruta := range []string{cfg.MotorFile, fallbackMotorFile()} {
		if err := os.MkdirAll(filepath.Dir(ruta), 0o755); err != nil {
			ultimo = err
			continue
		}
		if err := os.WriteFile(ruta, []byte(nome+"\n"), 0o644); err == nil {
			return nome, nil
		} else {
			ultimo = err
		}
	}
	return "", errors.Join(errors.New("non se puido escribir motor.txt"), ultimo)
}

// Soportado di se `nome` ten sentido neste sistema operativo. O motor Salt fala
// cos sockets dun salt-master local, que en macOS non existe nin pode existir
// (ver saltsetup.ErrMasterNonSoportado): alí non se ofrece.
//
// Compróbase tamén ao ler motor.txt para que un valor herdado doutro equipo (ou
// dunha versión anterior) non dea un estado morto: o botón amosaría un motor que
// nin sequera está no menú e toda acción fallaría.
func Soportado(nome string) bool {
	nome = aliasHerdado(nome)
	if nome == "salt" && runtime.GOOS == "darwin" {
		return false
	}
	return valido(nome)
}

func valido(nome string) bool {
	for _, m := range MotoresValidos {
		if m == nome {
			return true
		}
	}
	return false
}

// ── Fábrica con caché por nome ───────────────────────────────────────────────

var (
	cacheMu sync.Mutex
	cache   = map[string]Motor{}
)

// Get devolve a instancia do motor activo (ou o indicado en `nome`).
func Get(cfg *config.Config, nome string) Motor {
	if nome == "" {
		nome = MotorActivo(cfg)
	}
	nome = aliasHerdado(strings.ToLower(strings.TrimSpace(nome)))
	if !Soportado(nome) {
		nome = MotorDefecto
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if m, ok := cache[nome]; ok {
		return m
	}
	var m Motor
	switch nome {
	case "salt":
		m = &MotorSalt{cfg: cfg}
	case "ssh":
		m = &MotorSSH{cfg: cfg}
	default:
		m = &MotorAnsible{cfg: cfg}
	}
	cache[nome] = m
	return m
}
