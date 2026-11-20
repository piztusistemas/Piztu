package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActualizarAdminTao(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "dixitalizacion")
	ruta := filepath.Join(dir, "admin.json")

	// Sen admin.json (Tao nunca configurado): non hai nada que actualizar, e non se crea.
	if feito, err := actualizarAdminTao(home, "admin", "nova"); feito || err != nil {
		t.Fatalf("sen ficheiro: feito=%v err=%v; quere false, nil", feito, err)
	}
	if _, err := os.Stat(ruta); err == nil {
		t.Fatal("creou un admin.json que non existía")
	}

	// Con ficheiro: cambia SÓ as credenciais, mantén o resto e o modo 0600.
	os.MkdirAll(dir, 0o755)
	orixinal := `{"serverURL":"http://127.0.0.1:8080/arquivos","center":"IES Proba","department":"Tecnoloxía","fbAdminUser":"admin","fbAdminPass":"vella"}`
	if err := os.WriteFile(ruta, []byte(orixinal), 0o600); err != nil {
		t.Fatal(err)
	}
	const especial = `Ñandú p+a%s "1`
	feito, err := actualizarAdminTao(home, "admin2", especial)
	if !feito || err != nil {
		t.Fatalf("feito=%v err=%v; quere true, nil", feito, err)
	}
	var got map[string]string
	raw, _ := os.ReadFile(ruta)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("o resultado non é JSON válido: %v", err)
	}
	if got["fbAdminUser"] != "admin2" || got["fbAdminPass"] != especial {
		t.Errorf("credenciais = %q / %q", got["fbAdminUser"], got["fbAdminPass"])
	}
	if got["serverURL"] != "http://127.0.0.1:8080/arquivos" || got["center"] != "IES Proba" || got["department"] != "Tecnoloxía" {
		t.Errorf("tocou campos que non debía: %v", got)
	}
	if st, _ := os.Stat(ruta); st.Mode().Perm() != 0o600 {
		t.Errorf("modo = %v; quere 0600", st.Mode().Perm())
	}

	// JSON roto: erro e o ficheiro queda como estaba.
	roto := []byte(`{"fbAdminUser": `)
	os.WriteFile(ruta, roto, 0o600)
	if feito, err := actualizarAdminTao(home, "a", "b"); feito || err == nil {
		t.Errorf("con JSON roto: feito=%v err=%v; quere false e erro", feito, err)
	}
	if raw, _ := os.ReadFile(ruta); string(raw) != string(roto) {
		t.Error("estragou un admin.json que xa estaba roto")
	}
}
