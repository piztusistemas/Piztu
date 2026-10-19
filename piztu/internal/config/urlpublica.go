package config

import (
	"net"
	"net/url"
	"os"
	"strings"
)

// confProxyApache é a configuración que playbooks/instalar_filebrowser.yaml
// deixa activada cando pon Apache a publicar File Browser no porto 80. A súa
// existencia é o xeito máis directo de saber se a URL pública leva porto ou non.
const confProxyApache = "/etc/apache2/conf-enabled/piztu-filebrowser.conf"

// URLPublicaFileBrowser devolve a URL coa que os equipos da aula chegan ao File
// Browser deste servidor. Filebrowser.URL é a de uso local (127.0.0.1), que nun
// cliente non apunta a ningures; aquí cámbiase o loopback polo nome de rede do
// servidor e, se Apache xa fai de proxy no porto 80, quítase o :8080.
//
// Úsase ao instalar Tao nos equipos (ver internal/motor/ansible.go), para
// deixarlles escrito o servidor e que o alumnado non teña que tecleala.
// Devolve "" se non se pode deducir nada fiable — nese caso non se escribe nada
// e Tao segue co seu valor por defecto.
func (c *Config) URLPublicaFileBrowser() string {
	if manual := strings.TrimSpace(c.Filebrowser.URLPublica); manual != "" {
		return strings.TrimRight(manual, "/")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.Filebrowser.URL), "/"))
	if err != nil || u.Host == "" {
		return ""
	}
	// Se xa está configurada cun host de rede, é a boa tal cal.
	if !esLoopback(u.Hostname()) {
		return u.String()
	}
	host := nomeDeRedeDesteServidor()
	if host == "" {
		return ""
	}
	if _, err := os.Stat(confProxyApache); err == nil {
		u.Host = host // Apache publícao no 80: a URL vai sen porto
	} else if porto := u.Port(); porto != "" {
		u.Host = net.JoinHostPort(host, porto)
	} else {
		u.Host = host
	}
	return u.String()
}

// esLoopback di se o host é este mesmo equipo (localhost, 127.x, ::1).
func esLoopback(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// nomeDeRedeDesteServidor devolve o nome co que os clientes poden resolver este
// servidor. Se o hostname non ten dominio ningún engádeselle ".local": nas aulas
// os equipos resólvense por mDNS/avahi, onde o nome a secas non resolve.
func nomeDeRedeDesteServidor() string {
	nome, err := os.Hostname()
	if err != nil {
		return ""
	}
	nome = strings.TrimSpace(nome)
	if nome == "" || esLoopback(nome) {
		return ""
	}
	if !strings.Contains(nome, ".") {
		nome += ".local"
	}
	return nome
}
