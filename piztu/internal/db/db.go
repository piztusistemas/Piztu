// Package db é o porte de core/db.py: posicións dos equipos no mapa (SQLite),
// o limiar de ruído (ficheiro umbral.txt) e as opcións de bloqueo por defecto
// (ficheiro bloqueo_opcions.json).
package db

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // driver "sqlite" en Go puro (sen CGO)

	"piztu/internal/config"
)

// Store agrupa a conexión SQLite e as rutas de datos.
type Store struct {
	db  *sql.DB
	cfg *config.Config
}

// Posicion é a posición dun equipo no mapa.
type Posicion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Open abre a BD e crea as táboas/ficheiros necesarios (init_db).
func Open(cfg *config.Config) (*Store, error) {
	if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
		// BaseDir pode ser de só lectura (Puppet); non é fatal.
		_ = err
	}
	_ = os.MkdirAll(cfg.PlaybooksDir, 0o755)
	_ = os.MkdirAll(cfg.TmpDir, 0o755)

	conn, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(
		`CREATE TABLE IF NOT EXISTS posicion (hostname TEXT PRIMARY KEY, x REAL, y REAL)`,
	); err != nil {
		return nil, err
	}

	// Táboa chave-valor para axustes persistentes (datos da aula: prefixo,
	// dominio, nº de equipos e, máis adiante, servidor e usuario SSH).
	if _, err := conn.Exec(
		`CREATE TABLE IF NOT EXISTS axuste (clave TEXT PRIMARY KEY, valor TEXT)`,
	); err != nil {
		return nil, err
	}

	if _, err := os.Stat(cfg.UmbralFile); os.IsNotExist(err) {
		_ = os.WriteFile(cfg.UmbralFile, []byte(strconv.Itoa(cfg.Ruido.UmbralDefecto)), 0o644)
	}
	_ = os.MkdirAll(cfg.PracticasDir, 0o755)

	return &Store{db: conn, cfg: cfg}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// GardarPosicion insire ou actualiza a posición dun equipo.
func (s *Store) GardarPosicion(hostname string, x, y float64) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO posicion (hostname, x, y) VALUES (?,?,?)`,
		hostname, x, y,
	)
	return err
}

// GetPosicions devolve {hostname: {x,y}}.
func (s *Store) GetPosicions() (map[string]Posicion, error) {
	rows, err := s.db.Query(`SELECT hostname, x, y FROM posicion`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]Posicion{}
	for rows.Next() {
		var h string
		var p Posicion
		if err := rows.Scan(&h, &p.X, &p.Y); err != nil {
			return nil, err
		}
		res[h] = p
	}
	return res, rows.Err()
}

// LerAxuste devolve o valor gardado para `clave`, ou `defecto` se non existe.
func (s *Store) LerAxuste(clave, defecto string) string {
	var valor string
	err := s.db.QueryRow(`SELECT valor FROM axuste WHERE clave = ?`, clave).Scan(&valor)
	if err != nil {
		return defecto
	}
	return valor
}

// GardarAxuste insire ou actualiza un axuste chave-valor.
func (s *Store) GardarAxuste(clave, valor string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO axuste (clave, valor) VALUES (?,?)`,
		clave, valor,
	)
	return err
}

// LerUmbral le umbral.txt; se falla, devolve o valor por defecto.
func (s *Store) LerUmbral() int {
	data, err := os.ReadFile(s.cfg.UmbralFile)
	if err != nil {
		return s.cfg.Ruido.UmbralDefecto
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return s.cfg.Ruido.UmbralDefecto
	}
	return v
}

// GardarUmbral persiste o limiar en umbral.txt.
func (s *Store) GardarUmbral(valor int) error {
	_ = filepath.Dir(s.cfg.UmbralFile)
	return os.WriteFile(s.cfg.UmbralFile, []byte(strconv.Itoa(valor)), 0o644)
}

// opcionsBloqueoDefecto mantén o comportamento previo de bloqueoTotal (só
// desactivaba rato e teclado) mentres o profesor non configure nada máis.
func opcionsBloqueoDefecto() map[string]bool {
	return map[string]bool{
		"internet": false,
		"ssh":      false,
		"son":      false,
		"rato":     true,
		"teclado":  true,
	}
}

func (s *Store) opcionsBloqueoPath() string {
	return filepath.Join(s.cfg.BaseDir, "bloqueo_opcions.json")
}

// LerOpcionsBloqueo le bloqueo_opcions.json; se falla ou faltan claves,
// completa cos valores por defecto (nunca devolve un mapa incompleto).
func (s *Store) LerOpcionsBloqueo() map[string]bool {
	opcions := opcionsBloqueoDefecto()
	data, err := os.ReadFile(s.opcionsBloqueoPath())
	if err != nil {
		return opcions
	}
	var gardado map[string]bool
	if err := json.Unmarshal(data, &gardado); err != nil {
		return opcions
	}
	for k, v := range gardado {
		opcions[k] = v
	}
	return opcions
}

// GardarOpcionsBloqueo persiste as opcións en bloqueo_opcions.json.
func (s *Store) GardarOpcionsBloqueo(opcions map[string]bool) error {
	data, err := json.Marshal(opcions)
	if err != nil {
		return err
	}
	return os.WriteFile(s.opcionsBloqueoPath(), data, 0o644)
}
