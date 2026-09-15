// Package modulos xestiona os "apeiros" de piztu: funcións que cada centro pode
// activar ou non, sen que as demais lle estorben na barra.
//
// Hai dous tipos, e a diferenza é deliberada:
//
//   - Internos: van compilados en piztu (ruído, ficheiros). Declaranse aquí
//     para poder acendelos e apagalos, pero o seu código vive no seu paquete.
//   - Externos: aplicacións independentes soltas en <base_dir>/modulos/<id>/ cun
//     manifest modulo.json. Piztu limítase a amosalos e lanzalos — Tao e Xesta
//     son exemplos disto.
//
// O estado (activo/inactivo) persístese cos axustes chave-valor de internal/db,
// non nun ficheiro propio: xa existía ese almacén e non facía falla outro.
package modulos

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IDs dos módulos. Úsanse como clave de persistencia e nas comprobacións do
// backend, así que non deben cambiar á lixeira.
//
// Tao non é interno: é un módulo externo coma Xesta (manifest en tao/modulo.json,
// código en tao/). A constante mantense porque app.go segue tratándoo á parte
// (AbrirTao, localizarTao, o monitor de sesións…) por herdanza histórica e polas
// rutas de busca de instalacións anteriores.
const (
	Ruido     = "ruido"
	Ficheiros = "ficheiros"
	Tao       = "tao"
)

// Modulo describe un apeiro. Os externos completan Executable e Nome; os
// internos usan ClaveI18n para que o nome se traduza co tradutor de piztu.
type Modulo struct {
	ID        string
	ClaveI18n string
	// Nome é o texto de reserva. Os internos teñen tamén ClaveI18n e tradúcense
	// cando hai ficheiros de idioma; o Nome usarase se non os hai (en macOS
	// idiomas/ non chega ao base_dir, e o frontend xa traballa así).
	Nome  string
	Icona string
	// IconaImaxeDataURI, cando non baleiro, é unha imaxe (data:image/...;base64,...)
	// autocontida — non hai servidor de ficheiros estáticos para módulos
	// externos, así que resolvela a data URI en Externos() é máis simple ca
	// montar un. Baleiro para módulos internos e para externos sen imaxe
	// propia (quedan co emoji de Icona coma sempre).
	IconaImaxeDataURI string
	Interno           bool
	PorDefecto        bool
	Executable        string // só externos: que lanzar, relativo ao cartafol do módulo
	Dir               string // só externos: cartafol que o contén
	Accions           []Accion
	Recolector        *Recolector
	Panel             string // só externos: ficheiro YAML do panel, relativo a Dir
	// MantenPiztuAberto, cando true, fai que o botón da barra do mapa NON
	// peche Piztu ao abrir este módulo (comportamento normal doutro módulo
	// de pantalla completa, coma Tao/Yang: substitúe a Piztu por defecto,
	// Ctrl+clic mantén as dúas abertas — ver frontend/src/main.js). Pensado
	// para módulos pequenos, tipo ferramenta auxiliar, que o profesor quere
	// ver A CARÓN do mapa, non EN VEZ del.
	MantenPiztuAberto bool
	// Version, Autor, Email e Web son metadatos opcionais que o propio módulo
	// declara no seu modulo.json. Version é o que se compara co catálogo de
	// piztu.org para saber se hai unha actualización (ver completarInfoExterna
	// en app.go); un módulo sen version declarada cóntase coma desactualizado
	// en canto o catálogo teña calquera versión, para animar a engadila.
	Version string
	Autor   string
	Email   string
	Web     string
	// Descricion é un texto curto opcional que o módulo declara no seu
	// modulo.json, amosado en ⚙ Aula → Módulos xunto ao nome.
	Descricion string
	// Permisos son as capacidades que o módulo pide da API de módulos (ver
	// docs/api-modulos.md) — ex. "motor.ler", "motor.executar". Só teñen
	// sentido para módulos EXECUTABLE (Xesta, Tao); un módulo só de accións/
	// recolector/panel non fala coa API, non a precisa. Concedidos de golpe
	// ao activar o módulo (sen diálogo á parte, ver GardarPermisosConcedidos)
	// — non hai "pedidos pero non concedidos": ou o módulo está activo cos
	// permisos que declara, ou está apagado e non ten ningún.
	Permisos []string
}

// Internos devolve os módulos que van compilados en piztu.
//
// Veñen apagados por defecto a propósito: unha instalación nova non debe
// activar micrófono nin transferencia de ficheiros sen que o profesor o
// decida en ⚙ Aula → Módulos.
func Internos() []Modulo {
	return []Modulo{
		{ID: Ruido, ClaveI18n: "modulos.ruido", Nome: "Ruído", Icona: "🎙", Interno: true, PorDefecto: false},
		{ID: Ficheiros, ClaveI18n: "modulos.ficheiros", Nome: "Ficheiros", Icona: "📤", Interno: true, PorDefecto: false},
	}
}

// Preparar crea o cartafol de módulos e deixa dentro un LEEME.md coas
// instrucións. Chámase no arranque: sen isto o cartafol non existiría ata que
// alguén o creara á man, e o profesor non tería onde soltar nada.
func Preparar(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	leeme := filepath.Join(dir, "LEEME.md")
	if _, err := os.Stat(leeme); err == nil {
		return nil // xa está; pode telo editado
	}
	return os.WriteFile(leeme, []byte(textoLeeme), 0o644)
}

const textoLeeme = "# Módulos de Piztu\n" + `
Piztu é a máquina do tractor; os módulos son os apeiros. Actívanse e
desactívanse en ⚙ Aula → Módulos. Un módulo novo detéctase só, pero non se
acende: ti decides.

Cada módulo vai nun subcartafol deste directorio cun ficheiro ` + "`modulo.json`" + `.
Os módulos internos de Piztu (Ruído, Ficheiros) non aparecen aquí porque non
teñen ficheiros propios; Tao si, en modulos/tao/.

## modulo.json

    {
      "id": "cisterna",
      "nome": "Cisterna",
      "icona": "🚜",
      "iconaImaxe": "appicon.png",
      "executable": "Cisterna.app",

      "version": "1.0.0",
      "autor": "Nome do autor",
      "email": "autor@exemplo.gal",
      "web": "https://exemplo.gal/cisterna",

      "accions": [
        { "id": "baleirar", "etiqueta": "Baleirar", "icona": "💧",
          "script": "baleirar.sh", "estado": "baleirar.sls", "playbook": "baleirar.yaml" }
      ],

      "recolector": { "orde": "...", "cada": 30, "campo": "usuario" },

      "panel": "panel.yaml",

      "permisos": ["motor.ler", "motor.executar"],

      "mantenPiztuAberto": false
    }

Todo agás o id é opcional: un módulo pode ser só accións, só un recolector ou
só un panel. "version" NON fai falla escribila á man: se un módulo se
distribúe por piztu.org, quen o publica pon o número no propio nome do
ficheiro (ex. "cisterna_1.0.0.zip") e Piztu complétao só ao instalar. Sen
version en ningures, o módulo aparece coma desactualizado en canto o catálogo
teña calquera, coma aviso para publicar así.

## As cinco capacidades

1. EXECUTABLE — unha aplicación que Piztu lanza cun botón na barra. Ao lanzala
   recibe a ruta dun JSON co contexto, por argumento (--piztu-contexto RUTA) e
   por variable de contorno (PIZTU_CONTEXTO). Contén: base_dir, o id e cartafol
   do módulo, o motor activo, e a lista de equipos con MAC e se están acendidos.
   Por defecto o botón PECHA Piztu ao abrir o módulo (Ctrl+clic mantén as dúas
   abertas) — "mantenPiztuAberto": true inverte isto, para un módulo pequeno
   que se queira ver A CARÓN do mapa, non EN VEZ del.

2. ACCIONS — botóns novos na barra que se executan en todos os equipos co motor
   activo. Cada acción pode traer tres implementacións e Piztu usa a que
   corresponda: script (motor SSH), estado .sls (Salt e Salt-SSH) e
   playbook .yaml (Ansible). Se falta a do motor activo, o botón non se pinta.
   Nos scripts, @@USER@@ substitúese polo usuario da aula.
   Non se poden usar os nomes que xa emprega Piztu (apagar, bloqueo, liberar...).

3. RECOLECTOR — unha orde que Piztu executa por SSH en cada equipo cada "cada"
   segundos (mínimo 10) e cuxo resultado se amosa no mapa, baixo o nome da
   máquina. A orde debe imprimir JSON nunha liña; "campo" di cal amosar:

       orde:  printf '{"usuario":"%s}' "$(who | awk 'NR==1{print $1}')"
       campo: usuario

   Execútase SEN privilexios. Se un equipo deixa de responder, o seu dato
   desaparece do mapa en vez de quedar pegado.

4. PERMISOS — só ten sentido xunto con EXECUTABLE. Lista das capacidades que o
   módulo pide da API de Piztu (documentada en docs/api-modulos.md), ex.
   "motor.ler" (consultar motor/equipos), "motor.executar" (executar
   contido nos equipos — a capacidade perigosa). Concédense de golpe ao
   activar o módulo, sen diálogo á parte: activalo XA é o consentimento.
   Sen "permisos", o módulo non recibe token e non pode falar coa API.

5. PANEL — unha pantalla de configuración propia, descrita en YAML. Piztu
   debúxaa co seu estilo; non se executa código do módulo na interface:

       titulo: Cisterna do purín
       campos:
         - {tipo: info,        etiqueta: "Texto explicativo"}
         - {tipo: texto,       id: nome,    etiqueta: Nome}
         - {tipo: numero,      id: litros,  etiqueta: Litros, defecto: "3000"}
         - {tipo: lista,       id: equipo,  etiqueta: Equipo, fonte: equipos}
         - {tipo: interruptor, id: aviso,   etiqueta: Avisar ao rematar}
         - {tipo: boton,       id: b,       etiqueta: Baleirar, accion: baleirar}

   Os tipos son pechados: calquera outro ignórase. Os valores gárdanse solos.

## Se algo non aparece

Todo o que Piztu non entenda descártase en silencio para non deixarte sen poder
acender a aula: un manifest ilexible, unha acción sen implementación, un campo
de panel dun tipo descoñecido ou unha saída de recolector que non sexa JSON.
Revisa o rexistro da xanela de Piztu.
`

// AccionsReservadas son os nomes lóxicos que xa usa piztu. Un módulo non pode
// declaralos: senón podería substituír "apagar" ou "bloqueo" por algo seu, que é
// precisamente o que non queremos que faga un apeiro de terceiros.
var AccionsReservadas = map[string]bool{
	"acender": true, "acenderAula": true,
	"durmir": true, "durmirAula": true,
	"apagar": true, "apagar_aula": true,
	"reiniciar": true, "reiniciarAula": true,
	"bloqueo": true, "bloqueoTotal": true,
	"liberar": true, "desbloquear": true, "desbloquearAula": true,
	"aviso_ruido": true, "avisoRuido": true,
	"comprobar_bloqueo": true,
}

// Accion é unha acción que trae un módulo: un botón novo na barra que se executa
// en todos os equipos co motor activo.
//
// Os tres campos de implementación son alternativas para cada motor, e o módulo
// pode traer as que queira. Se falta a do motor activo, a acción non se ofrece:
// máis vale non pintar o botón que pintalo e que falle ao premelo.
type Accion struct {
	ID       string
	Etiqueta string
	Icona    string
	Playbook string // ruta absoluta ao .yaml  (motor Ansible)
	Estado   string // ruta absoluta ao .sls   (motores Salt e Salt-SSH)
	Script   string // ruta absoluta ao .sh    (motor SSH)
}

type accionManifest struct {
	ID       string `json:"id"`
	Etiqueta string `json:"etiqueta"`
	Icona    string `json:"icona"`
	Playbook string `json:"playbook"`
	Estado   string `json:"estado"`
	Script   string `json:"script"`
}

// Recolector describe unha orde que piztu executa periodicamente en cada equipo
// para publicar un dato no mapa (o usuario conectado, o espazo libre…).
//
// A orde debe imprimir JSON nunha liña: {"campo": "valor"}. Se `Campo` está
// definido, píntase ese; se non, o primeiro que veña.
type Recolector struct {
	Orde     string `json:"orde"`
	Cada     int    `json:"cada"`  // segundos entre voltas
	Campo    string `json:"campo"` // que campo do JSON amosar
	Etiqueta string `json:"etiqueta"`
}

// CadaMinimo: un recolector conéctase por SSH a toda a aula, así que non se lle
// deixa baixar de aquí por moito que o pida o manifest.
const CadaMinimo = 10

// Segundos devolve o intervalo efectivo, respectando o mínimo.
func (r Recolector) Segundos() int {
	if r.Cada < CadaMinimo {
		return CadaMinimo
	}
	return r.Cada
}

// manifest é o modulo.json dun módulo externo.
type manifest struct {
	ID    string `json:"id"`
	Nome  string `json:"nome"`
	Icona string `json:"icona"`
	// IconaImaxe é opcional: ruta dun ficheiro de imaxe (png/svg/jpg),
	// relativa ao propio cartafol do módulo, para amosar en vez do emoji de
	// Icona nos botóns do mapa e en ⚙ Aula → Módulos. Se falta ou non se
	// pode ler, cae en Icona coma sempre (ver iconaImaxeDataURI/Externos).
	IconaImaxe string           `json:"iconaImaxe"`
	Executable string           `json:"executable"`
	Accions    []accionManifest `json:"accions"`
	Recolector *Recolector      `json:"recolector"`
	Panel      string           `json:"panel"`
	// Version identifica esta copia instalada; compárase co catálogo de
	// piztu.org para ofrecer "Actualizar". Autor/Email/Web son só informativos.
	Version  string   `json:"version"`
	Autor    string   `json:"autor"`
	Email    string   `json:"email"`
	Web      string   `json:"web"`
	Permisos []string `json:"permisos"`
	// Descricion é un texto curto opcional que explica que fai o módulo,
	// amosado en ⚙ Aula → Módulos xunto ao nome.
	Descricion string `json:"descricion"`
	// MantenPiztuAberto — ver o campo homónimo en Modulo.
	MantenPiztuAberto bool `json:"mantenPiztuAberto"`
}

// lerPermisos limpa a listaxe de permisos dun manifest (sen espazos, sen
// baleiros). NON se valida contra un catálogo pechado aquí a propósito: o
// catálogo real vive en internal/api (as capacidades que de verdade protexen
// algo); un nome descoñecido ou mal escrito simplemente non casará con
// ningunha comprobación alí e non dará acceso a nada — mesmo criterio que xa
// segue este paquete con calquera outro campo de modulo.json que non entenda.
func lerPermisos(ps []string) []string {
	var res []string
	for _, p := range ps {
		if p = strings.TrimSpace(p); p != "" {
			res = append(res, p)
		}
	}
	return res
}

// lerAccions valida e resolve as accións declaradas nun manifest. As rutas
// devólvense absolutas, relativas ao cartafol do módulo.
func lerAccions(subdir string, ms []accionManifest) []Accion {
	var res []Accion
	vistas := map[string]bool{}
	for _, a := range ms {
		id := strings.TrimSpace(a.ID)
		if id == "" || AccionsReservadas[id] || vistas[id] {
			continue // sen id, reservada ou repetida: ignórase
		}
		if a.Playbook == "" && a.Estado == "" && a.Script == "" {
			continue // non implementa nada en ningún motor
		}
		vistas[id] = true

		abs := func(f string) string {
			if f == "" {
				return ""
			}
			return filepath.Join(subdir, f)
		}
		etiqueta := strings.TrimSpace(a.Etiqueta)
		if etiqueta == "" {
			etiqueta = id
		}
		res = append(res, Accion{
			ID: id, Etiqueta: etiqueta, Icona: a.Icona,
			Playbook: abs(a.Playbook), Estado: abs(a.Estado), Script: abs(a.Script),
		})
	}
	return res
}

// iconaImaxeDataURI le `nome` (relativo a subdir, o cartafol do propio
// módulo) e devolve unha data URI autocontida, ou "" se nome está baleiro ou
// o ficheiro non se pode ler — un módulo mal configurado ou cun ficheiro
// ausente simplemente cae no emoji de Icona, non rompe nada.
func iconaImaxeDataURI(subdir, nome string) string {
	nome = strings.TrimSpace(nome)
	if nome == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(subdir, nome))
	if err != nil {
		return ""
	}
	var mime string
	switch strings.ToLower(filepath.Ext(nome)) {
	case ".svg":
		mime = "image/svg+xml"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	default:
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// Externos percorre `dir` (<base_dir>/modulos) e devolve un módulo por cada
// subcartafol cun modulo.json válido.
//
// Un manifest ilexible ou incompleto ignórase en silencio en vez de tirar a
// aplicación: un módulo de terceiros mal empaquetado non pode deixar ao profesor
// sen poder acender a aula.
func Externos(dir string) []Modulo {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil // aínda non existe o cartafol: normal nunha instalación nova
	}

	var res []Modulo
	for _, e := range entradas {
		// Un cartafol oculto non é un módulo. Actualizar() usa ".<id>.novo" e
		// ".<id>.vello" mentres troca versións; sen isto, o ".novo" (que xa
		// leva o seu modulo.json co mesmo id) aparecería coma un módulo máis
		// no intre entre a descarga e o troco.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		subdir := filepath.Join(dir, e.Name())
		// e.IsDir() usa o tipo do propio DirEntry (Lstat, sen seguir
		// symlinks): un módulo instalado coma <base_dir>/yang + symlink
		// <base_dir>/modulos/yang -> ../yang (caso de yang_installer)
		// reportaría IsDir()==false e quedaría invisible. Resolver co
		// destino real só para as entradas que son symlinks.
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(subdir); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir {
			continue
		}
		data, err := os.ReadFile(filepath.Join(subdir, "modulo.json"))
		if err != nil {
			continue
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		id := strings.TrimSpace(m.ID)
		if id == "" {
			id = e.Name() // sen id explícito vale o nome do cartafol
		}
		accions := lerAccions(subdir, m.Accions)
		rec := m.Recolector
		if rec != nil && strings.TrimSpace(rec.Orde) == "" {
			rec = nil // declarado pero sen orde: non hai nada que recoller
		}
		panel := strings.TrimSpace(m.Panel)
		if strings.TrimSpace(m.Executable) == "" && len(accions) == 0 && rec == nil && panel == "" {
			continue // nin app, nin accións, nin recolector, nin panel: non hai módulo
		}
		nome := strings.TrimSpace(m.Nome)
		if nome == "" {
			nome = id
		}
		res = append(res, Modulo{
			ID:   id,
			Nome: nome,
			// Un módulo recén soltado no cartafol NON se acende só: detéctase,
			// ofrécese en ⚙ Aula, e só aparece na barra cando o profesor o
			// activa. Coma calquera módulo, interno ou externo.
			Icona:             m.Icona,
			IconaImaxeDataURI: iconaImaxeDataURI(subdir, m.IconaImaxe),
			PorDefecto:        false,
			Executable:        m.Executable,
			Dir:               subdir,
			Accions:           accions,
			Recolector:        rec,
			Panel:             panel,
			Version:           strings.TrimSpace(m.Version),
			Autor:             strings.TrimSpace(m.Autor),
			Email:             strings.TrimSpace(m.Email),
			Web:               strings.TrimSpace(m.Web),
			Descricion:        strings.TrimSpace(m.Descricion),
			Permisos:          lerPermisos(m.Permisos),
			MantenPiztuAberto: m.MantenPiztuAberto,
		})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].ID < res[j].ID })
	return res
}

// Todos devolve os internos seguidos dos externos atopados en `dirExternos`.
// Un módulo externo que reutilice o id dun interno ignórase: se non, podería
// suplantar unha función de piztu.
func Todos(dirExternos string) []Modulo {
	res := Internos()
	vistos := map[string]bool{}
	for _, m := range res {
		vistos[m.ID] = true
	}
	for _, m := range Externos(dirExternos) {
		if vistos[m.ID] {
			continue
		}
		vistos[m.ID] = true
		res = append(res, m)
	}
	return res
}

// Buscar devolve o módulo con ese id, e se foi atopado.
func Buscar(dirExternos, id string) (Modulo, bool) {
	for _, m := range Todos(dirExternos) {
		if m.ID == id {
			return m, true
		}
	}
	return Modulo{}, false
}

// ── Estado activo/inactivo ───────────────────────────────────────────────────

// Axustes é o que precisa este paquete do almacén de axustes (internal/db). Como
// interface, os tests non precisan unha base de datos real.
type Axustes interface {
	LerAxuste(clave, defecto string) string
	GardarAxuste(clave, valor string) error
}

// ClaveActivo é a clave de persistencia do estado dun módulo.
func ClaveActivo(id string) string { return "modulo_" + id + "_activo" }

// Activo di se o módulo está acendido. Sen entrada gardada vale o seu
// PorDefecto, para que unha instalación existente non note o cambio.
func Activo(ax Axustes, m Modulo) bool {
	if ax == nil {
		return m.PorDefecto
	}
	defecto := "0"
	if m.PorDefecto {
		defecto = "1"
	}
	return ax.LerAxuste(ClaveActivo(m.ID), defecto) == "1"
}

// ActivoPorID resolve o módulo polo seu id e devolve se está activo. Un id
// descoñecido conta como inactivo: se piztu non sabe que é, non debe deixalo
// actuar.
func ActivoPorID(ax Axustes, dirExternos, id string) bool {
	m, ok := Buscar(dirExternos, id)
	if !ok {
		return false
	}
	return Activo(ax, m)
}

// Set persiste o estado dun módulo.
func Set(ax Axustes, id string, activo bool) error {
	valor := "0"
	if activo {
		valor = "1"
	}
	return ax.GardarAxuste(ClaveActivo(id), valor)
}

// ── Permisos concedidos (API de módulos, ver docs/api-modulos.md) ───────────

// ClavePermisos é a clave de persistencia dos permisos concedidos a un módulo.
func ClavePermisos(id string) string { return "modulo_" + id + "_permisos" }

// GardarPermisosConcedidos persiste os permisos concedidos a un módulo. Non
// hai un paso de "pedir" e outro de "conceder" á parte: activar o módulo en
// ⚙ Aula → Módulos XA é o consentimento (ver §5 do documento citado arriba —
// decidido así a propósito, para non amosarlle un diálogo de permisos a un
// profesorado con pouca competencia TIC que acabaría aceptándoo sen ler).
// Chámase con m.Permisos ao activar, e con nil ao desactivar.
func GardarPermisosConcedidos(ax Axustes, id string, permisos []string) error {
	data, err := json.Marshal(permisos)
	if err != nil {
		return err
	}
	return ax.GardarAxuste(ClavePermisos(id), string(data))
}

// PermisosConcedidos devolve os permisos gardados para un módulo (nil se
// nunca se activou, está desactivado, ou non pide ningún).
func PermisosConcedidos(ax Axustes, id string) []string {
	if ax == nil {
		return nil
	}
	v := ax.LerAxuste(ClavePermisos(id), "")
	if v == "" {
		return nil
	}
	var permisos []string
	if err := json.Unmarshal([]byte(v), &permisos); err != nil {
		return nil
	}
	return permisos
}
