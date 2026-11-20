package saltsetup

import (
	"fmt"
	"time"
)

// AvisosMinions devolve, para cada equipo de `hosts`, un aviso para o mapa: ""
// se todo está ben (ou o equipo está apagado, que non contestar é o normal), ou
// a causa máis probable e o arranxo se está acendido (← ping) pero o seu minion
// non contesta a Salt.
//
// Existe porque ese fallo é mudo: o equipo vese acendido no mapa, "Bloquear"
// non fai nada e o único rastro é un "No minions matched the target". Casos
// reais: unha imaxe clonada co `id:`/`master:` doutro equipo (bac02, que se
// presentaba como bac01.local e apuntaba a un master vello) ou un id sen o
// sufixo do inventario (bac01 ≠ bac01.local).
//
// Un só `salt '*' test.ping` (xa permitido pola regra sudoers NOPASSWD;
// `salt-key` non o está). Timeout curto: corre en fondo cada minuto.
func AvisosMinions(hosts []string, acendidos map[string]bool) map[string]string {
	saida, _ := saidaCurta("sudo", []string{"-n", "salt", "--out=json", "--static", "-t", "5", "*", "test.ping"}, 60*time.Second)
	return avisosMinions(hosts, acendidos, idsQueResponden(saida))
}

// avisosMinions é o núcleo de AvisosMinions, sen executar `salt` (para os tests).
func avisosMinions(hosts []string, acendidos map[string]bool, responden []string) map[string]string {
	contesta := map[string]bool{}
	for _, id := range responden {
		contesta[id] = true
	}
	res := make(map[string]string, len(hosts))
	for _, h := range hosts {
		if contesta[h] || !acendidos[h] {
			res[h] = ""
			continue
		}
		parecido := ""
		for _, id := range responden {
			if id != h && nomeCurto(id) == nomeCurto(h) {
				parecido = id
				break
			}
		}
		if parecido != "" {
			res[h] = fmt.Sprintf("O minion Salt deste equipo preséntase como %q, non como %q: cambia o `id:` na súa configuración do minion (ou o nome no inventario).", parecido, h)
			continue
		}
		res[h] = fmt.Sprintf("Acendido, pero o minion Salt non contesta: revisa que o servizo salt-minion estea activo e que a súa configuración teña `id: %s` e `master:` apuntando a este servidor (Linux: /etc/salt/minion.d/; Windows: C:\\ProgramData\\Salt Project\\Salt\\conf\\).", h)
	}
	return res
}
