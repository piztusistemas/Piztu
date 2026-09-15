// Package discover atopa equipos na rede local que aínda non están no
// inventario: enumera as subredes IPv4 das interfaces locais e proba
// conexión TCP ao porto 22 (SSH) en paralelo. Non usa nmap nin arp-scan —
// segue a mesma filosofía Go-nativo do paquete sshkey (sen depender de
// ferramentas externas).
package discover

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Equipo é un host atopado no escaneo.
type Equipo struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"` // "" se non resolve por DNS reversa
}

// Log é o callback de progreso. host == "" indica unha mensaxe xeral.
type Log func(host, linha string)

const (
	portoSSH        = "22"
	timeoutConexion = 400 * time.Millisecond
	concurrencia    = 128
	maxHostsPorRede = 1024 // cap de seguridade: evita escaneos de horas nunha rede mal detectada (ex. /16)
)

// Escanear enumera as subredes IPv4 locais (excluíndo loopback) e devolve os
// equipos que responden no porto 22, ordenados por IP. Progreso e cada host
// atopado emítense por `log`.
func Escanear(log Log) []Equipo {
	redes := subredesLocais()
	if len(redes) == 0 {
		log("", "⚠️ non se atopou ningunha subrede IPv4 local")
		return nil
	}

	var enderezos []string
	for _, r := range redes {
		hosts := enderezosHost(r, maxHostsPorRede)
		if len(hosts) == maxHostsPorRede {
			log("", fmt.Sprintf("⚠️ %s é moi grande; escaneando só os primeiros %d equipos", r.String(), maxHostsPorRede))
		} else {
			log("", fmt.Sprintf("Escaneando %s (%d equipos)…", r.String(), len(hosts)))
		}
		enderezos = append(enderezos, hosts...)
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res []Equipo
	)
	sem := make(chan struct{}, concurrencia)
	for _, ip := range enderezos {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, portoSSH), timeoutConexion)
			if err != nil {
				return
			}
			conn.Close()

			hostname := ""
			if nomes, err := net.LookupAddr(ip); err == nil && len(nomes) > 0 {
				hostname = strings.TrimSuffix(nomes[0], ".")
			}

			mu.Lock()
			res = append(res, Equipo{IP: ip, Hostname: hostname})
			mu.Unlock()

			msg := "🟢 responde no porto 22"
			if hostname != "" {
				msg += " — " + hostname
			}
			log(ip, msg)
		}(ip)
	}
	wg.Wait()

	sort.Slice(res, func(i, j int) bool { return res[i].IP < res[j].IP })
	return res
}

// subredesLocais devolve as redes IPv4 (non loopback) das interfaces activas.
func subredesLocais() []*net.IPNet {
	var redes []*net.IPNet
	ifaces, err := net.Interfaces()
	if err != nil {
		return redes
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			redes = append(redes, &net.IPNet{IP: ip4, Mask: ipnet.Mask})
		}
	}
	return redes
}

// enderezosHost devolve as IPs "de host" da rede (exclúe rede e broadcast),
// parando en canto chega a `max` para non enumerar redes enormes por enteiro.
func enderezosHost(r *net.IPNet, max int) []string {
	var res []string
	ip := r.IP.Mask(r.Mask)
	for len(res) < max {
		ip = proximaIP(ip)
		if !r.Contains(ip) || esBroadcast(ip, r) {
			break
		}
		res = append(res, ip.String())
	}
	return res
}

func proximaIP(ip net.IP) net.IP {
	nova := make(net.IP, len(ip))
	copy(nova, ip)
	for i := len(nova) - 1; i >= 0; i-- {
		nova[i]++
		if nova[i] != 0 {
			break
		}
	}
	return nova
}

func esBroadcast(ip net.IP, r *net.IPNet) bool {
	broadcast := make(net.IP, len(r.IP))
	for i := range r.IP {
		broadcast[i] = r.IP[i] | ^r.Mask[i]
	}
	return ip.Equal(broadcast)
}
