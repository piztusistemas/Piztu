package historial

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"piztu/internal/config"
)

// TestQuantumReal exercises the history uploader against a REAL Quantum server (the fake
// in historial_test.go can drift from it unnoticed). Skipped unless PIZTU_QUANTUM_URL is
// set:
//
//	PIZTU_QUANTUM_URL=http://127.0.0.1:8082/arquivos \
//	PIZTU_QUANTUM_ADMIN_USER=admin PIZTU_QUANTUM_ADMIN_PASS=... go test -run TestQuantumReal ./internal/historial
//
// It writes tmp-historial-real/ and removes it afterwards.
func TestQuantumReal(t *testing.T) {
	base := os.Getenv("PIZTU_QUANTUM_URL")
	user, pass := os.Getenv("PIZTU_QUANTUM_ADMIN_USER"), os.Getenv("PIZTU_QUANTUM_ADMIN_PASS")
	if base == "" || user == "" || pass == "" {
		t.Skip("define PIZTU_QUANTUM_URL, PIZTU_QUANTUM_ADMIN_USER e PIZTU_QUANTUM_ADMIN_PASS para probar contra un Quantum real")
	}
	base = strings.TrimRight(base, "/")
	const ruta = "tmp-historial-real/2026-01-01.jsonl"

	cfg := &config.Config{Filebrowser: config.Filebrowser{URL: base, AdminUsuario: user, AdminContrasinal: pass}}
	c := novoCliente(cfg)
	t.Cleanup(func() {
		if tok := c.obterToken(); tok != "" {
			q := url.Values{"source": {fbSource}, "path": {"/tmp-historial-real"}}
			req, _ := http.NewRequest(http.MethodDelete, base+"/api/resources?"+q.Encode(), nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	})

	if !c.escribirFicheiro(ruta, []byte(`{"tipo":"inicio"}`+"\n")) {
		t.Fatal("escribirFicheiro (novo, cos pais que faltan) fallou")
	}
	if !c.escribirFicheiro(ruta, []byte(`{"tipo":"inicio"}`+"\n"+`{"tipo":"fin"}`+"\n")) {
		t.Fatal("escribirFicheiro sobre un existente (override) fallou")
	}

	// A token that expired (or is garbage) is renewed with one retry.
	c.mu.Lock()
	c.token = "token-caducado"
	c.mu.Unlock()
	if !c.escribirFicheiro(ruta, []byte(`{"tipo":"fin"}`+"\n")) {
		t.Error("con token inválido non se renovou a sesión")
	}
}

// A 429 from Quantum (login burst exhausted, see loginIntentos) is waited out, not fatal.
func TestLoginEsperaOs429(t *testing.T) {
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		if chamadas <= 2 {
			w.Header().Set("Retry-After", "6")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, "tok-ok")
	}))
	defer srv.Close()

	var esperas []time.Duration
	vello := durmir
	durmir = func(d time.Duration) { esperas = append(esperas, d) }
	defer func() { durmir = vello }()

	cfg := &config.Config{Filebrowser: config.Filebrowser{URL: srv.URL, AdminUsuario: "admin", AdminContrasinal: "x"}}
	if tok := novoCliente(cfg).login(); tok != "tok-ok" {
		t.Fatalf("login = %q; quere o token tras esperar", tok)
	}
	if len(esperas) != 2 || esperas[0] != 6*time.Second {
		t.Errorf("esperas = %v; quere dúas de 6 s (o Retry-After)", esperas)
	}
}

// The account lockout (429, Retry-After 900, "too many failed authentication attempts")
// is not waited out: one attempt, and no further login calls until it should be over.
func TestLoginBloqueoDeContaNonInsiste(t *testing.T) {
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"status":429,"message":"too many failed authentication attempts"}`)
	}))
	defer srv.Close()

	var esperas []time.Duration
	vello := durmir
	durmir = func(d time.Duration) { esperas = append(esperas, d) }
	defer func() { durmir = vello }()

	cfg := &config.Config{Filebrowser: config.Filebrowser{URL: srv.URL, AdminUsuario: "admin", AdminContrasinal: "x"}}
	c := novoCliente(cfg)
	for i := 0; i < 5; i++ { // 5 sondaxes do historial seguidas
		if tok := c.obterToken(); tok != "" {
			t.Fatalf("obterToken = %q con a conta bloqueada", tok)
		}
	}
	if chamadas != 1 || len(esperas) != 0 {
		t.Errorf("%d peticións e %d esperas; quere 1 e 0 (non martelear unha conta bloqueada)", chamadas, len(esperas))
	}
}
