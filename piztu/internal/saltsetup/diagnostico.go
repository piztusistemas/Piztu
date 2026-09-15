package saltsetup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Comprobacion é o resultado dunha proba do diagnóstico. Arranxo é a suxestión
// accionable para o profesor (baleiro se non fai falla nada).
type Comprobacion struct {
	Ok      bool   `json:"ok"`
	Titulo  string `json:"titulo"`
	Detalle string `json:"detalle"`
	Arranxo string `json:"arranxo"`
}

// Diagnostico comproba de punta a punta o circuíto do motor Salt e devolve unha
// lista lexible, sen tocar nada e sen pedir contrasinal (`salt` xa está na regra
// NOPASSWD que instala InstalarMaster; `salt-key` non, así que non se usa).
//
// Existe porque os fallos de Salt son mudos e encadeados: un state.apply que non
// fai nada pode ser o master parado, o file_roots sen configurar, un nome do
// inventario que non coincide co id do minion, ou simplemente o equipo apagado —
// e desde a interface todos se ven igual. Cada Comprobacion distingue un deses
// casos e di que facer.
func Diagnostico(hosts []string) []Comprobacion {
	var r []Comprobacion

	if _, err := exec.LookPath("salt"); err != nil {
		return append(r, Comprobacion{
			Titulo:  "Binario `salt`",
			Detalle: "non está instalado neste equipo",
			Arranxo: "⚙ Aula → Instalar Salt (master + minions).",
		})
	}
	r = append(r, Comprobacion{Ok: true, Titulo: "Binario `salt`", Detalle: "instalado"})

	// systemctl is-active non precisa root.
	estado, _ := saidaCurta("systemctl", []string{"is-active", "salt-master"}, 10*time.Second)
	activo := strings.TrimSpace(estado) == "active"
	r = append(r, Comprobacion{
		Ok:      activo,
		Titulo:  "Servizo salt-master",
		Detalle: strings.TrimSpace(estado),
		Arranxo: se(activo, "", "sudo systemctl restart salt-master (mira `journalctl -u salt-master` se non arranca)."),
	})
	if !activo {
		return r
	}

	// Un só test.ping a todos: valida de paso a regra sudoers NOPASSWD.
	saida, _ := saidaCurta("sudo", []string{"-n", "salt", "--out=json", "--static", "*", "test.ping"}, 90*time.Second)
	if strings.Contains(saida, "a password is required") {
		return append(r, Comprobacion{
			Titulo:  "Permiso para executar `salt`",
			Detalle: "sudo pide contrasinal ao chamar a `salt`",
			Arranxo: "falta a regra /etc/sudoers.d/piztu-salt; repite ⚙ Aula → Instalar Salt.",
		})
	}
	responden := idsQueResponden(saida)
	r = append(r, Comprobacion{
		Ok:      len(responden) > 0,
		Titulo:  "Minions que responden",
		Detalle: fmt.Sprintf("%d de %d equipos do inventario", len(responden), len(hosts)),
		Arranxo: se(len(responden) > 0, "", "ningún minion contesta: comproba que os equipos estean acendidos e coa chave aceptada (`sudo salt-key -L`)."),
	})

	// O fileserver do master: se non serve ficheiros, ningún state.apply funciona.
	if len(responden) > 0 {
		lista, _ := saidaCurta("sudo", []string{"-n", "salt", "--out=json", "--static", responden[0], "cp.list_master"}, 60*time.Second)
		n := len(ficheirosServidos(lista, responden[0]))
		r = append(r, Comprobacion{
			Ok:      n > 0,
			Titulo:  "Fileserver do master (file_roots)",
			Detalle: fmt.Sprintf("%d ficheiros servidos", n),
			Arranxo: se(n > 0, "", "o master non serve ningún estado: repite ⚙ Aula → Instalar Salt para reescribir /etc/salt/master.d/piztu.conf."),
		})
	}

	r = append(r, desaxusteDeNomes(hosts, responden)...)
	return r
}

// desaxusteDeNomes compara os nomes do inventario cos ids que contestaron. É a
// comprobación que máis tempo aforra: un equipo pode estar perfectamente vivo e
// aínda así ser inalcanzable porque o inventario o chama `tux01` e o minion se
// identifica como `tux01.local` (Salt apunta polo id, non por DNS) — e o único
// síntoma é un "No minions matched the target" que non chega á interface.
func desaxusteDeNomes(hosts, responden []string) []Comprobacion {
	no := map[string]bool{}
	for _, h := range responden {
		no[h] = true
	}
	inv := map[string]bool{}
	for _, h := range hosts {
		inv[h] = true
	}

	var r []Comprobacion
	for _, id := range responden {
		if inv[id] {
			continue
		}
		// ¿Está no inventario co mesmo nome curto pero outro sufixo?
		parecido := ""
		for _, h := range hosts {
			if nomeCurto(h) == nomeCurto(id) {
				parecido = h
				break
			}
		}
		if parecido != "" {
			r = append(r, Comprobacion{
				Titulo:  "Nome do inventario ≠ id do minion",
				Detalle: fmt.Sprintf("o inventario di %q pero o minion identifícase como %q", parecido, id),
				Arranxo: fmt.Sprintf("cambia %q por %q no inventario (⚙ Aula), ou o `id:` de /etc/salt/minion.d/piztu.conf nese equipo.", parecido, id),
			})
		} else {
			r = append(r, Comprobacion{
				Titulo:  "Minion descoñecido",
				Detalle: fmt.Sprintf("%q responde pero non está no inventario", id),
				Arranxo: "engádeo en ⚙ Aula se é da aula; se non o é, revisa os patróns de autosign.",
			})
		}
	}

	// Dos que non contestan, distinguir "non resolve" (mDNS/avahi) de "apagado".
	// En paralelo: unha aula de 22 equipos apagados son 22 tempos de espera
	// seguidos, e isto execútase cun botón da interface esperando.
	var mu sync.Mutex
	var wg sync.WaitGroup
	var senNome []string
	for _, h := range hosts {
		if no[h] {
			continue
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
				mu.Lock()
				senNome = append(senNome, host)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	if len(senNome) > 0 {
		sort.Strings(senNome)
		mostra := senNome
		if len(mostra) > 6 {
			mostra = append(mostra[:6:6], "…")
		}
		r = append(r, Comprobacion{
			Titulo:  "Nomes que non resolven",
			Detalle: fmt.Sprintf("%d equipo(s): %s", len(senNome), strings.Join(mostra, ", ")),
			Arranxo: "se están acendidos, falta mDNS: `sudo apt install avahi-daemon avahi-utils libnss-mdns` (⚙ Aula → Instalar Salt xa o fai).",
		})
	}
	return r
}

// idsQueResponden saca do JSON de `salt '*' test.ping` os ids que devolveron
// true. A saída pode traer texto por diante ("No minions matched…"), así que se
// busca o comezo do JSON en vez de parsear todo o bloque.
func idsQueResponden(saida string) []string {
	var m map[string]any
	if err := json.Unmarshal([]byte(soJSON(saida)), &m); err != nil {
		return nil
	}
	var ids []string
	for id, v := range m {
		if ok, _ := v.(bool); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func ficheirosServidos(saida, id string) []string {
	var m map[string][]string
	if err := json.Unmarshal([]byte(soJSON(saida)), &m); err != nil {
		return nil
	}
	return m[id]
}

func soJSON(s string) string {
	if i := strings.Index(s, "{"); i >= 0 {
		return s[i:]
	}
	return "{}"
}

// nomeCurto queda co nome ata o primeiro punto (tux01.local → tux01).
func nomeCurto(h string) string {
	if i := strings.Index(h, "."); i >= 0 {
		return h[:i]
	}
	return h
}

func saidaCurta(bin string, args []string, limite time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limite)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out), err
}

func se(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
