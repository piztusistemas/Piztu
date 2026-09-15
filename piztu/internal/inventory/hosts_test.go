package inventory

import (
	"strings"
	"testing"
	"time"
)

// TestResolverHostPrefireIPv4 cobre o fallo "a primeira orde tras abrir piztu
// non vai, a segunda si": os equipos son nomes .local e a resolución devolve
// primeiro a IPv6 link-local (fe80::…%en0). Se se usa esa, o Dial falla con
// "no route to host" mentres a veciñanza non estea resolta.
//
// Úsase localhost por ser o único nome de dobre pila garantido en calquera
// máquina onde corran os tests.
func TestResolverHostPrefireIPv4(t *testing.T) {
	ip := ResolverHost("localhost", "")

	if !strings.Contains(ip, ".") {
		t.Fatalf("ResolverHost devolveu %q; agardábase unha IPv4", ip)
	}
	if strings.Contains(ip, "%") {
		t.Errorf("ResolverHost devolveu unha dirección con zone-id (%q): non serve para conectar", ip)
	}
}

// TestResolverHostDevolveONomeSeNonResolve: sen resolución nin MAC, devólvese o
// nome orixinal para que o erro do Dial siga sendo comprensible.
func TestResolverHostDevolveONomeSeNonResolve(t *testing.T) {
	const inexistente = "equipo-que-non-existe-piztu.invalid"
	if got := ResolverHost(inexistente, ""); got != inexistente {
		t.Errorf("ResolverHost(%q) = %q; agardábase o nome orixinal", inexistente, got)
	}
}

// TestResolverHostCachedReutiliza cobre o "ao abrir piztu non vai, uns segundos
// despois si": a caché ten que ser compartida entre consumidores para que a
// primeira pasada do ping deixe as IPs listas para as accións e o terminal SSH.
func TestResolverHostCachedReutiliza(t *testing.T) {
	baleirarCacheRes()

	primeira := ResolverHostCached("localhost", "")
	if primeira == "" || primeira == "localhost" {
		t.Fatalf("non se resolveu localhost: %q", primeira)
	}

	// Trucamos a entrada: se a segunda chamada devolve isto, veu da caché.
	cacheResMu.Lock()
	cacheRes["localhost"] = entradaRes{ip: "10.9.9.9", desde: time.Now()}
	cacheResMu.Unlock()

	if got := ResolverHostCached("localhost", ""); got != "10.9.9.9" {
		t.Errorf("a segunda chamada resolveu de novo (%q); debería vir da caché", got)
	}
}

// TestResolverHostCachedNonCacheaFracasos: un equipo apagado non debe quedar
// 5 minutos cun valor que non conecta; hai que reintentalo.
func TestResolverHostCachedNonCacheaFracasos(t *testing.T) {
	baleirarCacheRes()
	const inexistente = "equipo-que-non-existe-piztu.invalid"

	ResolverHostCached(inexistente, "")

	cacheResMu.Lock()
	_, hai := cacheRes[inexistente]
	cacheResMu.Unlock()
	if hai {
		t.Error("cacheouse un fracaso de resolución")
	}
}

func baleirarCacheRes() {
	cacheResMu.Lock()
	cacheRes = map[string]entradaRes{}
	cacheResMu.Unlock()
}
