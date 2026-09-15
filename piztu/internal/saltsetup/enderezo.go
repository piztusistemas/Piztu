package saltsetup

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// EnderezoMaster deduce con que nome teñen os minions que chamar a este
// servidor. `dominio` é o sufixo da aula (o mesmo que xa se usa para xerar o
// inventario, normalmente ".local").
//
// Existe porque o valor por defecto era os.Hostname() a secas, e iso é
// precisamente o que rompe: nun servidor chamado "tux0" devolve "tux0", que
// resolve AQUÍ (está en /etc/hosts) pero non desde ningún cliente, onde os
// nomes da aula só existen por mDNS e sempre co sufixo. O minion quedaba
// configurado con `master: tux0`, non atopaba o master, e o síntoma era un
// equipo que simplemente non respondía — sen ningún erro visible no servidor.
// Por iso o candidato con dominio vai PRIMEIRO, e por iso non abonda con
// comprobar que o nome resolve: hai que comprobar o que resolvería un cliente.
func EnderezoMaster(dominio string) string {
	host, _ := os.Hostname()
	curto := host
	if i := strings.Index(curto, "."); i >= 0 {
		curto = curto[:i]
	}

	var candidatos []string
	if d := strings.TrimSpace(dominio); d != "" && curto != "" {
		if !strings.HasPrefix(d, ".") {
			d = "." + d
		}
		candidatos = append(candidatos, curto+d)
	}
	if strings.Contains(host, ".") {
		candidatos = append(candidatos, host) // FQDN real, se o hai
	}
	for _, c := range candidatos {
		if resolveLocal(c) {
			return c
		}
	}

	// Ningún nome resolve: mellor unha IP que un nome que os clientes non van
	// saber traducir. É menos bonita, pero non depende de mDNS nin de DNS.
	if ip := ipSainte(); ip != "" {
		return ip
	}
	if curto != "" {
		return curto
	}
	return host
}

// EnderezoMasterValidado devolve `gardado` se aínda serve, e se non, o enderezo
// deducido. Non se limita a respectar o que hai gardado a posta: o valor que
// quedou nas aulas xa instaladas foi posto por un defecto malo (os.Hostname()
// sen dominio), así que unha configuración vella arránxase soa ao abrir ⚙ Aula
// en vez de arrastrar o fallo para sempre. Un enderezo que o profesor puxese á
// man e que resolva respéctase intacto.
func EnderezoMasterValidado(gardado, dominio string) string {
	if g := strings.TrimSpace(gardado); g != "" && resolveLocal(g) {
		return g
	}
	return EnderezoMaster(dominio)
}

// resolveLocal di se un nome se pode traducir neste equipo. É unha aproximación
// ao que verá o cliente (os dous usan o mesmo mDNS), non unha garantía.
func resolveLocal(nome string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := net.DefaultResolver.LookupHost(ctx, nome)
	return err == nil
}

// ipSainte devolve a IP local coa que este equipo sae á rede. Non abre ningunha
// conexión (UDP non fai handshake): só lle pregunta ao sistema que interface
// usaría, que é a que os clientes da aula van ver.
func ipSainte() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}
