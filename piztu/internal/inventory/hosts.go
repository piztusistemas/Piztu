// Package inventory le o ficheiro de inventario `hosts` (formato Ansible).
// Porte de get_equipos / ler_hosts_con_vars de core/ansible.py. É neutro:
// tanto o motor Ansible como o Salt o usan para que o mapa sexa idéntico.
package inventory

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// EscribirHosts xera o ficheiro de inventario a partir dos datos da aula:
// unha liña por equipo (prefixo + número de 2 díxitos + dominio), dentro do
// grupo [aula] coas súas variables. Porte de generateHostsFile do instalador,
// para que piztu poida crear o inventario por si mesmo (sen o instalador).
func EscribirHosts(hostsFile, prefixo, dominio string, n int) error {
	lines := []string{"[aula]"}
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("%s%02d%s", prefixo, i, dominio))
	}
	lines = append(lines,
		"[aula:vars]",
		"ansible_become=yes",
		"ansible_become_method=sudo",
		"ansible_python_interpreter=/usr/bin/python3",
	)
	return os.WriteFile(hostsFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// AppendEquipos engade `hosts` ao bloque [aula] sen rexenerar o resto do
// ficheiro (preserva outros equipos, comentarios e variables). Ignora os que
// xa estean no inventario (dedupe contra GetEquipos). Se o ficheiro non
// existe aínda, créao coa mesma estrutura de EscribirHosts.
func AppendEquipos(hostsFile string, hosts []string) error {
	existentes := map[string]bool{}
	for _, h := range GetEquipos(hostsFile) {
		existentes[h] = true
	}
	var novos []string
	for _, h := range hosts {
		if !existentes[h] {
			novos = append(novos, h)
			existentes[h] = true
		}
	}
	if len(novos) == 0 {
		return nil
	}

	data, err := os.ReadFile(hostsFile)
	if err != nil {
		lines := append([]string{"[aula]"}, novos...)
		lines = append(lines,
			"[aula:vars]",
			"ansible_become=yes",
			"ansible_become_method=sudo",
			"ansible_python_interpreter=/usr/bin/python3",
		)
		return os.WriteFile(hostsFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	}

	linhas := strings.Split(string(data), "\n")
	dentroAula := false
	fin := len(linhas)
	for i, raw := range linhas {
		l := strings.TrimSpace(raw)
		if l == "[aula]" {
			dentroAula = true
			continue
		}
		if dentroAula && strings.HasPrefix(l, "[") {
			fin = i
			break
		}
	}
	if !dentroAula {
		// Non hai sección [aula]: créase ao principio, antes do resto.
		resultado := append([]string{"[aula]"}, novos...)
		resultado = append(resultado, linhas...)
		return os.WriteFile(hostsFile, []byte(strings.Join(resultado, "\n")), 0644)
	}

	resultado := append([]string{}, linhas[:fin]...)
	resultado = append(resultado, novos...)
	resultado = append(resultado, linhas[fin:]...)
	return os.WriteFile(hostsFile, []byte(strings.Join(resultado, "\n")), 0644)
}

// ActualizarMacs fixa (ou engade) o token `mac=` para os hosts indicados en
// `macs` (host → mac), preservando o resto de tokens e a estrutura do ficheiro.
// Só modifica liñas cuxo primeiro campo estea no mapa; grupos ([aula]) e
// variables de grupo (ansible_become=yes) déixanse intactos. Porte nativo do
// que facía xerarInventario.yaml co lineinfile+backrefs.
func ActualizarMacs(hostsFile string, macs map[string]string) error {
	data, err := os.ReadFile(hostsFile)
	if err != nil {
		return err
	}
	linhas := strings.Split(string(data), "\n")
	for i, raw := range linhas {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		campos := strings.Fields(line)
		mac, ok := macs[campos[0]]
		if !ok {
			continue
		}
		novos := []string{campos[0]}
		for _, tok := range campos[1:] {
			if strings.HasPrefix(tok, "mac=") {
				continue // substitúese pola nova
			}
			novos = append(novos, tok)
		}
		novos = append(novos, "mac="+mac)
		linhas[i] = strings.Join(novos, " ")
	}
	return os.WriteFile(hostsFile, []byte(strings.Join(linhas, "\n")), 0o644)
}

// LerHostsConVars devolve {hostname: "var1=v var2=v …"}.
func LerHostsConVars(hostsFile string) map[string]string {
	res := map[string]string{}
	data, err := os.ReadFile(hostsFile)
	if err != nil {
		return res
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			res[parts[0]] = strings.Join(parts[1:], " ")
		}
	}
	return res
}

// GetEquipos devolve os hostnames do inventario (ignora grupos e variables soltas).
func GetEquipos(hostsFile string) []string {
	equipos := []string{}
	data, err := os.ReadFile(hostsFile)
	if err != nil {
		return equipos
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		// Liña de variable de grupo (ex.: "ansible_user=x") sen host: ignórase.
		if strings.Contains(line, "=") && !strings.Contains(line, " ") {
			continue
		}
		equipos = append(equipos, strings.Fields(line)[0])
	}
	return equipos
}

// ExtraerMac extrae o valor de 'mac=' dunha liña de variables tipo
// "mac=aa:bb:cc:.. outra=val".
func ExtraerMac(variables string) string {
	for _, tok := range strings.Fields(variables) {
		if strings.HasPrefix(tok, "mac=") {
			return strings.TrimPrefix(tok, "mac=")
		}
	}
	return ""
}

// candidatosNome devolve o nome tal cal e a súa variante alternativa
// (con/sen sufixo '.local' — o caso "bac01" vs "bac01.local").
func candidatosNome(host string) []string {
	if strings.HasSuffix(strings.ToLower(host), ".local") {
		return []string{host, host[:len(host)-len(".local")]}
	}
	return []string{host, host + ".local"}
}

// ── Caché compartida de resolución ───────────────────────────────────────────

// ResolucionTTL: resolver un nome .local por mDNS pode levar segundos. A IP dun
// equipo non cambia en tan pouco tempo, así que se garda.
const ResolucionTTL = 5 * time.Minute

var (
	cacheResMu sync.Mutex
	cacheRes   = map[string]entradaRes{}
)

type entradaRes struct {
	ip    string
	desde time.Time
}

// ResolverHostCached é ResolverHost cunha caché compartida por TODOS os
// consumidores (ping, accións, terminal SSH).
//
// Que sexa compartida é o importante: antes o monitor de ping tiña a súa propia
// caché privada, así que unha acción lanzada nada máis abrir piztu resolvía pola
// súa conta e dependía de que a caché mDNS do sistema xa estivese quente. Iso
// producía o "ao abrir non vai, uns segundos despois si". Agora a primeira
// pasada do ping (que arranca de inmediato) deixa as IPs listas para todo o
// demais.
func ResolverHostCached(host, mac string) string {
	cacheResMu.Lock()
	entrada, hai := cacheRes[host]
	cacheResMu.Unlock()
	if hai && time.Since(entrada.desde) < ResolucionTTL {
		return entrada.ip
	}

	ip := ResolverHost(host, mac)

	// Non se cachea un fracaso (devolveuse o propio nome): interesa reintentalo
	// na seguinte pasada, non quedar 5 minutos cun valor que non conecta.
	if ip != host {
		cacheResMu.Lock()
		cacheRes[host] = entradaRes{ip: ip, desde: time.Now()}
		cacheResMu.Unlock()
	}
	return ip
}

// resolverIPv4 devolve a primeira IPv4 de `nome`, ou "" se non a hai. O timeout
// é curto a propósito: se o rexistro A non chega axiña, mellor seguir cos outros
// candidatos ca bloquear o mapa ou unha acción.
func resolverIPv4(nome string) string {
	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelar()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", nome)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0].String()
}

// resolverIPPorMac busca na táboa de veciñanza (ARP) do sistema a IP
// asociada a un MAC coñecido. Devolve "" se non a atopa.
func resolverIPPorMac(mac string) string {
	if mac == "" {
		return ""
	}
	out, err := exec.Command("ip", "neigh").Output()
	if err != nil {
		return ""
	}
	macNorm := strings.ToLower(strings.TrimSpace(mac))
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 5 && parts[3] == "lladdr" && strings.ToLower(parts[4]) == macNorm {
			return parts[0]
		}
	}
	return ""
}

// ResolverHost devolve o IP que realmente resolve para `host`, probando por
// orde: o propio nome, a variante con/sen '.local', e a táboa ARP a partir
// do MAC. Se nada resolve, devolve o nome orixinal.
//
// Devolve a IP (preferindo IPv4) en vez do nome candidato: a resolución
// mDNS (.local) pode tardar varios segundos, e se devolvésemos o nome, cada
// consumidor (ping, ssh, ansible) volvería resolvelo de seu — no caso de
// ping, iso facía que a chamada superase o seu propio timeout e o equipo
// se amosase sempre coma "apagado" aínda estando acendido.
func ResolverHost(host, mac string) string {
	for _, candidato := range candidatosNome(host) {
		if ips, err := net.LookupHost(candidato); err == nil && len(ips) > 0 {
			for _, ip := range ips {
				if strings.Contains(ip, ".") { // prefire IPv4 (evita complicacións de zone-id en IPv6 link-local)
					return ip
				}
			}
			// Só chegaron direccións IPv6. Con mDNS en frío isto é habitual —o
			// rexistro AAAA responde antes ca o A—, e a única IPv6 dun equipo da
			// aula é a link-local (fe80::…%en0): usala dá "no route to host"
			// mentres a veciñanza non estea resolta. Pedimos aquí o rexistro A
			// de forma explícita para agardar por el en vez de coller o que
			// chegase antes. Faise só neste caso: cando LookupHost xa trae a
			// IPv4 (o normal), non se paga ningunha consulta de máis.
			if ip := resolverIPv4(candidato); ip != "" {
				return ip
			}
			return ips[0]
		}
	}
	if ip := resolverIPPorMac(mac); ip != "" {
		return ip
	}
	return host
}
