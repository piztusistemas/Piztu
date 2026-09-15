// Package sshterm ofrece un terminal SSH interactivo (PTY + shell), porte da
// parte de paramiko de blueprints/control.py. Non depende de Wails: emite a
// saída por callbacks para que a capa app a reenvíe como eventos.
package sshterm

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"piztu/internal/config"
	"piztu/internal/inventory"
)

// Session é unha conexión SSH interactiva activa.
type Session struct {
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
	onData func(string)
	closed bool
	mu     sync.Mutex
}

// Manager mantén a sesión activa (a app de escritorio ten unha á vez).
type Manager struct {
	cfg *config.Config
	mu  sync.Mutex
	cur *Session
}

func NewManager(cfg *config.Config) *Manager { return &Manager{cfg: cfg} }

// Open abre unha sesión contra `host`. onData recibe a saída do terminal;
// onClosed avisa cando pecha. Pecha calquera sesión anterior.
func (m *Manager) Open(host string, onData func(string), onClosed func()) error {
	m.Close()
	if host == "" {
		return errors.New("host non especificado")
	}
	cfg := m.cfg

	auths := metodosAuth(cfg.SSHKeyFile)
	if len(auths) == 0 {
		return errors.New("sen métodos de autenticación SSH (clave ou axente)")
	}
	timeout := time.Duration(cfg.SSHTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	clientCfg := &ssh.ClientConfig{
		User:            cfg.SSHUser,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	// Resólvese o nome a IPv4 antes de conectar: os equipos da aula son nomes
	// .local e a resolución mDNS adoita devolver primeiro a IPv6 link-local, coa
	// que o Dial falla con "no route to host" mentres a veciñanza non estea
	// resolta (ver inventory.ResolverHost).
	addr := net.JoinHostPort(inventory.ResolverHostCached(host, ""), fmt.Sprint(cfg.SSHPort))
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", 50, 220, modes); err != nil {
		sess.Close()
		client.Close()
		return err
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	stderr, _ := sess.StderrPipe()
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return err
	}

	s := &Session{client: client, sess: sess, stdin: stdin, onData: onData}
	m.mu.Lock()
	m.cur = s
	m.mu.Unlock()

	onData(fmt.Sprintf("\r\n\033[1;32m✔ Conectado a %s\033[0m\r\n", host))

	bombear := func(r io.Reader) {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				onData(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}
	go bombear(stdout)
	go bombear(stderr)
	go func() {
		sess.Wait()
		onData("\r\n\033[1;31m[Conexión pechada]\033[0m\r\n")
		m.Close()
		if onClosed != nil {
			onClosed()
		}
	}()
	return nil
}

// Input envía texto ao terminal.
func (m *Manager) Input(data string) {
	m.mu.Lock()
	s := m.cur
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && s.stdin != nil {
		io.WriteString(s.stdin, data)
	}
}

// Resize redimensiona o pseudo-terminal.
func (m *Manager) Resize(cols, rows int) {
	m.mu.Lock()
	s := m.cur
	m.mu.Unlock()
	if s != nil && s.sess != nil {
		s.sess.WindowChange(rows, cols)
	}
}

// Close pecha a sesión activa (se a hai).
func (m *Manager) Close() {
	m.mu.Lock()
	s := m.cur
	m.cur = nil
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.sess != nil {
		s.sess.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
}

// metodosAuth constrúe os métodos de auth: clave privada + axente SSH.
func metodosAuth(keyFile string) []ssh.AuthMethod {
	var métodos []ssh.AuthMethod
	if keyFile != "" {
		if data, err := os.ReadFile(keyFile); err == nil {
			if signer, err := ssh.ParsePrivateKey(data); err == nil {
				métodos = append(métodos, ssh.PublicKeys(signer))
			}
		}
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			métodos = append(métodos, ssh.PublicKeysCallback(ag.Signers))
		}
	}
	return métodos
}
