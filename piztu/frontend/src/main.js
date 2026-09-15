import './style.css';
import './app.css';
import '@xterm/xterm/css/xterm.css';
import piztuLogo from './assets/images/piztu-logo.png';
import taoLogo from './assets/images/tao-logo.png';
import xestaLogo from './assets/images/xesta-logo.png';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';

import {
    GetEquipos, GardarLayout, Traducions, Idioma, SetIdioma, IdiomasDisponibles,
    GetMotor, SetMotor, MotorBase, MotoresDispo,
    ModulosDispo, SetModulo, InstalarModulo, ActualizarModulo, ComprobarActualizacionsModulos, AbrirModulo, RutaModulos, AbrirCartafolModulos, AccionsModulos, GetPublicacions, PanelModulo, GardarCampoModulo,
    GetActualizacionPiztu, AplicarActualizacionPiztu,
    ExecutarComando, ExecutarBloqueo, GetOpcionsBloqueo, GardarOpcionsBloqueo,
    SSHOpen, SSHInput, SSHResize, SSHClose,
    SelectFicheiros, EnviarPracticas, RecollerPracticas, ListarFicheiros, AbrirFicheiro,
    GetUmbral, GardarUmbral,
    GetEstadoTao, AbrirTao,
    IniciarRuido, DetenerRuido,
    XerarInventario, GetAulaConfig, DistribuirClaveSSH, EscanearRede, InstalarSalt, ComprobarSalt, InstalarAnsible,
    InstalarTaoClientes,
    DescubrirRede, DistribuirClaveHosts,
} from '../bindings/piztu/app';
import {Application, Events, Browser} from '@wailsio/runtime';

// ── Estado global ────────────────────────────────────────────────────────────
let I18N = {};
let equiposCache = [];
let motorActivo = 'salt';
let actualizacionPiztu = null; // { disponible, version_local, version_remota, url_descarga, notas }

function t(clave, fallback) {
    return I18N[clave] || fallback || clave;
}

// ── Marcado base ─────────────────────────────────────────────────────────────
document.querySelector('#app').innerHTML = `
<div class="navbar">
    <div class="navbar-brand">
        <img src="${piztuLogo}" alt="Piztu" class="navbar-logo">
        <h2 id="navbar-titulo">PIZTU</h2>
    </div>
    <span class="sep"></span>
    <div class="nav-group">
        <div class="toolbar-group">
            <span class="motor-wrap">
                <button id="btn-motor" class="btn-main" title="Motor de execución — mantén premido 3s para escoller (Salt / Ansible / SSH)">
                    <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="2.4"/><path d="M10 3v1.6M10 15.4V17M17 10h-1.6M4.6 10H3M14.9 5.1l-1.13 1.13M6.23 13.67 5.1 14.9M14.9 14.9l-1.13-1.13M6.23 6.23 5.1 5.1"/></svg>
                    <span id="btn-motor-label">…</span>
                    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" style="width:10px;height:10px;"><path d="M6 8l4 4 4-4"/></svg>
                </button>
                <div id="motor-menu" class="motor-menu"></div>
            </span>
        </div>
        <span class="sep"></span>
        <div class="toolbar-group">
            <button class="btn-main on btn-icon-only" id="btn-acender" title="Acender">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4v5"/><path d="M6.2 6A5.5 5.5 0 1 0 13.8 6"/></svg>
                <span id="btn-acender-label">Acender</span>
            </button>
            <button class="btn-main sleep btn-icon-only" id="btn-durmir" title="Durmir">
                <svg class="icon" viewBox="0 0 20 20" fill="currentColor"><path d="M14.5 12.4A6 6 0 0 1 7.6 5.5a6 6 0 1 0 6.9 6.9z"/></svg>
                <span id="btn-durmir-label">Durmir</span>
            </button>
            <button class="btn-main off btn-icon-only" id="btn-apagar" title="Apagar">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4v5"/><path d="M6.2 6A5.5 5.5 0 1 0 13.8 6"/></svg>
                <span id="btn-apagar-label">Apagar</span>
            </button>
            <button class="btn-main btn-icon-only" id="btn-reiniciar" title="Reiniciar">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M15.5 10a5.5 5.5 0 1 1-1.6-3.9"/><path d="M15.5 3.5v3.6h-3.6"/></svg>
                <span id="btn-reiniciar-label">Reiniciar</span>
            </button>
        </div>
        <span class="sep"></span>
        <div class="toolbar-group">
            <button class="btn-main lock" id="btn-bloqueo">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="9" width="10" height="7" rx="1.4"/><path d="M7 9V6.5a3 3 0 0 1 6 0V9"/></svg>
                <span id="btn-bloqueo-label">Bloquear</span>
            </button>
            <button class="btn-main unlock" id="btn-liberar">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="9" width="10" height="7" rx="1.4"/><path d="M7 9V6.5a3 3 0 0 1 5.7-1.3"/></svg>
                <span id="btn-liberar-label">Liberar</span>
            </button>
        </div>
        <span class="grupo-modulo" data-modulo="ficheiros">
            <span class="sep"></span>
            <div class="toolbar-group">
                <button class="btn-main btn-send" id="btn-enviar">
                    <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 13v2a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1v-2"/><path d="M10 12V4M7 7l3-3 3 3"/></svg>
                    <span id="btn-enviar-label">Enviar</span>
                </button>
                <button class="btn-main btn-collect" id="btn-recoller">
                    <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 13v2a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1v-2"/><path d="M10 4v8M7 9l3 3 3-3"/></svg>
                    <span id="btn-recoller-label">Recoller</span>
                </button>
            </div>
        </span>
        <span class="grupo-modulo" id="grupo-apps">
            <span class="sep"></span>
            <div class="toolbar-group">
                <button class="btn-main btn-tao" id="btn-tao" data-modulo="tao" title="Tao — mantén premido 3s para instalar nos equipos da aula"><img src="${taoLogo}" alt="" class="btn-tao-logo"><span id="btn-tao-label">Tao</span></button>
                <button class="btn-main" id="btn-xesta" data-modulo="xesta"><img src="${xestaLogo}" alt="" class="btn-tao-logo"><span>Xesta</span></button>
                <span id="modulos-externos"></span>
            </div>
        </span>
        <span id="accions-modulos"></span>
    </div>
    <div class="toolbar-right">
        <div class="noise-box" data-modulo="ruido">
            <span id="btn-mic" title="Monitor de ruído — mantén premido 3s para activar/desactivar">
                <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="7.5" y="3" width="5" height="8" rx="2.5"/><path d="M5 9.5a5 5 0 0 0 10 0"/><path d="M10 14.5V17M7.5 17h5"/></svg>
            </span>
            <div class="vumetro-bg"><div id="vumetro"></div></div>
            <input type="range" id="limiar" min="5" max="100" value="30">
            <b id="valorLimiar">—</b>
        </div>
        <button class="btn-main" id="btn-config" title="Configuración da aula">
            <svg class="icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="2.4"/><path d="M10 3v1.6M10 15.4V17M17 10h-1.6M4.6 10H3M14.9 5.1l-1.13 1.13M6.23 13.67 5.1 14.9M14.9 14.9l-1.13-1.13M6.23 6.23 5.1 5.1"/></svg>
            <span id="btn-config-label">Aula</span>
        </button>
        <button class="btn-main" id="btn-sobre" title="Sobre Piztu">
            <svg class="icon" viewBox="0 0 20 20"><circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" stroke-width="1.6"/><rect x="9.25" y="8.5" width="1.5" height="5" rx="0.75" fill="currentColor"/><rect x="9.25" y="5.8" width="1.5" height="1.5" rx="0.75" fill="currentColor"/></svg>
        </button>
    </div>
</div>
<div id="escenario"></div>

<div id="log-panel">
    <div class="log-header">
        <span>▸ <span id="log-titulo">Terminal</span></span>
        <div class="log-header-actions">
            <button id="btn-clear-log" title="Limpar o rexistro">
                <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 5.5h11"/><path d="M8 5.5V4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v1.5"/><path d="M5.5 5.5 6.2 16a1 1 0 0 0 1 .9h5.6a1 1 0 0 0 1-.9l.7-10.5"/><path d="M8.3 8.5v5M11.7 8.5v5"/></svg>
            </button>
            <button id="btn-toggle-log" title="Minimizar/ampliar">−</button>
        </div>
    </div>
    <div id="log-output"></div>
</div>

<div id="ssh-modal" class="modal-overlay">
    <div class="modal-box" id="ssh-box">
        <div class="modal-titlebar">
            <span id="ssh-titulo-prefix">🖥 Terminal SSH — </span><span id="ssh-host-label">—</span>
            <button class="modal-close" id="ssh-close">✕</button>
        </div>
        <div id="ssh-terminal"></div>
    </div>
</div>

<div id="envio-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="envio-titulo">📤 Enviar práctica aos equipos</span>
            <button class="modal-close" data-close="envio-modal">✕</button>
        </div>
        <div class="modal-body">
            <button class="btn-main btn-send" id="btn-engadir-ficheiros" style="width:100%;margin-bottom:10px;">➕ Engadir ficheiros…</button>
            <ul id="lista-ficheiros-envio"></ul>
            <div class="destino-box">
                <label class="lbl-destino" id="envio-destino-label">Destino:</label>
                <label><input type="radio" name="destino" value="todos" checked> <span id="envio-destino-todos">Todos os equipos</span></label>
                <label><input type="radio" name="destino" value="seleccion"> <span id="envio-destino-manual">Selección manual</span></label>
                <div id="seleccion-manual"></div>
            </div>
            <button class="btn-main btn-send" style="width:100%; padding:10px;" id="btn-enviar-now">📤 Enviar agora</button>
            <div class="progress-container" id="progreso-envio"></div>
        </div>
    </div>
</div>

<div id="recollida-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="recollida-titulo">📥 Recollendo traballos...</span>
            <button class="modal-close" data-close="recollida-modal">✕</button>
        </div>
        <div class="modal-body">
            <p class="modal-hint" id="recollida-sftp">Conectando por SFTP a cada equipo e descargando o cartafol <code>~/Desktop/practicas/</code>.</p>
            <div class="progress-container" id="progreso-recollida"></div>
        </div>
    </div>
</div>

<div id="explorer-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="explorer-titulo-prefix">📁 Traballos de </span><span id="explorer-host">—</span>
            <button class="modal-close" data-close="explorer-modal">✕</button>
        </div>
        <div class="modal-body" style="padding:0;">
            <ul id="explorer-list"></ul>
        </div>
    </div>
</div>

<div id="sobre-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="sobre-titulo">Sobre Piztu Sistemas</span>
            <button class="modal-close" data-close="sobre-modal">✕</button>
        </div>
        <div class="modal-body">
            <img src="${piztuLogo}" alt="Piztu" class="sobre-logo">
            <p id="sobre-version" class="modal-hint"></p>
            <p id="sobre-actualizacion" class="modal-hint" style="display:none; color:var(--actualizacion); font-weight:600;"></p>
            <button id="btn-sobre-actualizar" class="btn-main" style="display:none; margin:4px 0 8px;"></button>
            <p id="sobre-autor"></p>
            <p><a href="#" id="sobre-web" style="color:var(--primary); text-decoration:underline; cursor:pointer;"></a></p>
            <p id="sobre-contacto"></p>
            <p id="sobre-licenza" style="font-weight:600; margin-top:10px;"></p>
            <p id="sobre-licdesc" class="modal-hint" style="white-space:pre-line;"></p>
        </div>
    </div>
</div>

<div id="panel-modulo-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="panel-modulo-titulo">Módulo</span>
            <button class="modal-close" data-close="panel-modulo-modal">✕</button>
        </div>
        <div class="modal-body" id="panel-modulo-corpo"></div>
    </div>
</div>

<div id="config-modal" class="modal-overlay">
    <div class="modal-box config-box">
        <div class="modal-titlebar">
            <span id="config-titulo">⚙ Configuración da aula (Debian)</span>
            <button class="modal-close" data-close="config-modal">✕</button>
        </div>
        <div class="modal-body">
            <div class="config-layout">
            <div class="config-grid">

                <div class="config-card">
                    <h4><span class="step-num">1</span><span id="cfg-p1-titulo">Grupo</span></h4>
                    <p class="modal-hint" id="cfg-p1-desc">Define os equipos do grupo (aula, oficina...). Xérase o inventario e actualízase o mapa.</p>
                    <div class="field">
                        <label class="field-label" id="cfg-p1-prefixo-label">Prefixo do nome</label>
                        <input type="text" id="cfg-prefixo" value="tux" autocapitalize="off" autocorrect="off" spellcheck="false">
                    </div>
                    <div class="field">
                        <label class="field-label" id="cfg-p1-dominio-label">Dominio</label>
                        <input type="text" id="cfg-dominio" value=".local">
                    </div>
                    <div class="field">
                        <label class="field-label" id="cfg-p1-nequipos-label">Número de equipos</label>
                        <input type="number" id="cfg-nequipos" value="20" min="1" max="99">
                    </div>
                    <p class="modal-hint config-mono" id="cfg-previa"></p>
                    <button class="btn-main on config-btn" id="btn-xerar-inventario">Xerar inventario</button>
                    <p class="modal-hint" id="cfg-resultado"></p>
                    <button class="btn-main config-btn config-btn-secondary" id="btn-escaneo-avanzado">🔍 Escaneo avanzado de rede</button>
                </div>

                <div class="config-card">
                    <h4><span class="step-num">2</span><span id="cfg-p2-titulo">Clave SSH</span></h4>
                    <p class="modal-hint" id="cfg-p2-desc">Créase aquí e envíase aos equipos (append a <code>authorized_keys</code> e copia a <code>/etc/skel</code>). Precisa o contrasinal do usuario dos equipos, unha soa vez — reutilízase nas tarxetas de Salt e Ansible.</p>
                    <div class="field">
                        <label class="field-label" id="cfg-p2-usuario-label">Usuario dos equipos</label>
                        <input type="text" id="cfg-ssh-user" value="usuario" autocapitalize="off" autocorrect="off" spellcheck="false">
                    </div>
                    <div class="field">
                        <label class="field-label" id="cfg-p2-pass-label">Contrasinal (sudo)</label>
                        <input type="password" id="cfg-ssh-pass">
                    </div>
                    <button class="btn-main lock config-btn" id="btn-distribuir-clave">Crear e enviar clave SSH</button>
                    <p class="modal-hint config-pre" id="cfg-ssh-resultado"></p>
                </div>

                <div class="config-card">
                    <h4><span class="step-num">3</span><span id="cfg-p3-titulo">Rede</span></h4>
                    <p class="modal-hint" id="cfg-p3-desc">Detecta os equipos acendidos (por SSH) e garda a súa MAC no inventario, para poder acendelos por Wake-on-LAN.</p>
                    <button class="btn-main on config-btn" id="btn-escanear-rede">Escanear rede e gardar MACs</button>
                    <p class="modal-hint" id="cfg-scan-resultado"></p>
                </div>

                <div class="config-card">
                    <h4><span class="step-num">4</span><span id="cfg-p4-titulo">Instalar Salt</span></h4>
                    <p class="modal-hint" id="cfg-p4-desc"><code>salt-master</code> neste equipo e <code>salt-minion</code> en cada cliente por SSH (repositorio oficial de SaltProject para Debian). Usa o usuario e contrasinal da tarxeta 2. O enderezo do master calcúlase só; cámbiao unicamente se o master está noutra máquina.</p>
                    <div class="field">
                        <label class="field-label" id="cfg-p4-master-label">Enderezo do master (automático)</label>
                        <input type="text" id="cfg-salt-master" autocapitalize="off" autocorrect="off" spellcheck="false">
                    </div>
                    <button class="btn-main sleep config-btn" id="btn-instalar-salt">Instalar Salt (master + minions)</button>
                    <button class="btn-main config-btn" id="btn-comprobar-salt">Comprobar Salt</button>
                    <p class="modal-hint" id="cfg-salt-resultado"></p>
                    <pre class="modal-hint" id="cfg-salt-diagnostico" style="white-space:pre-wrap;margin:0"></pre>
                </div>

                <div class="config-card">
                    <h4><span class="step-num">5</span><span id="cfg-p5-titulo">Instalar Ansible</span></h4>
                    <p class="modal-hint" id="cfg-p5-desc">Instala o paquete <code>ansible</code> con apt neste equipo, para habilitar o motor Ansible. Usa o contrasinal da tarxeta 2.</p>
                    <button class="btn-main btn-collect config-btn" id="btn-instalar-ansible">Instalar Ansible (apt)</button>
                    <p class="modal-hint" id="cfg-ansible-resultado"></p>
                </div>

            </div>

                <div class="config-card config-modulos">
                    <div class="config-idioma-inline">
                        <h4 id="cfg-idioma-titulo">Idioma da interface</h4>
                        <p class="modal-hint" id="cfg-idioma-hint">Cambia o idioma de Piztu. Os módulos xa abertos non se recargan só por isto.</p>
                        <div class="field">
                            <select id="cfg-idioma"></select>
                        </div>
                    </div>
                    <h4 id="cfg-modulos-titulo">Módulos</h4>
                    <button class="btn-main config-btn config-btn-secondary" id="btn-comprobar-actualizacions">🔄 Comprobar actualizacións</button>
                    <div id="lista-modulos" class="lista-modulos-grid"></div>
                    <code id="ruta-modulos" class="ruta-modulos">—</code>
                    <button class="btn-main config-btn config-btn-secondary" id="btn-abrir-modulos">📂 Abrir o cartafol de módulos</button>
                </div>

            </div>
        </div>
    </div>
</div>

<div id="scan-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="scan-titulo">🔍 Escaneo avanzado de rede</span>
            <button class="modal-close" data-close="scan-modal">✕</button>
        </div>
        <div class="modal-body">
            <p class="modal-hint" id="scan-desc">Escanea a rede local, marca os equipos que queres xestionar e escribe o seu contrasinal para enviarlles a clave SSH. Os nomes e contrasinais poden ser calquera — non fai falta seguir o esquema prefixo+número.</p>

            <button class="btn-main on" style="width:100%; padding:10px;" id="btn-scan-iniciar">Escanear rede local</button>
            <p class="modal-hint" id="scan-resultado" style="margin-top:8px;"></p>

            <label class="lbl-destino" id="scan-usuario-label" style="margin-top:12px; display:block;">Usuario nos equipos</label>
            <input type="text" id="scan-usuario" value="usuario" autocapitalize="off" autocorrect="off" spellcheck="false" style="width:100%; margin-bottom:8px;">

            <div id="lista-descubertos" class="lista-descubertos"></div>

            <button class="btn-main lock" style="width:100%; padding:10px; margin-top:10px;" id="btn-scan-distribuir" disabled>🔑 Distribuír clave aos seleccionados</button>
            <div id="progreso-scan" style="margin-top:8px;"></div>
        </div>
    </div>
</div>

<div id="bloqueo-config-modal" class="modal-overlay">
    <div class="modal-box">
        <div class="modal-titlebar">
            <span id="bloqueo-titulo">🔒 Configurar bloqueo</span>
            <button class="modal-close" data-close="bloqueo-config-modal">✕</button>
        </div>
        <div class="modal-body">
            <p class="modal-hint" id="bloqueo-desc">Escolle que se bloquea cando premas "Bloquear" cun clic simple. Lémbrase entre sesións.</p>
            <label class="chk-bloqueo-opcion"><input type="checkbox" id="chk-bloq-internet"> <span id="bloqueo-op-internet">Conexión a internet</span></label>
            <label class="chk-bloqueo-opcion"><input type="checkbox" id="chk-bloq-ssh"> <span id="bloqueo-op-ssh">SSH</span></label>
            <label class="chk-bloqueo-opcion"><input type="checkbox" id="chk-bloq-son"> <span id="bloqueo-op-son">Son</span></label>
            <label class="chk-bloqueo-opcion"><input type="checkbox" id="chk-bloq-rato"> <span id="bloqueo-op-rato">Rato</span></label>
            <label class="chk-bloqueo-opcion"><input type="checkbox" id="chk-bloq-teclado"> <span id="bloqueo-op-teclado">Teclado</span></label>
            <button class="btn-main on" id="btn-bloqueo-config-gardar" style="width:100%; margin-top:12px; padding:10px;">Gardar</button>
        </div>
    </div>
</div>
`;

const escenario = document.getElementById('escenario');

// ── i18n ─────────────────────────────────────────────────────────────────────
// aplicarTraducions() actualiza TODOS os textos traducibles dun golpe, tanto
// da xanela principal coma do modal de Configuración da aula e do de Sobre
// Piztu — todo vive no mesmo #app, así que un cambio de idioma reflíctese
// decontado en calquera modal xa aberto sen recargar nada.
//
// Envolta nun try/catch: se algún elemento novo (engadido ao HTML pero sen
// entrada en `t()`, ou viceversa) fixese fallar UNHA soa liña, antes
// abortaba TODO o resto da función a partir dese punto en silencio (unha
// asignación a document.getElementById(...).textContent sobre `null` lanza
// TypeError) — daba a falsa impresión de que "cambia a xanela principal
// pero non o modal de Configuración", cando en realidade era un fallo a
// medio camiño que cortaba xustamente aí. Agora rexístrase no panel de log
// e o resto de textos non implicados séguense actualizando.
function aplicarTraducions() {
  try {
    aplicarTraducionsInterno();
  } catch (err) {
    log('[IDIOMA] ' + t('log.idioma.erroaplicando', 'erro aplicando traducións:') + ' ' + err);
  }
}

function aplicarTraducionsInterno() {
    document.getElementById('navbar-titulo').textContent = t('web.navbar', 'PIZTU');
    // Os botóns fixos da navbar levan agora unha icona SVG propia (ver marcado
    // base máis arriba) — .textContent xa non vai directo ao botón, senón a
    // un <span>-label irmán da icona, para non borrala en cada troco de idioma.
    document.getElementById('btn-acender-label').textContent = t('web.acender', 'Acender');
    document.getElementById('btn-acender').title = t('web.acender', 'Acender');
    document.getElementById('btn-durmir-label').textContent = t('web.durmir', 'Durmir');
    document.getElementById('btn-durmir').title = t('web.durmir', 'Durmir');
    document.getElementById('btn-apagar-label').textContent = t('web.apagar', 'Apagar');
    document.getElementById('btn-apagar').title = t('web.apagar', 'Apagar');
    document.getElementById('btn-reiniciar-label').textContent = t('web.reiniciar', 'Reiniciar');
    document.getElementById('btn-reiniciar').title = t('web.reiniciar', 'Reiniciar');
    document.getElementById('btn-bloqueo-label').textContent = t('web.bloqueo', 'Bloquear');
    document.getElementById('btn-liberar-label').textContent = t('web.liberar', 'Liberar');
    cargarModulos();  // repintar a lista co idioma xa cargado
    document.getElementById('btn-enviar-label').textContent = t('web.enviar', 'Enviar');
    document.getElementById('btn-recoller-label').textContent = t('web.recoller', 'Recoller');
    document.getElementById('envio-titulo').textContent = t('web.envio.titulo', '📤 Enviar práctica aos equipos');
    document.getElementById('btn-tao-label').textContent = t('tao.btn', 'Tao');
    document.getElementById('btn-sobre').title = t('sobre.btn', 'ℹ Sobre Piztu Sistemas');
    document.getElementById('btn-config-label').textContent = t('web.config.btn', 'Aula');
    document.getElementById('btn-config').title = t('web.config.tip', 'Configuración da aula');
    document.getElementById('config-titulo').textContent = t('web.config.titulo', '⚙ Configuración da aula (Debian)');
    document.getElementById('cfg-idioma-titulo').textContent = t('web.config.idioma.titulo', 'Idioma da interface');
    document.getElementById('cfg-idioma-hint').textContent = t('web.config.idioma.hint',
        'Cambia o idioma de Piztu. Os módulos xa abertos non se recargan só por isto.');
    document.getElementById('btn-clear-log').title = t('web.log.limpar', 'Limpar o rexistro');
    document.getElementById('btn-toggle-log').title = t('web.log.toggle', 'Minimizar/ampliar');
    document.getElementById('ssh-titulo-prefix').textContent = t('web.ssh.titulo', '🖥 Terminal SSH —') + ' ';
    document.getElementById('envio-destino-label').textContent = t('web.destino', 'Destino:');
    document.getElementById('envio-destino-todos').textContent = t('web.todos', 'Todos os equipos');
    document.getElementById('envio-destino-manual').textContent = t('web.selmanual', 'Selección manual');
    document.getElementById('btn-enviar-now').textContent = t('web.enviaragora', '📤 Enviar agora');
    document.getElementById('btn-engadir-ficheiros').textContent = t('web.envio.engadir', '➕ Engadir ficheiros…');
    document.getElementById('recollida-titulo').textContent = t('web.recollendo', '📥 Recollendo traballos...');
    document.getElementById('recollida-sftp').innerHTML = t('web.sftp',
        'Conectando por SFTP a cada equipo e descargando o cartafol <code>~/Desktop/practicas/</code>.');
    document.getElementById('explorer-titulo-prefix').textContent = t('web.explorer.titulo', '📁 Traballos de') + ' ';
    document.getElementById('scan-titulo').textContent = t('web.scan.titulo', '🔍 Escaneo avanzado de rede');
    document.getElementById('scan-desc').textContent = t('web.scan.desc',
        'Escanea a rede local, marca os equipos que queres xestionar e escribe o seu contrasinal para enviarlles a clave SSH. Os nomes e contrasinais poden ser calquera — non fai falta seguir o esquema prefixo+número.');
    document.getElementById('btn-scan-iniciar').textContent = t('web.scan.iniciar', 'Escanear rede local');
    document.getElementById('scan-usuario-label').textContent = t('web.scan.usuario', 'Usuario nos equipos');
    document.getElementById('btn-scan-distribuir').textContent = t('web.scan.distribuir', '🔑 Distribuír clave aos seleccionados');
    document.getElementById('bloqueo-titulo').textContent = t('web.bloqueo.titulo', '🔒 Configurar bloqueo');
    document.getElementById('bloqueo-desc').textContent = t('web.bloqueo.desc',
        'Escolle que se bloquea cando premas "Bloquear" cun clic simple. Lémbrase entre sesións.');
    document.getElementById('bloqueo-op-internet').textContent = t('web.bloqueo.internet', 'Conexión a internet');
    document.getElementById('bloqueo-op-ssh').textContent = t('web.bloqueo.ssh', 'SSH');
    document.getElementById('bloqueo-op-son').textContent = t('web.bloqueo.son', 'Son');
    document.getElementById('bloqueo-op-rato').textContent = t('web.bloqueo.rato', 'Rato');
    document.getElementById('bloqueo-op-teclado').textContent = t('web.bloqueo.teclado', 'Teclado');
    document.getElementById('btn-bloqueo-config-gardar').textContent = t('web.gardar', 'Gardar');
    document.getElementById('cfg-p1-titulo').textContent = t('web.config.p1.titulo', 'Grupo');
    document.getElementById('cfg-p1-desc').textContent = t('web.config.p1.desc',
        'Define os equipos do grupo (aula, oficina...). Xérase o inventario e actualízase o mapa.');
    document.getElementById('cfg-p1-prefixo-label').textContent = t('web.config.p1.prefixo', 'Prefixo do nome');
    document.getElementById('cfg-p1-dominio-label').textContent = t('web.config.p1.dominio', 'Dominio');
    document.getElementById('cfg-p1-nequipos-label').textContent = t('web.config.p1.nequipos', 'Número de equipos');
    document.getElementById('btn-xerar-inventario').textContent = t('web.config.p1.xerar', 'Xerar inventario');
    document.getElementById('btn-escaneo-avanzado').textContent = t('web.scan.titulo', '🔍 Escaneo avanzado de rede');
    document.getElementById('cfg-p2-titulo').textContent = t('web.config.p2.titulo', 'Clave SSH');
    document.getElementById('cfg-p2-desc').innerHTML = t('web.config.p2.desc',
        'Créase aquí e envíase aos equipos (append a <code>authorized_keys</code> e copia a <code>/etc/skel</code>). Precisa o contrasinal do usuario dos equipos, unha soa vez — reutilízase nas tarxetas de Salt e Ansible.');
    document.getElementById('cfg-p2-usuario-label').textContent = t('web.config.p2.usuario', 'Usuario dos equipos');
    document.getElementById('cfg-p2-pass-label').textContent = t('web.config.p2.pass', 'Contrasinal (sudo)');
    document.getElementById('btn-distribuir-clave').textContent = t('web.config.p2.distribuir', 'Crear e enviar clave SSH');
    document.getElementById('cfg-p3-titulo').textContent = t('web.config.p3.titulo', 'Rede');
    document.getElementById('cfg-p3-desc').textContent = t('web.config.p3.desc',
        'Detecta os equipos acendidos (por SSH) e garda a súa MAC no inventario, para poder acendelos por Wake-on-LAN.');
    document.getElementById('btn-escanear-rede').textContent = t('web.config.p3.escanear', 'Escanear rede e gardar MACs');
    document.getElementById('cfg-p4-titulo').textContent = t('web.config.p4.titulo', 'Instalar Salt');
    document.getElementById('cfg-p4-desc').innerHTML = t('web.config.p4.desc',
        '<code>salt-master</code> neste equipo e <code>salt-minion</code> en cada cliente por SSH (repositorio oficial de SaltProject para Debian). Usa o usuario e contrasinal da tarxeta 2. O enderezo do master calcúlase só; cámbiao unicamente se o master está noutra máquina.');
    document.getElementById('cfg-p4-master-label').textContent = t('web.config.p4.master', 'Enderezo do master (automático)');
    document.getElementById('btn-instalar-salt').textContent = t('web.config.p4.instalar', 'Instalar Salt (master + minions)');
    document.getElementById('btn-comprobar-salt').textContent = t('web.config.p4.comprobar', 'Comprobar Salt');
    document.getElementById('cfg-p5-titulo').textContent = t('web.config.p5.titulo', 'Instalar Ansible');
    document.getElementById('cfg-p5-desc').innerHTML = t('web.config.p5.desc',
        'Instala o paquete <code>ansible</code> con apt neste equipo, para habilitar o motor Ansible. Usa o contrasinal da tarxeta 2.');
    document.getElementById('btn-instalar-ansible').textContent = t('web.config.p5.instalar', 'Instalar Ansible (apt)');
    document.getElementById('cfg-modulos-titulo').textContent = t('modulos.titulo', 'Módulos');
    document.getElementById('btn-comprobar-actualizacions').textContent = t('web.config.modulos.comprobar', '🔄 Comprobar actualizacións');
    document.getElementById('btn-abrir-modulos').textContent = t('web.config.modulos.abrir', '📂 Abrir o cartafol de módulos');
    document.getElementById('btn-mic').title = t('web.ruido.tip', 'Monitor de ruído — mantén premido 3s para activar/desactivar');
    document.getElementById('btn-motor').title = t('web.motor.tip', 'Motor de execución — mantén premido 3s para escoller (Salt / Ansible / SSH)');
    document.getElementById('btn-tao').title = t('web.tao.tip', 'Tao — mantén premido 3s para instalar nos equipos da aula');
    // Reservas explícitas en todos os campos: t(clave, '') devolvía a propia
    // clave cando non hai traducións cargadas, e esta pantalla amosaba
    // "sobre.version", "sobre.autor"… en cru. Ningún t() debería quedar sen
    // texto de reserva por este motivo.
    document.getElementById('sobre-titulo').textContent = t('sobre.titulo', 'Sobre Piztu Sistemas');
    document.getElementById('sobre-autor').textContent = t('sobre.autor', 'Autor: Martín Pardiñas');
    document.getElementById('sobre-web').textContent = t('sobre.web', 'Web: piztu.org');
    document.getElementById('sobre-contacto').textContent = t('sobre.contacto', 'Contacto: piztu@omeu.gal');
    document.getElementById('sobre-licenza').textContent = t('sobre.licenza', 'Licenza GNU Affero General Public License v3 (AGPLv3)');
    document.getElementById('sobre-licdesc').textContent = t('sobre.licdesc',
        'Software libre: pódese usar, estudar, modificar e redistribuír libremente. Calquera traballo derivado que se redistribúa — incluído o uso a través da rede — ten que publicarse tamén baixo AGPLv3, co código fonte dispoñible (copyleft). Texto completo no ficheiro LICENSE.');
    pintarSobreActualizacion();
    setResumen(t('web.listo', 'Listo.'));
}

// pintarSelectorIdioma enche o <select> de ⚙ Configuración cos idiomas
// dispoñibles (IdiomasDisponibles, cada un co seu propio .lang en
// recursos/idiomas/) e marca o activo — chámase cada vez que se abre o
// modal, non só unha vez, por se o idioma cambiou noutro sitio.
function pintarSelectorIdioma() {
    const sel = document.getElementById('cfg-idioma');
    Promise.all([IdiomasDisponibles(), Idioma()]).then(([idiomas, activo]) => {
        sel.innerHTML = '';
        idiomas.forEach((idi) => {
            const opt = document.createElement('option');
            opt.value = idi.code;
            opt.textContent = idi.nome;
            if (idi.code === activo) opt.selected = true;
            sel.appendChild(opt);
        });
    }).catch(() => {});
}

// Cambiar de idioma refresca decontado toda a interface (I18N global +
// aplicarTraducions()) — sen recargar a páxina. Tao, Pancho e Xesta (xanela
// propia, proceso novo por lanzamento) non se enteran se xa están abertos:
// só herdan o idioma novo os que se abran a partir de agora (ver
// ContextoModulo.Idioma en piztu/app.go).
document.getElementById('cfg-idioma').addEventListener('change', (ev) => {
    SetIdioma(ev.target.value).then((traducions) => {
        I18N = traducions;
        aplicarTraducions();
    }).catch((err) => log('[IDIOMA] ' + err));
});

// ── Actualización de Piztu (← botón "i" da navbar) ──────────────────────────
// O backend comproba en fondo ao arrincar (evento actualizacion_piztu_cambiada,
// abaixo); aquí só se pinta o que xa se sabe.
function pintarSobreActualizacion() {
    document.getElementById('sobre-version').textContent =
        t('sobre.version', 'Versión') + ' ' + (actualizacionPiztu ? actualizacionPiztu.version_local : '');

    const btnSobre = document.getElementById('btn-sobre');
    const dispo = !!(actualizacionPiztu && actualizacionPiztu.disponible);
    btnSobre.classList.toggle('btn-actualizacion-dispo', dispo);

    const aviso = document.getElementById('sobre-actualizacion');
    const btn = document.getElementById('btn-sobre-actualizar');
    if (dispo) {
        aviso.textContent = t('sobre.novaversion', 'Nova versión dispoñible: %s')
            .replace('%s', actualizacionPiztu.version_remota);
        aviso.style.display = '';
        btn.style.display = '';
        btn.disabled = false;
        btn.textContent = t('sobre.actualizar', '⟳ Actualizar');
    } else {
        aviso.style.display = 'none';
        btn.style.display = 'none';
    }
}

function aplicarActualizacionPiztu(estado) {
    actualizacionPiztu = estado || null;
    pintarSobreActualizacion();
}

// A píldora "Listo." da navbar eliminouse (non aportaba nada, a pedimento do
// usuario) — mantense esta función coma no-op para non ter que tocar cada un
// dos puntos de chamada (progreso de envío/recollida, zoom, resultado dun
// comando...); eses mesmos eventos xa se ven no panel de log.
function setResumen(txt) {}

// ── Panel de log (← log_output/btn_toggle_term de main.py) ──────────────────
const LOG_MAX_LINEAS = 500;
const logOutput = document.getElementById('log-output');

function log(txt) {
    const linea = document.createElement('div');
    linea.textContent = txt;
    logOutput.appendChild(linea);
    while (logOutput.childElementCount > LOG_MAX_LINEAS) {
        logOutput.removeChild(logOutput.firstChild);
    }
    logOutput.scrollTop = logOutput.scrollHeight;
}

const logPanel = document.getElementById('log-panel');
const btnToggleLog = document.getElementById('btn-toggle-log');
btnToggleLog.addEventListener('click', () => {
    const colapsado = logPanel.classList.toggle('collapsed');
    btnToggleLog.textContent = colapsado ? '+' : '−';
});

const btnClearLog = document.getElementById('btn-clear-log');
btnClearLog.addEventListener('click', () => {
    logOutput.innerHTML = '';
});

// ── Motor de execución (Salt / Ansible / SSH) ─────────────────────────────
// PRESION_MOTOR_MS ségueno usando outros botóns de premido longo (micrófono,
// bloqueo…); o selector de motor xa non: agora abre un menú co clic normal.
const PRESION_MOTOR_MS = 3000;
const btnMotor = document.getElementById('btn-motor');
const menuMotor = document.getElementById('motor-menu');
// Motor "base" = o defecto do SO (ssh en macOS, salt en Linux). Consérvase
// para saber cal recomendar no menú.
let motorBase = 'salt';
const NOMES_MOTOR = {salt: 'Salt', ansible: 'Ansible', ssh: 'SSH'};
// Unha liña de axuda por motor: sobre todo "SSH" precisa explicación.
const DESC_MOTOR = {
    salt: () => t('web.motor.desc.salt',
        'salt-master neste equipo + salt-minion en cada cliente. O máis rápido en aulas grandes.'),
    ansible: () => t('web.motor.desc.ansible',
        'Ansible por SSH. Precisa o paquete «ansible» instalado (⚙ Aula → Instalar Ansible).'),
    ssh: () => t('web.motor.desc.ssh',
        'SSH directo desde Piztu, sen Salt, Ansible nin servidor. Fai as accións de serie; non executa scripts xerados por módulos como Xesta.'),
};

function pintarMotor(nome) {
    motorActivo = nome;
    if (nome !== 'ansible') motorBase = nome;
    document.getElementById('btn-motor-label').textContent = NOMES_MOTOR[nome] || nome;
}
function cargarMotor() {
    GetMotor().then(pintarMotor).catch(() => {});
}
function enviarMotor(nome) {
    SetMotor(nome).then(pintarMotor).catch((err) => log('[MOTOR] ' + err));
}

// ── Menú de motores ──────────────────────────────────────────────────────────
// Manter premido #btn-motor 3s (botón esquerdo) abre o menú emerxente coas
// tres opcións — mesmo xesto ca #btn-mic / #btn-bloqueo. O clic curto non fai
// nada. Os motores que non se poden usar neste equipo saen en gris co motivo
// (ver MotoresDispo). O menú é `position: fixed` e colócao colocarMenuMotor()
// porque a .navbar (overflow-x: auto) recortaría un menú `absolute`.
let temporizadorMotor = null;

function pecharMenuMotor() {
    menuMotor.classList.remove('visible');
    btnMotor.setAttribute('aria-expanded', 'false');
}

// colocarMenuMotor sitúa o menú (position: fixed) xusto debaixo do botón,
// aliñando os bordos dereitos e sen que se saia da ventá. Faise en JS porque
// a .navbar recorta calquera menú `absolute` (ten overflow-x: auto).
function colocarMenuMotor() {
    const r = btnMotor.getBoundingClientRect();
    const w = menuMotor.offsetWidth;
    let left = r.right - w;                                   // bordos dereitos aliñados
    left = Math.max(8, Math.min(left, window.innerWidth - w - 8));
    menuMotor.style.top = Math.round(r.bottom + 6) + 'px';
    menuMotor.style.left = Math.round(left) + 'px';
}

function abrirMenuMotor() {
    MotoresDispo().then((motores) => {
        menuMotor.innerHTML = '';
        motores.forEach((m) => {
            const item = document.createElement('button');
            item.className = 'motor-item';
            item.type = 'button';
            item.disabled = !m.dispo;
            if (m.nome === motorActivo) item.classList.add('activo');
            item.title = m.dispo ? '' : m.motivo;
            const marca = m.nome === motorActivo ? '●' : '○';
            const nota = m.dispo
                ? (m.recomendar ? ' <i>(recomendado)</i>' : '')
                : ' <i>— ' + m.motivo + '</i>';
            const desc = DESC_MOTOR[m.nome] ? DESC_MOTOR[m.nome]() : '';
            item.innerHTML =
                '<span class="motor-marca">' + marca + '</span>' +
                '<span class="motor-texto">' +
                    '<span class="motor-nome">' + m.etiqueta + nota + '</span>' +
                    (desc ? '<span class="motor-desc">' + desc + '</span>' : '') +
                '</span>';
            item.addEventListener('click', () => {
                pecharMenuMotor();
                if (m.dispo && m.nome !== motorActivo) enviarMotor(m.nome);
            });
            menuMotor.appendChild(item);
        });
        menuMotor.classList.add('visible');   // antes de medir: display:none daría ancho 0
        colocarMenuMotor();
        btnMotor.setAttribute('aria-expanded', 'true');
    }).catch((err) => log('[MOTOR] ' + err));
}

// Se cambia o tamaño da ventá co menú aberto, recolócao (ou péchao ao rolar).
window.addEventListener('resize', () => {
    if (menuMotor.classList.contains('visible')) colocarMenuMotor();
});

btnMotor.setAttribute('aria-haspopup', 'menu');
btnMotor.setAttribute('aria-expanded', 'false');

function iniciarPresionMotor() {
    if (menuMotor.classList.contains('visible')) return;
    btnMotor.classList.add('pressing');
    temporizadorMotor = setTimeout(() => {
        temporizadorMotor = null;
        btnMotor.classList.remove('pressing');
        abrirMenuMotor();
    }, PRESION_MOTOR_MS);
}
function cancelarPresionMotor() {
    if (temporizadorMotor) {
        clearTimeout(temporizadorMotor);
        temporizadorMotor = null;
    }
    btnMotor.classList.remove('pressing');
}
// Só botón esquerdo (ev.button === 0); co dereito ignórase.
btnMotor.addEventListener('mousedown', (ev) => { if (ev.button === 0) iniciarPresionMotor(); });
btnMotor.addEventListener('touchstart', iniciarPresionMotor, {passive: true});
btnMotor.addEventListener('mouseup', cancelarPresionMotor);
btnMotor.addEventListener('mouseleave', cancelarPresionMotor);
btnMotor.addEventListener('touchend', cancelarPresionMotor);
btnMotor.addEventListener('touchcancel', cancelarPresionMotor);

// Pechar ao clicar fóra ou con Esc.
document.addEventListener('click', (ev) => {
    if (!menuMotor.classList.contains('visible')) return;
    if (menuMotor.contains(ev.target) || btnMotor.contains(ev.target)) return;
    pecharMenuMotor();
});
document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape') pecharMenuMotor();
});

Events.On('motor_cambiado', (e) => ((d) => {
    pintarMotor(d.motor);
    log('[MOTOR] ' + t('log.motor.cambiado', 'cambiado a') + ' ' + d.motor);
    // As accións ofrecidas dependen do motor: cada un resolve un tipo de
    // ficheiro distinto (.yaml, .sls ou .sh).
    if (d.accions) pintarAccionsModulos(d.accions);
})(e.data));

// ── Módulos (os "apeiros": ruído, ficheiros, Tao…) ──────────────────────────
// Piztu é a máquina do tractor e cada módulo un apeiro: o profesor activa os que
// usa e o resto nin aparece na barra. O estado real vive no backend (ver
// internal/modulos); aquí só se pinta e se piden cambios.

// aplicarModulos amosa ou oculta os grupos da barra marcados con data-modulo, e
// reconstrúe os botóns dos módulos externos (que non están no marcado fixo).
function aplicarModulos(lista) {
    const activos = {};
    lista.forEach((m) => { activos[m.id] = m.activo; });
    document.querySelectorAll('[data-modulo]').forEach((el) => {
        // Un módulo descoñecido para o backend ocúltase: máis vale non amosar
        // un botón que non vai facer nada.
        el.hidden = !activos[el.dataset.modulo];
    });
    pintarBotonsExternos(lista.filter((m) => !m.interno && m.activo));
    // Sen ningún módulo-aplicación activo, a píldora de Tao/Xesta/externos
    // quedaría na barra baleira (ten fondo, bordo e padding propios) e cun
    // separador diante. Ocúltase enteira, igual ca o grupo de Ficheiros —
    // que xa o fai só, co seu data-modulo no <span> envolvente.
    const grupoApps = document.getElementById('grupo-apps');
    grupoApps.hidden = document.getElementById('btn-tao').hidden
        && document.getElementById('btn-xesta').hidden
        && document.getElementById('modulos-externos').children.length === 0;
    cargarAccionsModulos();
}

// cargarAccionsModulos pinta un botón por cada acción que traen os módulos
// activos. O backend só devolve as que o motor actual pode executar, así que
// cambiar de motor pode facer aparecer ou desaparecer botóns: é o correcto,
// unha acción que só trae .sls non se pode executar co motor SSH.
function cargarAccionsModulos() {
    AccionsModulos().then(pintarAccionsModulos).catch((err) => log('[MÓDULOS] ' + err));
}

function pintarAccionsModulos(accions) {
    const cont = document.getElementById('accions-modulos');
    cont.innerHTML = '';
    (accions || []).forEach((ac) => {
        const sep = document.createElement('span');
        sep.className = 'sep';
        cont.appendChild(sep);

        const btn = document.createElement('button');
        btn.className = 'btn-main';
        btn.textContent = (ac.icona ? ac.icona + ' ' : '') + ac.etiqueta;
        btn.title = ac.etiqueta + ' (módulo ' + ac.modulo + ')';
        btn.addEventListener('click', () => {
            log('[MÓDULOS] ' + t('log.modulos.executando', 'executando') + ' ' + ac.id + ' (' + ac.modulo + ')');
            enviar(ac.id, 'aula');
        });
        cont.appendChild(btn);
    });
}

// pintarBotonsExternos crea un botón por cada módulo externo activo. Sen isto,
// un módulo do cartafol aparecería na lista de configuración pero non habería
// forma de lanzalo.
function pintarBotonsExternos(externos) {
    const cont = document.getElementById('modulos-externos');
    cont.innerHTML = '';
    externos.forEach((m) => {
        // Xesta e Tao teñen modulo.json (para o panel de axustes/⚙ Aula → Módulos
        // e para que AbrirModulo os atope), pero NON deben aparecer aquí coma
        // botón xenérico: xa teñen o seu propio botón fixo (#btn-xesta,
        // #btn-tao). Seguen a aparecer, iso si, en ⚙ Aula → Módulos
        // (pintarListaModulos) para activalos/configuralos.
        if (m.id === 'xesta' || m.id === 'tao') return;
        // Sen separador: Pancho/Yang van no mesmo bloque visual ca Tao/Xesta
        // (ver marcado base — #modulos-externos vive dentro do mesmo
        // .toolbar-group), non coma píldoras soltas.
        const btn = document.createElement('button');
        btn.className = 'btn-main';
        // Icona propia do módulo (modulo.json: "iconaImaxe") se a declara;
        // senón o emoji de sempre. Mesmo criterio que pintarListaModulos.
        if (m.iconaImaxeDataURI) {
            const img = document.createElement('img');
            img.src = m.iconaImaxeDataURI;
            img.alt = '';
            img.className = 'modulo-icona-img';
            btn.appendChild(img);
            btn.appendChild(document.createTextNode(' ' + m.nome));
        } else {
            btn.textContent = (m.icona ? m.icona + ' ' : '') + m.nome;
        }
        btn.title = m.nome;
        // Mesmo comportamento que xa ten #btn-tao (liña ~1736): un módulo
        // externo de pantalla completa substitúe a Piztu por defecto — Ctrl
        // clic mantén as dúas apps abertas. Xenérico para calquera módulo
        // externo que pase por aquí (hoxe, Yang). Un módulo pode declarar
        // "mantenPiztuAberto" no seu modulo.json (ex. Inventario) para que
        // isto sexa así SEMPRE, sen precisar Ctrl.
        btn.addEventListener('click', (ev) => {
            const manterAberto = ev.ctrlKey || m.mantenPiztuAberto;
            AbrirModulo(m.id)
                .then(() => {
                    log('[MÓDULOS] ' + t('log.modulos.aberto', 'aberto') + ' ' + m.nome);
                    if (!manterAberto) Application.Quit();
                })
                .catch((err) => log('[MÓDULOS] ' + err));
        });
        cont.appendChild(btn);
    });
}

// Instalar Tao nos equipos cliente: dispárase desde o botón ⚙ da lista de
// módulos (⚙ Aula) e tamén desde o premido longo (3s) de #btn-tao na barra.
function instalarTaoClientes() {
    if (!confirm(t('web.tao.instalarclientes.confirmar',
        'Instalar Tao no escritorio de TODOS os equipos da aula?\n\n' +
        'Cópiase o aplicativo a cada equipo cliente e créase un acceso ' +
        'directo no menú e no escritorio do alumnado.'
    ))) return;
    const btnGear = document.getElementById('btn-tao-instalar-clientes');
    if (btnGear) btnGear.disabled = true;
    log('[TAO] ' + t('log.tao.instalandoclientes', 'instalando nos equipos cliente…'));
    InstalarTaoClientes();
}

function pintarListaModulos(lista) {
    const cont = document.getElementById('lista-modulos');
    cont.innerHTML = '';
    lista.forEach((m) => {
        const fila = document.createElement('label');
        fila.className = 'modulo-fila';

        const chk = document.createElement('input');
        chk.type = 'checkbox';
        chk.checked = m.activo;
        if (!m.instalado) {
            // Sen instalar non hai nada que activar: a caselas actívase soa
            // no repintado (evento modulos_cambiado) en canto remate a descarga.
            chk.disabled = true;
            chk.title = t('web.modulos.descargaantes', 'Descarga o módulo primeiro');
        }
        chk.addEventListener('change', () => {
            chk.disabled = true;
            SetModulo(m.id, chk.checked)
                .then(() => log('[MÓDULOS] ' + m.nome + ' ' + (chk.checked ? t('log.modulos.activado', 'activado') : t('log.modulos.desactivado', 'desactivado'))))
                .catch((err) => { chk.checked = !chk.checked; log('[MÓDULOS] ' + err); })
                .finally(() => { chk.disabled = false; });
        });

        const texto = document.createElement('span');
        if (m.iconaImaxeDataURI) {
            const img = document.createElement('img');
            img.src = m.iconaImaxeDataURI;
            img.alt = '';
            img.className = 'modulo-icona-img';
            texto.appendChild(img);
            texto.appendChild(document.createTextNode(' ' + t(m.clave, m.nome)));
        } else {
            texto.textContent = (m.icona ? m.icona + ' ' : '') + t(m.clave, m.nome);
        }

        fila.appendChild(chk);
        fila.appendChild(texto);
        if (!m.interno) {
            const etq = document.createElement('i');
            etq.className = 'modulo-etiqueta';
            etq.textContent = t('web.modulos.externo', 'externo');
            fila.appendChild(etq);
        }
        if (m.novo && !m.interno) {
            const etq = document.createElement('i');
            etq.className = 'modulo-novo';
            etq.textContent = t('web.modulos.novo', 'novo');
            fila.appendChild(etq);
        }
        if (m.ten_panel) {
            const cfg = document.createElement('button');
            cfg.className = 'modulo-cfg';
            cfg.textContent = '⚙';
            cfg.title = t('web.modulos.configurar', 'Configurar %s').replace('%s', m.nome);
            cfg.addEventListener('click', (ev) => {
                ev.preventDefault();
                abrirPanelModulo(m.id, m.nome);
            });
            fila.appendChild(cfg);
        }
        if (m.id === 'tao') {
            const instalar = document.createElement('button');
            instalar.id = 'btn-tao-instalar-clientes';
            instalar.className = 'modulo-cfg';
            instalar.textContent = '⚙';
            instalar.title = t('web.tao.instalarclientes.tip', 'Instalar Tao no escritorio dos equipos cliente');
            instalar.addEventListener('click', (ev) => {
                ev.preventDefault();
                instalarTaoClientes();
            });
            fila.appendChild(instalar);
        }
        if (!m.instalado && m.descargable) {
            const descargar = document.createElement('button');
            descargar.className = 'modulo-descargar';
            descargar.textContent = t('web.modulos.descargarbtn', '⬇ Descargar');
            descargar.title = t('web.modulos.descargar', 'Descargar %s').replace('%s', m.nome);
            descargar.addEventListener('click', (ev) => {
                ev.preventDefault();
                descargar.disabled = true;
                descargar.textContent = t('web.modulos.descargando', 'Descargando…');
                log('[MÓDULOS] ' + t('log.modulos.descargando', 'descargando') + ' ' + m.nome + '…');
                InstalarModulo(m.id)
                    .then(() => log('[MÓDULOS] ' + m.nome + ' ' + t('log.modulos.descargadoactiva', 'descargado — actívao na lista')))
                    .catch((err) => {
                        log('[MÓDULOS] ' + err);
                        descargar.disabled = false;
                        descargar.textContent = t('web.modulos.descargarbtn', '⬇ Descargar');
                    });
            });
            fila.appendChild(descargar);
        } else if (!m.instalado && m.motivo) {
            const aviso = document.createElement('i');
            aviso.className = 'modulo-etiqueta';
            aviso.textContent = '— ' + m.motivo;
            fila.appendChild(aviso);
        } else if (m.instalado && m.actualizable) {
            const actualizar = document.createElement('button');
            actualizar.className = 'modulo-descargar modulo-actualizar';
            actualizar.textContent = t('sobre.actualizar', '⟳ Actualizar');
            actualizar.title = (m.version ? 'v' + m.version : t('web.modulos.versionsendeclarar', 'versión sen declarar'))
                + ' → v' + m.version_nova;
            actualizar.addEventListener('click', (ev) => {
                ev.preventDefault();
                actualizar.disabled = true;
                actualizar.textContent = t('sobre.actualizando', 'Actualizando...');
                log('[MÓDULOS] ' + t('log.modulos.actualizando', 'actualizando') + ' ' + m.nome + '…');
                ActualizarModulo(m.id)
                    .then(() => log('[MÓDULOS] ' + m.nome + ' ' + t('log.modulos.actualizadoa', 'actualizado a') + ' v' + m.version_nova))
                    .catch((err) => {
                        log('[MÓDULOS] ' + err);
                        actualizar.disabled = false;
                        actualizar.textContent = t('sobre.actualizar', '⟳ Actualizar');
                    });
            });
            fila.appendChild(actualizar);
        }
        cont.appendChild(fila);

        // Descrición curta que o propio módulo declara no seu modulo.json
        // (opcional): unha liña baixo o nome que di que fai.
        if (m.descricion) {
            const descricion = document.createElement('div');
            descricion.className = 'modulo-descricion';
            descricion.textContent = m.descricion;
            cont.appendChild(descricion);
        }

        // Onde vive o módulo. É o que fai comprensible o cartafol: vese dun
        // golpe cales teñen ficheiros propios e cales van dentro de Piztu.
        const onde = document.createElement('div');
        onde.className = 'modulo-onde';
        onde.textContent = m.ruta ? m.ruta : t('web.modulos.dentrodepiztu', 'dentro de Piztu (sen ficheiros propios)');
        cont.appendChild(onde);
    });
}

// ultimaListaModulos evita repintar cando nada cambiou: o re-escaneo dispárase
// ao abrir ⚙ Aula e ao volver á xanela, e non ten sentido reconstruír a lista
// (nin roubar o foco dunha casela) se o cartafol segue igual.
let ultimaListaModulos = '';

function cargarModulos(forzar) {
    ModulosDispo().then((lista) => {
        const firma = JSON.stringify(lista);
        if (!forzar && firma === ultimaListaModulos) return;
        const habiaNovos = ultimaListaModulos !== '' &&
            lista.some((m) => m.novo && !ultimaListaModulos.includes('"id":"' + m.id + '"'));
        ultimaListaModulos = firma;
        aplicarModulos(lista);
        pintarListaModulos(lista);
        if (habiaNovos) {
            log('[MÓDULOS] ' + t('log.modulos.novodetectado', 'detectouse un módulo novo no cartafol — actívao en ⚙ Aula'));
        }
    }).catch((err) => log('[MÓDULOS] ' + err));
    RutaModulos().then((r) => {
        document.getElementById('ruta-modulos').textContent = r;
    }).catch(() => {});
}

document.getElementById('btn-abrir-modulos').addEventListener('click', () => {
    AbrirCartafolModulos().catch((err) => log('[MÓDULOS] ' + err));
});

// Volve consultar piztu.org e os github_repo do catálogo local sen reiniciar
// piztu. O repintado chega só, co evento modulos_cambiado que emite o backend.
document.getElementById('btn-comprobar-actualizacions').addEventListener('click', (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    const texto = btn.textContent;
    btn.textContent = t('web.modulos.comprobando', 'Comprobando…');
    log('[MÓDULOS] ' + t('log.modulos.comprobandoactualizacions', 'comprobando actualizacións…'));
    ComprobarActualizacionsModulos()
        .then(() => log('[MÓDULOS] ' + t('log.modulos.catalogoactualizado', 'catálogo actualizado')))
        .catch((err) => log('[MÓDULOS] ' + err))
        .finally(() => { btn.disabled = false; btn.textContent = texto; });
});

// O backend avisa cando remata de comprobar (ou volve comprobar) se hai unha
// versión nova de Piztu, así non hai que reiniciar piztu para velo.
Events.On('actualizacion_piztu_cambiada', (e) => aplicarActualizacionPiztu(e.data));

// O backend avisa tras cada cambio, así non hai que reiniciar piztu.
Events.On('modulos_cambiado', (e) => ((d) => {
    if (!d) return;
    if (d.modulos) {
        ultimaListaModulos = JSON.stringify(d.modulos);
        aplicarModulos(d.modulos);
        pintarListaModulos(d.modulos);
    }
    if (d.accions) pintarAccionsModulos(d.accions);
})(e.data));

// Actualización automática dun módulo (ver vixiarActualizacionsModulos): faise
// soa e sen preguntar, pero deixa a súa liña no rexistro — un módulo que muda
// de aspecto sen explicación parece unha avaría.
Events.On('modulo_actualizado', (e) => ((d) => {
    if (!d) return;
    log('[MÓDULOS] ' + d.nome + ' ' + t('log.modulos.actualizadoa', 'actualizado a')
        + ' v' + d.nova + (d.anterior ? ' (antes v' + d.anterior + ')' : ''));
})(e.data));

// ── Paneis declarativos dos módulos ─────────────────────────────────────────
// O módulo describe os campos nun YAML e piztu debúxaos co seu propio estilo.
// Non se executa código do módulo aquí: os tipos son pechados e todo o que non
// se recoñeza xa quedou fóra ao ler o panel (ver internal/modulos/panel.go).

function abrirPanelModulo(id, nome) {
    PanelModulo(id).then((res) => {
        if (!res || !res.panel) {
            log('[MÓDULOS] ' + nome + ' ' + t('log.modulos.senpanel', 'non ten panel'));
            return;
        }
        document.getElementById('panel-modulo-titulo').textContent = res.panel.titulo || nome;
        pintarPanelModulo(id, res);
        abrirModal('panel-modulo-modal');
    }).catch((err) => log('[MÓDULOS] ' + err));
}

function pintarPanelModulo(id, res) {
    const corpo = document.getElementById('panel-modulo-corpo');
    corpo.innerHTML = '';
    const valores = res.valores || {};

    (res.panel.campos || []).forEach((c) => {
        if (c.tipo === 'info') {
            const p = document.createElement('p');
            p.className = 'modal-hint';
            p.textContent = c.etiqueta;
            corpo.appendChild(p);
            return;
        }
        if (c.tipo === 'boton') {
            const b = document.createElement('button');
            b.className = 'btn-main';
            b.style.cssText = 'width:100%; padding:10px; margin-bottom:8px;';
            b.textContent = c.etiqueta;
            b.addEventListener('click', () => {
                log('[MÓDULOS] ' + t('log.modulos.executando', 'executando') + ' ' + c.accion);
                enviar(c.accion, 'aula');
            });
            corpo.appendChild(b);
            return;
        }

        const lbl = document.createElement('label');
        lbl.className = 'lbl-destino';
        lbl.textContent = c.etiqueta;
        corpo.appendChild(lbl);

        let control;
        if (c.tipo === 'lista') {
            control = document.createElement('select');
            const opcions = c.fonte === 'equipos' ? (res.equipos || []) : (c.opcions || []);
            opcions.forEach((o) => {
                const op = document.createElement('option');
                op.value = o; op.textContent = o;
                control.appendChild(op);
            });
            control.value = valores[c.id] || c.defecto || '';
        } else if (c.tipo === 'interruptor') {
            control = document.createElement('input');
            control.type = 'checkbox';
            control.checked = (valores[c.id] || c.defecto) === '1';
        } else {
            control = document.createElement('input');
            control.type = c.tipo === 'numero' ? 'number' : 'text';
            control.value = valores[c.id] || c.defecto || '';
        }
        if (control.tagName !== 'INPUT' || control.type !== 'checkbox') {
            control.style.cssText = 'width:100%; margin-bottom:10px;';
        }

        const gardar = () => {
            const v = control.type === 'checkbox' ? (control.checked ? '1' : '0') : control.value;
            GardarCampoModulo(id, c.id, String(v)).catch((err) => log('[MÓDULOS] ' + err));
        };
        control.addEventListener('change', gardar);
        corpo.appendChild(control);

        if (c.axuda) {
            const a = document.createElement('p');
            a.className = 'modal-hint';
            a.textContent = c.axuda;
            corpo.appendChild(a);
        }
    });
}

// ── Ruído (limiar + vúmetro en tempo real) ──────────────────────────────────
let currentUmbral = 30;
GetUmbral().then((v) => {
    currentUmbral = v;
    document.getElementById('limiar').value = v;
    document.getElementById('valorLimiar').textContent = v + '%';
});
document.getElementById('limiar').addEventListener('input', (e) => {
    currentUmbral = parseInt(e.target.value);
    document.getElementById('valorLimiar').textContent = currentUmbral + '%';
    GardarUmbral(currentUmbral);
});

// Interruptor manual do monitor de ruído (← chk_ruido/toggle_monitor de main.py).
// IMPORTANTE: o monitor NON arrinca só ao abrir a app — pode bloquear todos os
// equipos automaticamente se o ruído persiste, así que ten que ser sempre unha
// decisión explícita do profesor. Por iso actívase/desactívase mantendo premida
// a icona do micrófono 3s (mesmo xesto e animación ca #btn-motor), non cun
// simple clic. Empeza desactivado.
let monitorRuidoActivo = false;
let temporizadorMic = null;
const btnMic = document.getElementById('btn-mic');

function iniciarPresionMic() {
    btnMic.classList.add('pressing');
    temporizadorMic = setTimeout(() => {
        temporizadorMic = null;
        btnMic.classList.remove('pressing');
        monitorRuidoActivo = !monitorRuidoActivo;
        btnMic.classList.toggle('activo', monitorRuidoActivo);
        if (monitorRuidoActivo) {
            IniciarRuido();
            log('[RUÍDO] ' + t('log.ruido.activado', '✅ Monitor activado'));
        } else {
            DetenerRuido();
            log('[RUÍDO] ' + t('log.ruido.desactivado', '⏹ Monitor desactivado'));
        }
    }, PRESION_MOTOR_MS);
}
function cancelarPresionMic() {
    if (temporizadorMic) {
        clearTimeout(temporizadorMic);
        temporizadorMic = null;
        btnMic.classList.remove('pressing');
    }
}
btnMic.addEventListener('mousedown', iniciarPresionMic);
btnMic.addEventListener('touchstart', iniciarPresionMic, {passive: true});
btnMic.addEventListener('mouseup', cancelarPresionMic);
btnMic.addEventListener('touchend', cancelarPresionMic);
btnMic.addEventListener('mouseleave', cancelarPresionMic);
btnMic.addEventListener('touchcancel', cancelarPresionMic);

// Niveis do monitor de ruído (integrado en Go, ← internal/ruido).
Events.On('ruido_actual', (e) => ((d) => {
    const v = document.getElementById('vumetro');
    v.style.width = d.nivel + '%';
    v.style.background = d.nivel > currentUmbral ? 'var(--off)' : 'var(--on)';
})(e.data));
Events.On('ruido_notificacion', (e) => log('[RUÍDO] ' + e.data.msg));

// Estado online/offline por equipo (integrado en Go, ← internal/ping,
// porte de main.py:_iniciar_ping_loop).
Events.On('ping_estado', (e) => ((d) => {
    const pc = document.getElementById('pc-' + d.host);
    if (!pc) return;
    pc.classList.toggle('online', d.online);
    pc.classList.toggle('offline', !d.online);
})(e.data));

// Alumnado conectado con Tao por equipo (integrado en Go, ← internal/tao,
// porte de core/tao.py + blueprints/tao.py).
function pintarTao(estado) {
    const mapa = estado || {};
    document.querySelectorAll('.pc-user').forEach((el) => {
        const nome = el.dataset.host || '';
        const info = mapa[nome] || mapa[nome.split('.')[0]];
        if (info && info.vivo) {
            el.textContent = info.nome || '';
            el.classList.add('activo');
        } else {
            el.textContent = '';
            el.classList.remove('activo');
        }
    });
}
Events.On('estado_tao', (e) => pintarTao(e.data));

// ── Publicacións dos módulos ────────────────────────────────────────────────
// Un módulo pode declarar un recolector: unha orde que piztu executa en cada
// equipo, e cuxo resultado se pinta aquí, canda o nome da máquina. É o mesmo
// oco que enche Tao con .pc-user, pero aberto a calquera módulo.
function pintarPublicacions(pub) {
    const datos = pub || {};
    document.querySelectorAll('.pc-publicacions').forEach((el) => {
        const host = el.dataset.host || '';
        const liñas = [];
        Object.keys(datos).sort().forEach((modulo) => {
            const porEquipo = datos[modulo] || {};
            const valor = porEquipo[host] || porEquipo[host.split('.')[0]];
            if (valor) liñas.push(valor);
        });
        el.textContent = liñas.join(' · ');
        el.classList.toggle('activo', liñas.length > 0);
    });
}
Events.On('publicacions_modulo', (e) => pintarPublicacions(e.data));

// ── Comandos Ansible/Salt ────────────────────────────────────────────────────
function enviar(accion, target) {
    ExecutarComando(accion, target);
}
document.getElementById('btn-acender').addEventListener('click', () => enviar('acenderAula', 'aula'));
document.getElementById('btn-durmir').addEventListener('click', () => enviar('durmirAula', 'aula'));
// Bloquear: clic simple bloquea a aula coas opcións xa gardadas; premido 3s
// abre a pantalla para escoller que se bloquea por defecto (mesmo xesto e
// animación ca #btn-motor/#btn-mic). Lémbrase entre sesións (ver internal/db).
let temporizadorBloqueo = null;
const btnBloqueo = document.getElementById('btn-bloqueo');

const CHKS_BLOQUEO = {
    internet: document.getElementById('chk-bloq-internet'),
    ssh: document.getElementById('chk-bloq-ssh'),
    son: document.getElementById('chk-bloq-son'),
    rato: document.getElementById('chk-bloq-rato'),
    teclado: document.getElementById('chk-bloq-teclado'),
};

function abrirModalConfigBloqueo() {
    GetOpcionsBloqueo().then((opcions) => {
        for (const chave in CHKS_BLOQUEO) {
            CHKS_BLOQUEO[chave].checked = !!(opcions && opcions[chave]);
        }
        abrirModal('bloqueo-config-modal');
    });
}
document.getElementById('btn-bloqueo-config-gardar').addEventListener('click', () => {
    const opcions = {};
    for (const chave in CHKS_BLOQUEO) {
        opcions[chave] = CHKS_BLOQUEO[chave].checked;
    }
    GardarOpcionsBloqueo(opcions).then(() => cerrarModal('bloqueo-config-modal'));
});

function iniciarPresionBloqueo() {
    btnBloqueo.classList.add('pressing');
    temporizadorBloqueo = setTimeout(() => {
        temporizadorBloqueo = null;
        btnBloqueo.classList.remove('pressing');
        abrirModalConfigBloqueo();
    }, PRESION_MOTOR_MS);
}
function soltarPresionBloqueo() {
    if (temporizadorBloqueo) {
        // Soltouse antes do limiar: clic simple -> bloquear coa config gardada.
        clearTimeout(temporizadorBloqueo);
        temporizadorBloqueo = null;
        btnBloqueo.classList.remove('pressing');
        ExecutarBloqueo('aula');
    }
    // Se xa non hai temporizador, o premido longo xa abriu a configuración.
}
function cancelarPresionBloqueo() {
    if (temporizadorBloqueo) {
        clearTimeout(temporizadorBloqueo);
        temporizadorBloqueo = null;
        btnBloqueo.classList.remove('pressing');
    }
}
btnBloqueo.addEventListener('mousedown', iniciarPresionBloqueo);
btnBloqueo.addEventListener('touchstart', iniciarPresionBloqueo, {passive: true});
btnBloqueo.addEventListener('mouseup', soltarPresionBloqueo);
btnBloqueo.addEventListener('touchend', soltarPresionBloqueo);
btnBloqueo.addEventListener('mouseleave', cancelarPresionBloqueo);
btnBloqueo.addEventListener('touchcancel', cancelarPresionBloqueo);

document.getElementById('btn-liberar').addEventListener('click', () => enviar('desbloquearAula', 'aula'));
document.getElementById('btn-apagar').addEventListener('click', () => {
    if (confirm(t('web.js.apagartotal', '⚠️ Vas apagar TODOS os equipos da aula.'))) {
        enviar('apagar_aula', 'aula');
    }
});
document.getElementById('btn-reiniciar').addEventListener('click', () => {
    if (confirm(t('web.js.reiniciartotal', '⚠️ Vas REINICIAR TODOS os equipos da aula.'))) {
        enviar('reiniciarAula', 'aula');
    }
});

Events.On('comando_resultado', (e) => ((d) => {
    if (d.estado === 'erro') {
        setResumen('❌ ' + d.host + ': ' + (d.msg || 'erro'));
        log('[ERRO] ' + (d.accion || d.target || '') + ' @ ' + (d.host || d.target) + ': ' + (d.msg || t('log.erro.generico', 'erro')));
    } else {
        log('[OK] ' + d.accion + ' @ ' + d.host);
    }
})(e.data));
Events.On('comando_completo', (e) => ((d) => {
    setResumen('✅ ' + d.accion + ' — ' + d.total + ' equipo(s)');
    log('[SISTEMA] ' + d.accion + ' ' + t('log.sistema.completado', 'completado en %s equipo(s)').replace('%s', d.total));
})(e.data));

// ── Escenario / tarxetas de equipo ──────────────────────────────────────────
function renderEquipos(equipos) {
    equiposCache = equipos;
    escenario.innerHTML = '';
    equipos.forEach((e) => {
        const pc = document.createElement('div');
        pc.className = 'pc';
        pc.id = 'pc-' + e.nome;
        pc.style.left = e.x + 'px';
        pc.style.top = e.y + 'px';
        pc.innerHTML = `
            <span class="pc-header" data-host="${e.nome}" title="${t('equipo.tip.abrirmover', 'Premer para abrir; arrastrar para mover')}">${e.nome}</span>
            <span class="pc-user" data-host="${e.nome}"></span>
            <span class="pc-publicacions" data-host="${e.nome}"></span>
            <div class="controls">
                <button class="btn on" data-acc="acenderAula" title="${t('web.acender', 'Acender')}"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4v5"/><path d="M6.2 6A5.5 5.5 0 1 0 13.8 6"/></svg></button>
                <button class="btn sleep" data-acc="durmirAula" title="${t('web.durmir', 'Durmir')}"><svg viewBox="0 0 20 20" fill="currentColor"><path d="M14.5 12.4A6 6 0 0 1 7.6 5.5a6 6 0 1 0 6.9 6.9z"/></svg></button>
                <button class="btn lock" data-lock="1" title="${t('web.bloqueo', 'Bloquear')}"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="9" width="10" height="7" rx="1.4"/><path d="M7 9V6.5a3 3 0 0 1 6 0V9"/></svg></button>
                <button class="btn unlock" data-acc="desbloquearAula" title="${t('web.liberar', 'Liberar')}"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="9" width="10" height="7" rx="1.4"/><path d="M7 9V6.5a3 3 0 0 1 5.7-1.3"/></svg></button>
                <button class="btn off" data-off="1" title="${t('web.apagar', 'Apagar')}"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4v5"/><path d="M6.2 6A5.5 5.5 0 1 0 13.8 6"/></svg></button>
                <button class="btn ssh" data-ssh="1" title="SSH"><svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="14" height="12" rx="1.4"/><path d="M6.5 8l2.5 2-2.5 2"/><path d="M10.5 12.5h3"/></svg></button>
                <button class="btn btn-individual-oculto" data-enviar="1" title="${t('equipo.tip.enviar', 'Enviar')}">↑</button>
                <button class="btn btn-individual-oculto" data-recoller="1" title="${t('equipo.tip.recoller', 'Recoller')}">↓</button>
            </div>
        `;
        pc.querySelector('.pc-header').addEventListener('mousedown', (ev) => {
            ev.stopPropagation();
            arrastrarOuAbrir(ev, pc, e.nome);
        });
        pc.querySelectorAll('.btn[data-acc]').forEach((b) => {
            b.addEventListener('click', (ev) => {
                ev.stopPropagation();
                enviar(b.dataset.acc, e.nome);
            });
        });
        pc.querySelector('.btn[data-lock]').addEventListener('click', (ev) => {
            ev.stopPropagation();
            ExecutarBloqueo(e.nome);
        });
        pc.querySelector('.btn[data-off]').addEventListener('click', (ev) => {
            ev.stopPropagation();
            if (confirm(t('web.js.apagarequipo', 'Apagar o equipo %s?').replace('%s', e.nome))) {
                enviar('apagar_aula', e.nome);
            }
        });
        pc.querySelector('.btn[data-ssh]').addEventListener('click', (ev) => {
            ev.stopPropagation();
            abrirSSH(e.nome);
        });
        pc.querySelector('.btn[data-enviar]').addEventListener('click', (ev) => {
            ev.stopPropagation();
            abrirModalEnvioPara(e.nome);
        });
        pc.querySelector('.btn[data-recoller]').addEventListener('click', (ev) => {
            ev.stopPropagation();
            recollerEquipo(e.nome);
        });
        escenario.appendChild(pc);
    });
    renderSeleccionManual(equipos);
}

// Arrastre e clic comparten a cabeceira: por baixo do limiar de movemento
// cóntase coma clic (abrir explorador); por riba, coma arrastre (mover tarxeta).
const LIMIAR_ARRASTRE_PX = 4;

function arrastrarOuAbrir(ev, pc, nome) {
    const startClientX = ev.clientX;
    const startClientY = ev.clientY;
    const startLeft = pc.offsetLeft;
    const startTop = pc.offsetTop;
    let arrastrando = false;

    function mover(e) {
        // O escenario pode estar ampliado/reducido (ver zoom máis abaixo): o
        // desprazamento do rato en píxeles de pantalla hai que dividilo polo
        // zoom para obter o desprazamento real na coordenada local do escenario.
        const dx = e.clientX - startClientX;
        const dy = e.clientY - startClientY;
        if (!arrastrando && Math.hypot(dx, dy) > LIMIAR_ARRASTRE_PX) {
            arrastrando = true;
            pc.querySelector('.pc-header').classList.add('arrastrando');
        }
        if (arrastrando) {
            pc.style.left = (startLeft + dx / escenarioZoom) + 'px';
            pc.style.top = (startTop + dy / escenarioZoom) + 'px';
        }
    }
    function soltar() {
        document.removeEventListener('mousemove', mover);
        document.removeEventListener('mouseup', soltar);
        pc.querySelector('.pc-header').classList.remove('arrastrando');
        if (arrastrando) {
            GardarLayout(nome, parseInt(pc.style.left), parseInt(pc.style.top));
        } else {
            abrirExplorador(nome);
        }
    }
    document.addEventListener('mousemove', mover);
    document.addEventListener('mouseup', soltar);
}

// ── Zoom do escenario (Ctrl + / Ctrl -) ──────────────────────────────────────
const ZOOM_MIN = 0.4;
const ZOOM_MAX = 2;
const ZOOM_PASO = 0.1;
let escenarioZoom = 1;

function aplicarZoom() {
    escenario.style.transform = `scale(${escenarioZoom})`;
    escenario.style.transformOrigin = 'top left';
}

function cambiarZoom(delta) {
    const novo = Math.round((escenarioZoom + delta) * 100) / 100;
    escenarioZoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, novo));
    aplicarZoom();
    log(t('web.js.zoom', 'Ampliación: %s%').replace('%s', Math.round(escenarioZoom * 100)));
}

document.addEventListener('keydown', (e) => {
    if (!e.ctrlKey) return;
    if (e.key === '+' || e.key === '=') {
        e.preventDefault();
        cambiarZoom(ZOOM_PASO);
    } else if (e.key === '-' || e.key === '_') {
        e.preventDefault();
        cambiarZoom(-ZOOM_PASO);
    } else if (e.key === '0') {
        e.preventDefault();
        escenarioZoom = 1;
        aplicarZoom();
        log(t('web.js.zoom', 'Ampliación: %s%').replace('%s', 100));
    }
});

function cargarEquipos() {
    GetEquipos().then((equipos) => {
        renderEquipos(equipos);
        log('[SISTEMA] ' + t('log.sistema.cargados', '%s equipos cargados.').replace('%s', equipos.length));
        GetEstadoTao().then(pintarTao).catch(() => {});
    }).catch((err) => {
        console.error(err);
        log('[ERRO] ' + t('log.erro.cargandoequipos', 'non se puideron cargar os equipos:') + ' ' + err);
    });
}

// ── Terminal SSH ─────────────────────────────────────────────────────────────
let sshTerm = null, sshFitAddon = null;

function abrirSSH(host) {
    document.getElementById('ssh-host-label').textContent = host;
    abrirModal('ssh-modal');
    if (!sshTerm) {
        sshTerm = new Terminal({
            cursorBlink: true, fontSize: 13,
            fontFamily: '"JetBrains Mono","Cascadia Code","Fira Mono",monospace',
            // #5b62e1 é --primary (oklch(56% 0.19 276)) xa convertido a sRGB: o
            // canvas de xterm.js non resolve var() (non ten acceso á cascada
            // CSS), así que non abonda con pasarlle a variable directamente.
            theme: {background: '#090d16', foreground: '#e2e8f0', cursor: '#5b62e1'},
        });
        sshFitAddon = new FitAddon();
        sshTerm.loadAddon(sshFitAddon);
        sshTerm.open(document.getElementById('ssh-terminal'));
        sshFitAddon.fit();
        sshTerm.onData((d) => SSHInput(d));
        sshTerm.onResize((sz) => SSHResize(sz.cols, sz.rows));
    } else {
        sshTerm.reset();
        sshFitAddon.fit();
    }
    sshTerm.writeln('\r\n\x1b[1;34m' + t('web.js.conectando', 'Conectando a %s...').replace('%s', host) + '\x1b[0m');
    log('[SSH] ' + t('log.ssh.conectando', 'conectando a') + ' ' + host);
    SSHOpen(host);
}
function cerrarSSH() {
    SSHClose();
    cerrarModal('ssh-modal');
}
document.getElementById('ssh-close').addEventListener('click', cerrarSSH);
Events.On('ssh_data', (e) => { if (sshTerm) sshTerm.write(e.data.output); });
Events.On('ssh_closed', () => {
    if (sshTerm) sshTerm.writeln('\r\n\x1b[90m' + t('web.js.sesionpechada', '[sesión pechada]') + '\x1b[0m');
    log('[SSH] ' + t('log.ssh.pechada', 'sesión pechada'));
});

// ── Modais xenéricos ─────────────────────────────────────────────────────────
function abrirModal(id) { document.getElementById(id).classList.add('visible'); }
function cerrarModal(id) { document.getElementById(id).classList.remove('visible'); }
document.querySelectorAll('[data-close]').forEach((b) => {
    b.addEventListener('click', () => cerrarModal(b.dataset.close));
});
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        cerrarSSH();
        ['envio-modal', 'recollida-modal', 'explorer-modal', 'sobre-modal', 'bloqueo-config-modal', 'config-modal', 'scan-modal'].forEach(cerrarModal);
    }
});

// ── Modais arrastrables ──────────────────────────────────────────────────────
// A xanela de Configuración da aula ocupa case toda a pantalla e tapa o mapa:
// poder movela deixa consultar os equipos sen ter que pechala e reabrila.
// Móvese arrastrando a súa barra de título, coma unha ventá calquera.
//
// Impleméntase con transform e non con top/left: a caixa segue centrada polo
// flex do overlay, así que o arrastre é sempre un desprazamento relativo ao
// centro — non hai que calcular a posición inicial nin tocar o layout, e
// segue recentrándose soa se cambia o tamaño da ventá.
function facerArrastrable(caixa, asa) {
    let x = 0, y = 0;      // desprazamento actual respecto ao centro
    let px = 0, py = 0;    // punto onde se colleu, xa descontado o desprazamento
    let activo = false;

    // Mantén sempre a barra de título dentro da pantalla: sen isto pódese
    // soltar a xanela fóra e quedar sen nada por onde collela outra vez.
    const limitar = () => {
        const marxe = 48;
        const left0 = (window.innerWidth - caixa.offsetWidth) / 2;
        const top0 = (window.innerHeight - caixa.offsetHeight) / 2;
        x = Math.min(Math.max(x, marxe - caixa.offsetWidth - left0), window.innerWidth - marxe - left0);
        y = Math.min(Math.max(y, -top0), window.innerHeight - marxe - top0);
    };
    const aplicar = () => { caixa.style.transform = `translate(${x}px, ${y}px)`; };

    asa.classList.add('arrastrable');
    asa.addEventListener('pointerdown', (e) => {
        // Só o botón principal, e nunca desde un control da propia barra (o ✕):
        // arrastrar desde o botón de pechar impediría premelo.
        if (e.button !== 0 || e.target.closest('button')) return;
        activo = true;
        px = e.clientX - x;
        py = e.clientY - y;
        asa.setPointerCapture(e.pointerId);
        asa.classList.add('arrastrando');
        e.preventDefault();
    });
    asa.addEventListener('pointermove', (e) => {
        if (!activo) return;
        x = e.clientX - px;
        y = e.clientY - py;
        limitar();
        aplicar();
    });
    const rematar = (e) => {
        if (!activo) return;
        activo = false;
        try { asa.releasePointerCapture(e.pointerId); } catch (_) { }
        asa.classList.remove('arrastrando');
    };
    asa.addEventListener('pointerup', rematar);
    asa.addEventListener('pointercancel', rematar);

    // Ao redimensionar, a caixa recéntrase soa (flex) pero o desprazamento
    // gardado pode deixala fóra da pantalla: reaxústase.
    window.addEventListener('resize', () => {
        if (!x && !y) return;
        limitar();
        aplicar();
    });
}

{
    const caixaConfig = document.querySelector('#config-modal .config-box');
    facerArrastrable(caixaConfig, caixaConfig.querySelector('.modal-titlebar'));
}

// ── Configuración da aula (Paso 1: xerar inventario local) ────────────────────
// Infire prefixo/dominio/nº dos equipos actuais, para que o formulario apareza
// xa co que hai. Ex.: "tux01.local" → prefixo "tux", dominio ".local".
function inferirDatosAula() {
    const nomes = equiposCache.map((e) => e.nome);
    const datos = {prefixo: 'tux', dominio: '.local', n: nomes.length || 20};
    const m = /^([a-zA-Z]+)\d+(\..*)?$/.exec(nomes[0] || '');
    if (m) {
        datos.prefixo = m[1];
        datos.dominio = m[2] || '';
    }
    return datos;
}

function actualizarPreviaConfig() {
    const p = document.getElementById('cfg-prefixo').value.trim();
    const d = document.getElementById('cfg-dominio').value.trim();
    const n = parseInt(document.getElementById('cfg-nequipos').value, 10) || 0;
    const dous = (i) => String(i).padStart(2, '0');
    let previa = '';
    if (n >= 1) previa += `${p}${dous(1)}${d}`;
    if (n >= 2) previa += n > 2 ? `  …  ${p}${dous(n)}${d}` : `  ${p}${dous(2)}${d}`;
    document.getElementById('cfg-previa').textContent = previa;
}

document.getElementById('btn-config').addEventListener('click', () => {
    // Re-escanear o cartafol: se soltaches un módulo co programa aberto, aquí é
    // onde o vas buscar.
    cargarModulos();
    // Preferimos os datos persistidos; se aínda non se configurou nunca
    // (nequipos == 0), inferímolos dos equipos actuais do mapa.
    GetAulaConfig().then((cfg) => {
        const datos = cfg.nequipos > 0
            ? {prefixo: cfg.prefixo, dominio: cfg.dominio, n: cfg.nequipos}
            : inferirDatosAula();
        document.getElementById('cfg-prefixo').value = datos.prefixo;
        document.getElementById('cfg-dominio').value = datos.dominio;
        document.getElementById('cfg-nequipos').value = datos.n;
        document.getElementById('cfg-ssh-user').value = cfg.sshuser || 'usuario';
        document.getElementById('cfg-salt-master').value = cfg.saltmaster || '';
        document.getElementById('cfg-resultado').textContent = '';
        document.getElementById('cfg-ssh-resultado').textContent = '';
        document.getElementById('cfg-salt-resultado').textContent = '';
        actualizarPreviaConfig();
        pintarSelectorIdioma();
        abrirModal('config-modal');
    });
});

// O enderezo do salt-master resólvese en fondo (DNS con timeout, ver
// resolverSaltMaster en app.go) para non atrasar a apertura de ⚙ Aula. Se
// chega despois de abrir o modal, enche/corríxese o campo aquí — só se o
// profesor non o editou á man neste intre.
Events.On('aula_salt_master_resolto', (e) => {
    const el = document.getElementById('cfg-salt-master');
    const addr = (e.data && e.data.saltmaster) || '';
    if (el && addr && el !== document.activeElement) el.value = addr;
});

// Distribución da clave SSH (Paso 3): xerar no Mac + enviar aos equipos.
document.getElementById('btn-distribuir-clave').addEventListener('click', () => {
    const usuario = document.getElementById('cfg-ssh-user').value.trim();
    const pass = document.getElementById('cfg-ssh-pass').value;
    const res = document.getElementById('cfg-ssh-resultado');
    const btn = document.getElementById('btn-distribuir-clave');
    if (!usuario || !pass) {
        res.textContent = '⚠️ Indica usuario e contrasinal.';
        return;
    }
    btn.disabled = true;
    res.textContent = '⏳ Xerando e enviando clave…';
    log('[CLAVE] ' + t('log.clave.iniciando', 'iniciando distribución da clave SSH…'));
    DistribuirClaveSSH(usuario, pass);
});

Events.On('clave_log', (e) => ((d) => {
    const prefixo = d.host ? '[CLAVE ' + d.host + '] ' : '[CLAVE] ';
    log(prefixo + d.msg);
})(e.data));
Events.On('tao_clientes_log', (e) => ((d) => {
    const prefixo = d.host ? '[TAO ' + d.host + '] ' : '[TAO] ';
    log(prefixo + d.msg);
})(e.data));
Events.On('tao_clientes_completo', (e) => ((d) => {
    log(d.ok
        ? '[TAO] ✅ instalado en ' + d.total + ' equipo(s).'
        : '[TAO] ❌ non se completou en todos os equipos — revisa o rexistro.');
    const btn = document.getElementById('btn-tao-instalar-clientes');
    if (btn) btn.disabled = false;
})(e.data));

Events.On('clave_completo', (e) => ((d) => {
    const res = document.getElementById('cfg-ssh-resultado');
    res.textContent = d.ok
        ? '✅ Clave distribuída a ' + d.total + ' equipos. Revisa o rexistro para o detalle por equipo.'
        : '❌ Non se puido completar. Revisa o rexistro.';
    document.getElementById('btn-distribuir-clave').disabled = false;
})(e.data));

// Escaneo da rede (Paso 4): detectar equipos acendidos e gardar as súas MACs.
document.getElementById('btn-escanear-rede').addEventListener('click', () => {
    const res = document.getElementById('cfg-scan-resultado');
    const btn = document.getElementById('btn-escanear-rede');
    btn.disabled = true;
    res.textContent = '⏳ Escaneando a rede…';
    log('[SCAN] ' + t('log.scan.iniciando', 'iniciando escaneo da rede…'));
    EscanearRede();
});

Events.On('scan_log', (e) => ((d) => {
    const prefixo = d.host ? '[SCAN ' + d.host + '] ' : '[SCAN] ';
    log(prefixo + d.msg);
})(e.data));
Events.On('scan_completo', (e) => ((d) => {
    const res = document.getElementById('cfg-scan-resultado');
    res.textContent = d.ok
        ? '✅ ' + d.online + ' de ' + d.total + ' equipos acendidos; MACs gardadas no inventario.'
        : '❌ Non se puido escanear. Revisa o rexistro.';
    document.getElementById('btn-escanear-rede').disabled = false;
    if (d.ok) cargarEquipos(); // refresca o mapa co inventario actualizado
})(e.data));

// Escaneo avanzado de rede (alternativa ao esquema prefixo+número da tarxeta
// "1. Grupo"): atopa equipos aínda non inventariados na rede local e
// distribúe a clave SSH compartida cun contrasinal por equipo — nomes e
// contrasinais poden ser calquera, e o contrasinal só se usa unha vez, para
// esta distribución puntual.
let _equiposDescubertos = [];

document.getElementById('btn-escaneo-avanzado').addEventListener('click', () => {
    document.getElementById('lista-descubertos').innerHTML = '';
    document.getElementById('progreso-scan').innerHTML = '';
    document.getElementById('scan-resultado').textContent = '';
    document.getElementById('btn-scan-distribuir').disabled = true;
    _equiposDescubertos = [];
    abrirModal('scan-modal');
});

document.getElementById('btn-scan-iniciar').addEventListener('click', () => {
    document.getElementById('btn-scan-iniciar').disabled = true;
    document.getElementById('scan-resultado').textContent = '⏳ Escaneando a rede local…';
    document.getElementById('lista-descubertos').innerHTML = '';
    document.getElementById('btn-scan-distribuir').disabled = true;
    log('[DESCUBRIMENTO] ' + t('log.descubrimento.iniciando', 'iniciando escaneo de rede…'));
    DescubrirRede();
});

Events.On('descubrimento_log', (e) => ((d) => {
    const prefixo = d.host ? '[DESCUBRIMENTO ' + d.host + '] ' : '[DESCUBRIMENTO] ';
    log(prefixo + d.msg);
})(e.data));

Events.On('descubrimento_completo', (e) => ((d) => {
    document.getElementById('btn-scan-iniciar').disabled = false;
    _equiposDescubertos = d.equipos || [];
    document.getElementById('scan-resultado').textContent = _equiposDescubertos.length
        ? '✅ Atopados ' + _equiposDescubertos.length + ' equipo(s).'
        : 'Non se atopou ningún equipo.';
    pintarListaDescubertos(_equiposDescubertos);
    document.getElementById('btn-scan-distribuir').disabled = _equiposDescubertos.length === 0;
})(e.data));

function pintarListaDescubertos(equipos) {
    const cont = document.getElementById('lista-descubertos');
    cont.innerHTML = '';
    equipos.forEach((eq) => {
        const fila = document.createElement('div');
        fila.className = 'host-fila';
        fila.dataset.ip = eq.ip;

        const chk = document.createElement('input');
        chk.type = 'checkbox';
        chk.className = 'host-chk';

        const etiqueta = document.createElement('span');
        etiqueta.className = 'host-etiqueta';
        etiqueta.textContent = eq.hostname ? `${eq.hostname} (${eq.ip})` : eq.ip;

        const pass = document.createElement('input');
        pass.type = 'password';
        pass.className = 'host-pass';
        pass.placeholder = 'contrasinal';

        fila.appendChild(chk);
        fila.appendChild(etiqueta);
        fila.appendChild(pass);
        cont.appendChild(fila);
    });
}

document.getElementById('btn-scan-distribuir').addEventListener('click', () => {
    const usuario = document.getElementById('scan-usuario').value.trim() || 'usuario';
    const credenciais = Array.from(document.querySelectorAll('.host-fila'))
        .filter((fila) => fila.querySelector('.host-chk').checked && fila.querySelector('.host-pass').value)
        .map((fila) => ({
            ip: fila.dataset.ip,
            usuario,
            contrasinal: fila.querySelector('.host-pass').value,
        }));

    if (credenciais.length === 0) {
        document.getElementById('scan-resultado').textContent = '⚠️ Marca polo menos un equipo e escribe o seu contrasinal.';
        return;
    }

    document.getElementById('btn-scan-distribuir').disabled = true;
    document.getElementById('progreso-scan').innerHTML = '';
    log('[CLAVE] ' + t('log.clave.distribuindo', 'distribuíndo clave a %s equipo(s) descubertos…').replace('%s', credenciais.length));
    DistribuirClaveHosts(credenciais);
});

Events.On('clave_hosts_log', (e) => ((d) => {
    const prefixo = d.host ? '[CLAVE ' + d.host + '] ' : '[CLAVE] ';
    log(prefixo + d.msg);
    if (d.host) {
        const container = document.getElementById('progreso-scan');
        const rowId = progressRow(container, d.host, 'pclave');
        const fill = document.getElementById('fill-' + rowId);
        const icon = document.getElementById('icon-' + rowId);
        if (d.msg.startsWith('✅')) {
            fill.className = 'progress-bar-fill fill-ok'; fill.style.width = '100%'; icon.textContent = '✅';
        } else if (d.msg.startsWith('❌')) {
            fill.className = 'progress-bar-fill fill-erro'; fill.style.width = '100%'; icon.textContent = '❌';
            document.getElementById(rowId).title = d.msg;
        }
    }
})(e.data));

Events.On('clave_hosts_completo', (e) => ((d) => {
    document.getElementById('btn-scan-distribuir').disabled = false;
    document.getElementById('scan-resultado').textContent = d.ok
        ? '✅ Clave distribuída aos ' + d.total + ' equipo(s) seleccionados.'
        : '⚠️ ' + (d.exitosos ? d.exitosos.length : 0) + ' de ' + d.total + ' equipo(s) completados. Revisa o rexistro.';
    if (d.exitosos && d.exitosos.length) cargarEquipos(); // refresca o mapa cos novos equipos no inventario
})(e.data));

// Instalación de Salt (master + minions).
document.getElementById('btn-instalar-salt').addEventListener('click', () => {
    const master = document.getElementById('cfg-salt-master').value.trim();
    const usuario = document.getElementById('cfg-ssh-user').value.trim();
    const pass = document.getElementById('cfg-ssh-pass').value;
    const res = document.getElementById('cfg-salt-resultado');
    const btn = document.getElementById('btn-instalar-salt');
    if (!usuario || !pass) {
        res.textContent = t('web.config.p4.faltan', '⚠️ Indica usuario e contrasinal (os da tarxeta 2).');
        return;
    }
    btn.disabled = true;
    res.textContent = '⏳ Instalando Salt… (pode tardar; mira o rexistro)';
    log('[SALT] ' + t('log.salt.iniciando', 'iniciando instalación de Salt…'));
    InstalarSalt(master, usuario, pass);
});

// Diagnóstico de Salt: só le, non toca nada nin pide contrasinal.
document.getElementById('btn-comprobar-salt').addEventListener('click', async () => {
    const btn = document.getElementById('btn-comprobar-salt');
    const saida = document.getElementById('cfg-salt-diagnostico');
    btn.disabled = true;
    saida.textContent = t('web.config.p4.comprobando', '⏳ Comprobando…');
    try {
        const liñas = await ComprobarSalt();
        saida.textContent = liñas.join('\n');
        liñas.forEach((l) => log('[SALT] ' + l.replace(/\n\s+/g, ' ')));
    } catch (err) {
        saida.textContent = '⚠️ ' + err;
    } finally {
        btn.disabled = false;
    }
});

Events.On('salt_setup_log', (e) => ((d) => {
    const prefixo = d.host ? '[SALT ' + d.host + '] ' : '[SALT] ';
    log(prefixo + d.msg);
})(e.data));
Events.On('salt_setup_completo', (e) => ((d) => {
    const res = document.getElementById('cfg-salt-resultado');
    res.textContent = d.ok
        ? '✅ Proceso rematado. Revisa o rexistro para o detalle por equipo.'
        : '❌ Non se puido completar. Revisa o rexistro.';
    document.getElementById('btn-instalar-salt').disabled = false;
})(e.data));

// Instalador de Ansible (apt en Debian, Homebrew en macOS). Usa o mesmo
// contrasinal (sudo) da tarxeta 2 (Clave SSH).
document.getElementById('btn-instalar-ansible').addEventListener('click', () => {
    const pass = document.getElementById('cfg-ssh-pass').value;
    const btn = document.getElementById('btn-instalar-ansible');
    btn.disabled = true;
    document.getElementById('cfg-ansible-resultado').textContent = '⏳ Instalando Ansible… (mira o rexistro)';
    log('[ANSIBLE] ' + t('log.ansible.instalando', 'instalando…'));
    InstalarAnsible(pass);
});

Events.On('ansible_setup_log', (e) => log('[ANSIBLE] ' + e.data.msg));
Events.On('ansible_setup_completo', (e) => ((d) => {
    document.getElementById('cfg-ansible-resultado').textContent = d.ok
        ? '✅ Ansible instalado. Escolle o motor Ansible na barra (⚙, premido 3s).'
        : '⚠️ Non se completou. Revisa o rexistro.';
    document.getElementById('btn-instalar-ansible').disabled = false;
})(e.data));

['cfg-prefixo', 'cfg-dominio', 'cfg-nequipos'].forEach((id) => {
    document.getElementById(id).addEventListener('input', actualizarPreviaConfig);
});

document.getElementById('btn-xerar-inventario').addEventListener('click', () => {
    const prefixo = document.getElementById('cfg-prefixo').value.trim();
    const dominio = document.getElementById('cfg-dominio').value.trim();
    const n = parseInt(document.getElementById('cfg-nequipos').value, 10) || 0;
    const res = document.getElementById('cfg-resultado');
    if (!prefixo || n < 1) {
        res.textContent = '⚠️ Indica un prefixo e polo menos 1 equipo.';
        return;
    }
    XerarInventario(prefixo, dominio, n).then((ruta) => {
        res.textContent = '✅ Inventario escrito en ' + ruta;
        log('[CONFIG] ' + t('log.config.inventarioxerado', 'inventario xerado con %s equipos →').replace('%s', n) + ' ' + ruta);
        cargarEquipos();
    }).catch((err) => {
        res.textContent = '❌ ' + err;
        log('[ERRO] ' + t('log.erro.xerandoinventario', 'xerando inventario:') + ' ' + err);
    });
});

// ── Envío de prácticas ───────────────────────────────────────────────────────
let _rutasEnvio = [];
let _opIdEnvioActivo = null;

function renderSeleccionManual(equipos) {
    const cont = document.getElementById('seleccion-manual');
    cont.innerHTML = equipos.map((e) =>
        `<label><input type="checkbox" class="chk-equipo" value="${e.nome}"> ${e.nome}</label>`
    ).join('');
}

document.querySelectorAll('input[name="destino"]').forEach((r) => {
    r.addEventListener('change', function () {
        document.getElementById('seleccion-manual').style.display = this.value === 'seleccion' ? 'block' : 'none';
    });
});

function abrirModalEnvio() {
    _rutasEnvio = [];
    document.getElementById('lista-ficheiros-envio').innerHTML = '';
    document.getElementById('progreso-envio').innerHTML = '';
    document.getElementById('btn-enviar-now').disabled = false;
    document.querySelector('input[name="destino"][value="todos"]').checked = true;
    document.getElementById('seleccion-manual').style.display = 'none';
    abrirModal('envio-modal');
}
document.getElementById('btn-enviar').addEventListener('click', abrirModalEnvio);

// Igual ca abrirModalEnvio(), pero preseleccionando un só equipo coma destino
// (← enviar_practica_a de main.py).
function abrirModalEnvioPara(host) {
    abrirModalEnvio();
    document.querySelector('input[name="destino"][value="seleccion"]').checked = true;
    document.getElementById('seleccion-manual').style.display = 'block';
    document.querySelectorAll('.chk-equipo').forEach((c) => { c.checked = (c.value === host); });
}

document.getElementById('btn-engadir-ficheiros').addEventListener('click', () => {
    SelectFicheiros().then((rutas) => {
        (rutas || []).forEach((r) => {
            if (!_rutasEnvio.includes(r)) _rutasEnvio.push(r);
        });
        renderListaFicheiros();
    }).catch((err) => console.error(err));
});

function renderListaFicheiros() {
    const ul = document.getElementById('lista-ficheiros-envio');
    ul.innerHTML = '';
    _rutasEnvio.forEach((ruta, idx) => {
        const nome = ruta.split('/').pop();
        const li = document.createElement('li');
        li.innerHTML = `📄 <span class="fnome">${nome}</span><button data-idx="${idx}">✕</button>`;
        li.querySelector('button').addEventListener('click', () => {
            _rutasEnvio.splice(idx, 1);
            renderListaFicheiros();
        });
        ul.appendChild(li);
    });
}

document.getElementById('btn-enviar-now').addEventListener('click', () => {
    if (_rutasEnvio.length === 0) {
        alert(t('web.js.selficheiro', 'Selecciona polo menos un ficheiro.'));
        return;
    }
    const destinoTipo = document.querySelector('input[name="destino"]:checked').value;
    let destinos = [];
    if (destinoTipo === 'seleccion') {
        destinos = Array.from(document.querySelectorAll('.chk-equipo:checked')).map((c) => c.value);
        if (destinos.length === 0) {
            alert(t('web.js.selequipo', 'Selecciona polo menos un equipo.'));
            return;
        }
    }
    document.getElementById('btn-enviar-now').disabled = true;
    document.getElementById('progreso-envio').innerHTML = `<p class="modal-hint">${t('web.js.iniciandoenvio', 'Iniciando envío...')}</p>`;
    EnviarPracticas(_rutasEnvio, destinos).then((opId) => {
        _opIdEnvioActivo = opId;
        document.getElementById('progreso-envio').innerHTML = '';
        setResumen(t('web.js.enviandoa', 'Enviando a %s equipos...').replace('%s', destinoTipo === 'seleccion' ? destinos.length : equiposCache.length));
    });
});

function progressRow(container, host, prefix) {
    const rowId = prefix + '-' + host.replace(/\./g, '_');
    let row = document.getElementById(rowId);
    if (!row) {
        row = document.createElement('div');
        row.className = 'progress-row';
        row.id = rowId;
        row.innerHTML = `<span class="host-lbl">${host}</span>
            <div class="progress-bar-wrap"><div class="progress-bar-fill fill-sending" id="fill-${rowId}" style="width:50%"></div></div>
            <span class="status-icon" id="icon-${rowId}">⏳</span>`;
        container.appendChild(row);
    }
    return rowId;
}

Events.On('progreso_envio', (e) => ((d) => {
    if (_opIdEnvioActivo && d.op_id && d.op_id !== _opIdEnvioActivo) return;
    const container = document.getElementById('progreso-envio');
    const rowId = progressRow(container, d.host, 'penv');
    const fill = document.getElementById('fill-' + rowId);
    const icon = document.getElementById('icon-' + rowId);
    if (d.estado === 'ok') {
        fill.className = 'progress-bar-fill fill-ok'; fill.style.width = '100%'; icon.textContent = '✅';
    } else if (d.estado === 'erro') {
        fill.className = 'progress-bar-fill fill-erro'; fill.style.width = '100%'; icon.textContent = '❌';
        document.getElementById(rowId).title = d.msg || '';
    }
})(e.data));
Events.On('envio_completo', (e) => ((d) => {
    if (_opIdEnvioActivo && d.op_id && d.op_id !== _opIdEnvioActivo) return;
    const ok = d.ok !== undefined ? d.ok : 0;
    const err = d.erros !== undefined ? d.erros : (d.total - ok);
    setResumen(t('web.js.enviocompleto', 'Envío completo: ✅ %s correctos').replace('%s', ok) + (err > 0 ? `, ❌ ${err} ${t('web.js.conerros', 'con erros')}` : ''));
    document.getElementById('btn-enviar-now').disabled = false;
    log('[ENVÍO] ' + t('log.envio.completo', 'completo — ✅') + ' ' + ok + (err > 0 ? ', ❌ ' + err : ''));
})(e.data));

// ── Recollida de prácticas ───────────────────────────────────────────────────
let _opIdRecolActivo = null;

function recollerDestinos(destinos, numEquipos) {
    document.getElementById('progreso-recollida').innerHTML = `<p class="modal-hint">${t('web.js.iniciandorecollida', 'Iniciando recollida...')}</p>`;
    abrirModal('recollida-modal');
    RecollerPracticas(destinos).then((opId) => {
        _opIdRecolActivo = opId;
        document.getElementById('progreso-recollida').innerHTML = '';
        setResumen(t('web.js.recollendoa', 'Recollendo traballos de %s equipos...').replace('%s', numEquipos));
    });
}
document.getElementById('btn-recoller').addEventListener('click', () => {
    recollerDestinos([], equiposCache.length);
});

// Recollida dun só equipo (← recoller_equipo de main.py).
function recollerEquipo(host) { recollerDestinos([host], 1); }

// Abrir Tao neste mesmo servidor (← abrir_tao de main.py).
// Por defecto Tao substitúe a Piztu (péchase despois de abrilo); con
// Ctrl+clic mantéñense os dous aplicativos abertos á vez.
document.getElementById('btn-sobre').addEventListener('click', () => {
    abrirModal('sobre-modal');
});
document.getElementById('sobre-web').addEventListener('click', (ev) => {
    ev.preventDefault();
    Browser.OpenURL('https://piztu.org');
});

// Instala en quente a última versión de Piztu e reinicia (← bordo violeta no
// botón "i" cando GetActualizacionPiztu/evento actualizacion_piztu_cambiada
// din que hai unha nova). Piztu péchase só e reábrese xa actualizado — non
// hai que descargar nin descomprimir nada á man.
document.getElementById('btn-sobre-actualizar').addEventListener('click', () => {
    if (!confirm(t('sobre.actualizar.confirmar',
        'Isto vai reiniciar Piztu para aplicar a actualización.\n\nPecharanse as sesións SSH abertas e calquera envío ou recollida en curso. ¿Continuar?'))) {
        return;
    }
    const btn = document.getElementById('btn-sobre-actualizar');
    btn.disabled = true;
    btn.textContent = t('sobre.actualizando', 'Actualizando...');
    AplicarActualizacionPiztu()
        .then(() => {
            btn.textContent = t('sobre.actualizar.ok', '✔ Reiniciando…');
            log('[ACTUALIZACIÓN] ' + t('log.actualizacion.instalada', 'instalada — reiniciando Piztu…'));
            setTimeout(() => Application.Quit(), 400);
        })
        .catch((err) => {
            btn.disabled = false;
            btn.textContent = t('sobre.actualizar', '⟳ Actualizar');
            log('[ACTUALIZACIÓN] ' + err);
        });
});

// Clic simple abre Tao (Ctrl clic mantén Piztu aberto tamén); premido 3s
// (mesma animación de recheo có motor/bloqueo) ofrece instalalo nos clientes.
function abrirTao(manterAberto) {
    AbrirTao().then(() => {
        log('[TAO] ' + t('log.tao.aberto', 'Tao aberto.'));
        if (!manterAberto) Application.Quit();
    }).catch((err) => {
        alert(t('tao.erro.msg', 'Tao non está instalado neste servidor.'));
        log('[ERRO TAO] ' + err);
    });
}
const btnTao = document.getElementById('btn-tao');
let temporizadorTao = null;
function iniciarPresionTao() {
    btnTao.classList.add('pressing');
    temporizadorTao = setTimeout(() => {
        temporizadorTao = null;
        btnTao.classList.remove('pressing');
        instalarTaoClientes();
    }, PRESION_MOTOR_MS);
}
function soltarPresionTao(ev) {
    if (temporizadorTao) {
        // Soltouse antes do limiar: clic simple -> abrir Tao.
        clearTimeout(temporizadorTao);
        temporizadorTao = null;
        btnTao.classList.remove('pressing');
        abrirTao(!!(ev && ev.ctrlKey));
    }
    // Se xa non hai temporizador, o premido longo xa lanzou a instalación.
}
function cancelarPresionTao() {
    if (temporizadorTao) {
        clearTimeout(temporizadorTao);
        temporizadorTao = null;
        btnTao.classList.remove('pressing');
    }
}
btnTao.addEventListener('mousedown', iniciarPresionTao);
btnTao.addEventListener('touchstart', iniciarPresionTao, {passive: true});
btnTao.addEventListener('mouseup', soltarPresionTao);
btnTao.addEventListener('touchend', soltarPresionTao);
btnTao.addEventListener('mouseleave', cancelarPresionTao);
btnTao.addEventListener('touchcancel', cancelarPresionTao);

// ── Xesta (asistente de IA) ───────────────────────────────────────────────
// Xanela propia (mesmo patrón ca Tao/Pancho — ver docs/api-modulos.md e
// AbrirModulo en app.go), non un servidor HTTP incrustado nun iframe coma
// antes. "mantenPiztuAberto" en modulo.json fai que quede aberta a carón do
// mapa por defecto (sen precisar Ctrl+clic) — Xesta úsase mentres se mira o
// mapa, non en vez del.
document.getElementById('btn-xesta').addEventListener('click', () => {
    log('[XESTA] ' + t('log.xesta.arrincando', 'arrincando…'));
    AbrirModulo('xesta').then(() => {
        log('[XESTA] ' + t('log.xesta.aberto', 'Xesta aberto.'));
        // Non peche Piztu: modulo.json declara "mantenPiztuAberto" (ver
        // pintarBotonsExternos e o botón de Tao para o patrón contrario).
    }).catch((err) => {
        alert(t('xesta.erro.msg', 'Non se puido arrincar Xesta: ') + err);
        log('[ERRO XESTA] ' + err);
    });
});

Events.On('progreso_recollida', (e) => ((d) => {
    if (_opIdRecolActivo && d.op_id && d.op_id !== _opIdRecolActivo) return;
    const container = document.getElementById('progreso-recollida');
    const rowId = progressRow(container, d.host, 'prec');
    const fill = document.getElementById('fill-' + rowId);
    const icon = document.getElementById('icon-' + rowId);
    if (d.estado === 'ok') {
        fill.className = 'progress-bar-fill fill-ok'; fill.style.width = '100%'; icon.textContent = '✅';
        const n = (d.ficheiros || []).length;
        const hdr = document.querySelector(`.pc-header[data-host="${d.host}"]`);
        if (hdr && n > 0) hdr.classList.add('ten-ficheiros');
    } else if (d.estado === 'erro') {
        fill.className = 'progress-bar-fill fill-erro'; fill.style.width = '100%'; icon.textContent = '❌';
        document.getElementById(rowId).title = d.msg || '';
        log('[RECOLLIDA ' + d.host + '] ❌ ' + (d.msg || t('log.erro.generico', 'erro')));
    }
})(e.data));
Events.On('recollida_completa', (e) => ((d) => {
    if (_opIdRecolActivo && d.op_id && d.op_id !== _opIdRecolActivo) return;
    const resultados = d.resultados || {};
    const ok = Object.values(resultados).filter((v) => v.ok).length;
    const err = d.total - ok;
    setResumen(t('web.js.recollidacompleta', 'Recollida completa: ✅ %s equipos').replace('%s', ok) + (err > 0 ? `, ❌ ${err} ${t('web.js.conerros', 'con erros')}` : ''));
    log('[RECOLLIDA] ' + t('log.recollida.completa', 'completa — ✅') + ' ' + ok + (err > 0 ? ', ❌ ' + err : ''));
})(e.data));

// ── Explorador de ficheiros dun equipo ──────────────────────────────────────
const ICONS = {
    pdf: '📄', doc: '📝', docx: '📝', odt: '📝', txt: '📃',
    py: '🐍', js: '📜', html: '🌐', css: '🎨',
    png: '🖼', jpg: '🖼', jpeg: '🖼', gif: '🖼',
    zip: '🗜', tar: '🗜', gz: '🗜',
};

function formatBytes(b) {
    if (b < 1024) return b + ' B';
    if (b < 1048576) return (b / 1024).toFixed(1) + ' KB';
    return (b / 1048576).toFixed(1) + ' MB';
}

function abrirExplorador(host) {
    document.getElementById('explorer-host').textContent = host;
    document.getElementById('explorer-list').innerHTML = `<li><span class="modal-hint">${t('web.js.cargando', 'Cargando...')}</span></li>`;
    abrirModal('explorer-modal');
    ListarFicheiros(host).then(renderExplorer);
}

function renderExplorer(data) {
    const ul = document.getElementById('explorer-list');
    const ficheiros = data.ficheiros || [];
    if (ficheiros.length === 0) {
        ul.innerHTML = `<li><div class="explorer-empty"><div class="empty-icon">📭</div>
            <p>${t('web.js.senficheiros', 'Non hai ficheiros recollidos de')} <strong>${data.host}</strong>.</p>
            <p class="modal-hint">${t('web.js.pulsarrecoller', 'Pulsa "📥 Recoller traballos" para descargar os ficheiros do equipo.')}</p></div></li>`;
        return;
    }
    ul.innerHTML = '';
    ficheiros.forEach((f) => {
        const ext = f.nome.split('.').pop().toLowerCase();
        const icon = ICONS[ext] || '📎';
        const fecha = new Date(f.modificado * 1000).toLocaleString('gl-ES');
        const li = document.createElement('li');
        li.title = t('web.js.abrirficheiro', 'Dobre clic para abrir');
        li.innerHTML = `<span class="file-icon">${icon}</span>
            <span class="file-name" title="${fecha}">${f.nome}</span>
            <span class="file-size">${formatBytes(f['tamaño'])}</span>`;
        li.addEventListener('dblclick', () => {
            AbrirFicheiro(data.host, f.nome).catch((err) => alert(t('web.js.erroabrir', 'Non se puido abrir o ficheiro: ') + err));
        });
        ul.appendChild(li);
    });
}

// ── Arranque ─────────────────────────────────────────────────────────────────
Traducions().then((traducions) => {
    I18N = traducions || {};
    aplicarTraducions();
}).catch(() => {});
// Coñecer o motor base (ssh en macOS, salt en Linux) antes de pintar, para
// que o botón volva ao motor correcto ao desactivar Ansible.
MotorBase().then((b) => { motorBase = b; }).finally(cargarMotor);
cargarEquipos();
cargarModulos(true);
GetPublicacions().then(pintarPublicacions).catch(() => {});
GetActualizacionPiztu().then(aplicarActualizacionPiztu).catch(() => {});

// Volver á xanela tras soltar un módulo no cartafol tamén dispara o re-escaneo,
// para que o botón apareza sen ter que abrir a configuración nin reiniciar.
window.addEventListener('focus', () => cargarModulos());
