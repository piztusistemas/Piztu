import './app.css';
import piztuLogo from './assets/images/logo.png';

import {
    Idioma, SetIdioma, Idiomas, Traducions,
    EsRoot, PermisoDenegado, DestDir, InstalacionDesc, CompletadoDesc, Version,
    Instalar, InstalarLamp, InstalarFileBrowser, CrearAccesosDirectos,
    DesinstalarLamp, DesinstalarFileBrowser, DesinstalarPiztu,
} from '../wailsjs/go/main/App';
import {EventsOn, Quit} from '../wailsjs/runtime/runtime';

// ── Estado global ────────────────────────────────────────────────────────────
let I18N = {};
let IDIOMAS = [{code: 'gl', nome: 'Galego'}];
let idiomaActual = 'gl';
let esRootCache = true;
let destDirCache = '/opt/piztu';
let instDescCache = '';
let finDescCache = '';
let versionCache = '';
let current = 0;

const STEPS = [
    {id: 'benvida', key: 'paso.benvida'},
    {id: 'instalacion', key: 'paso.instalacion'},
    {id: 'lamp', key: 'paso.lamp'},
    {id: 'filebrowser', key: 'paso.filebrowser'},
    {id: 'completado', key: 'paso.completado'},
    {id: 'desinstalar', key: 'paso.desinstalar', danger: true},
];

function t(clave, fallback) {
    return I18N[clave] || fallback || clave;
}

// "Paso %d de %d", "Escribe %s para confirmar"… substitúe %d e %s pola orde
// en que aparecen (como fai fmt.Sprintf no lado Go).
function formatoNum(fmt, ...args) {
    let i = 0;
    return fmt.replace(/%[ds]/g, () => args[i++]);
}

const app = document.getElementById('app');

// ── Pantalla de permiso denegado (pkexec non dispoñible/cancelado) ─────────
function renderPermisoDenegado() {
    app.innerHTML = `
      <div class="permiso-screen">
        <div class="card">
          <h3>${t('permiso.title', 'Permiso necesario')}</h3>
          <p>${t('permiso.texto', '')}</p>
          <button class="btn-main primary" id="btn-permiso-pechar">${t('permiso.pechar', 'Pechar')}</button>
        </div>
      </div>
    `;
    document.getElementById('btn-permiso-pechar').addEventListener('click', () => Quit());
}

// ── Esqueleto principal (barra lateral + panel) ─────────────────────────────
function render() {
    app.innerHTML = `
      <div class="sidebar">
        <div class="sidebar-top">
          <img src="${piztuLogo}" class="sidebar-logo" alt="Piztu Sistemas">
          <div class="sidebar-lang">
            <span>${t('nav.idioma', 'Idioma')}</span>
            <select id="lang-select">
              ${IDIOMAS.map(d => `<option value="${d.code}" ${d.code === idiomaActual ? 'selected' : ''}>${d.nome}</option>`).join('')}
            </select>
          </div>
        </div>
        <div class="sidebar-sep"></div>
        <div class="sidebar-steps">
          ${STEPS.map((s, i) => `
            ${s.danger && !STEPS[i - 1]?.danger ? '<div class="sidebar-sep"></div>' : ''}
            <button class="step-btn ${i === current ? 'activo' : ''} ${s.danger ? 'danger' : ''}" data-step="${i}">${t(s.key, s.id)}</button>
          `).join('')}
        </div>
        <div class="sidebar-sep"></div>
        <div class="sidebar-info">${t('app.info', 'Piztu Sistemas')}${versionCache ? ' ' + versionCache : ''}</div>
        <div class="sidebar-info">${t('app.licenza', 'Licenza AGPLv3')}</div>
      </div>
      <div class="main-panel">
        <div class="main-content" id="main-content"></div>
        <div class="main-footer">
          <div class="progress-row">
            <div class="progress-track"><div class="progress-fill" id="progress-fill"></div></div>
            <span class="progress-label" id="progress-label"></span>
          </div>
          <div class="nav-row">
            <button class="btn-main" id="btn-prev">${t('nav.anterior', '← Anterior')}</button>
            <span class="spacer"></span>
            <button class="btn-main" id="btn-close">${t('nav.pechar', 'Pechar')}</button>
            <button class="btn-main primary" id="btn-next"></button>
          </div>
        </div>
      </div>
    `;

    document.getElementById('lang-select').addEventListener('change', async (e) => {
        idiomaActual = e.target.value;
        await SetIdioma(idiomaActual);
        [I18N, instDescCache, finDescCache] = await Promise.all([Traducions(), InstalacionDesc(), CompletadoDesc()]);
        render();
    });
    document.querySelectorAll('.step-btn').forEach((btn) => {
        btn.addEventListener('click', () => goTo(parseInt(btn.dataset.step, 10)));
    });
    document.getElementById('btn-prev').addEventListener('click', () => goTo(current - 1));
    document.getElementById('btn-close').addEventListener('click', () => Quit());

    // A "Zona de perigo" queda fóra do asistente lineal (só se chega a ela
    // dende a barra lateral): tanto "completado" coma "desinstalar" ofrecen
    // "Finalizar" no canto de "Seguinte".
    const btnNext = document.getElementById('btn-next');
    const esUltimo = STEPS[current].id === 'completado' || STEPS[current].id === 'desinstalar';
    btnNext.textContent = esUltimo ? t('nav.finalizar', 'Finalizar') : t('nav.seguinte', 'Seguinte →');
    btnNext.addEventListener('click', () => {
        if (esUltimo) { Quit(); } else { goTo(current + 1); }
    });

    document.getElementById('btn-prev').disabled = current === 0;

    const pasosWizard = STEPS.filter((s) => !s.danger).length;
    const pct = pasosWizard > 1 ? (Math.min(current, pasosWizard - 1) / (pasosWizard - 1)) * 100 : 0;
    document.getElementById('progress-fill').style.width = pct + '%';
    document.getElementById('progress-label').textContent = STEPS[current].danger
        ? t('paso.desinstalar', 'Zona de perigo')
        : formatoNum(t('nav.progreso', 'Paso %d de %d'), current + 1, pasosWizard);

    renderStep();
}

function goTo(n) {
    if (n < 0 || n >= STEPS.length) return;
    current = n;
    render();
}

function renderStep() {
    const el = document.getElementById('main-content');
    switch (STEPS[current].id) {
        case 'benvida':
            el.innerHTML = benvidaHTML();
            break;
        case 'instalacion':
            el.innerHTML = instalacionHTML();
            wireInstalacion();
            break;
        case 'lamp':
            el.innerHTML = lampHTML();
            wireLamp();
            break;
        case 'filebrowser':
            el.innerHTML = filebrowserHTML();
            wireFilebrowser();
            break;
        case 'completado':
            el.innerHTML = completadoHTML();
            wireCompletado();
            break;
        case 'desinstalar':
            el.innerHTML = desinstalarHTML();
            wireDesinstalar();
            break;
    }
}

function addLogLine(box, text) {
    const div = document.createElement('div');
    div.textContent = text;
    box.appendChild(div);
    box.scrollTop = box.scrollHeight;
}

// ── Paso 0: Benvida ──────────────────────────────────────────────────────────
function benvidaHTML() {
    const bloques = [
        ['benvida.nomes.titulo', 'benvida.nomes.sub', 'benvida.nomes.corpo'],
        // Servidor e clientes tiñan dúas tarxetas que se contradicían ("o
        // servidor non precisa sudo"): en realidade o servidor tamén precisa
        // NOPASSWD para instalar e xestionar salt-master. Unha soa tarxeta.
        ['benvida.sudo.titulo', 'benvida.sudo.sub', 'benvida.sudo.corpo'],
        ['benvida.ssh.titulo', 'benvida.ssh.sub', 'benvida.ssh.corpo'],
    ];
    return `
      <h1 class="titulo">${t('benvida.titulo', 'Benvido')}</h1>
      <p class="desc">${t('benvida.desc', '')}</p>
      ${bloques.map(([ti, su, co]) => `
        <div class="card">
          <h3>${t(ti, '')}</h3>
          <p class="card-sub">${t(su, '')}</p>
          <p>${t(co, '')}</p>
        </div>
      `).join('')}
      ${!esRootCache ? `<div class="aviso">${t('benvida.aviso', '')}</div>` : ''}
    `;
}

// ── Paso 1: Instalación ──────────────────────────────────────────────────────
function instalacionHTML() {
    return `
      <div class="card">
        <h3>${t('inst.card', 'Instalación')}</h3>
        <p>${instDescCache}</p>
        <div class="sep"></div>
        <button class="btn-main primary" id="btn-instalar">${t('inst.btn', '🚀 Instalar Piztu')}</button>
        <div class="log-box" id="inst-log"><div class="placeholder">${t('inst.out.inicial', '')}</div></div>
      </div>
    `;
}

function wireInstalacion() {
    const btn = document.getElementById('btn-instalar');
    const logBox = document.getElementById('inst-log');
    btn.addEventListener('click', () => {
        btn.disabled = true;
        logBox.innerHTML = '';
        addLogLine(logBox, t('inst.iniciando', 'Iniciando instalación...'));
        Instalar();
    });
}

EventsOn('inst_log', (line) => {
    const box = document.getElementById('inst-log');
    if (box) addLogLine(box, line);
});
EventsOn('inst_completo', () => {
    const btn = document.getElementById('btn-instalar');
    if (btn) btn.disabled = false;
});

// ── Paso 2: LAMP ──────────────────────────────────────────────────────────────
function lampHTML() {
    return `
      <div class="card">
        <h3>${t('lamp.card', 'LAMP')}</h3>
        <p>${t('lamp.desc', '')}</p>
        <div class="sep"></div>
        <div class="form-grid">
          <div class="form-row">
            <label>${t('lamp.form.dbpass', '')}</label>
            <input type="password" id="lamp-dbpass" autocomplete="new-password">
          </div>
        </div>
        <button class="btn-main primary" id="btn-lamp">${t('lamp.btn', '🌐 Instalar LAMP')}</button>
        <p class="error-msg" id="lamp-error" style="display:none;"></p>
        <div class="log-box" id="lamp-log" style="display:none;"></div>
      </div>
    `;
}

function wireLamp() {
    const btn = document.getElementById('btn-lamp');
    const err = document.getElementById('lamp-error');
    const logBox = document.getElementById('lamp-log');
    btn.addEventListener('click', () => {
        const dbPass = document.getElementById('lamp-dbpass').value;
        err.style.display = 'none';
        if (!dbPass) {
            err.textContent = t('lamp.faltan', '');
            err.style.display = 'block';
            return;
        }
        btn.disabled = true;
        logBox.style.display = 'block';
        logBox.innerHTML = '';
        InstalarLamp(dbPass);
    });
}

EventsOn('lamp_log', (line) => {
    const box = document.getElementById('lamp-log');
    if (box) addLogLine(box, line);
});
EventsOn('lamp_completo', () => {
    const btn = document.getElementById('btn-lamp');
    if (btn) btn.disabled = false;
});

// ── Paso 3: File Browser ─────────────────────────────────────────────────────
function filebrowserHTML() {
    return `
      <div class="card">
        <h3>${t('filebrowser.card', 'File Browser')}</h3>
        <p>${t('filebrowser.desc', '')}</p>
        <div class="aviso-verde">
          <h4>${t('filebrowser.doususuarios.titulo', '')}</h4>
          <p>${t('filebrowser.doususuarios.corpo', '')}</p>
          <div class="check-row">
            <input type="checkbox" id="fb-check-entendido">
            <label for="fb-check-entendido">${t('filebrowser.doususuarios.check', '')}</label>
          </div>
        </div>
        <div class="sep"></div>
        <div class="form-grid">
          <div class="form-row">
            <label>${t('filebrowser.form.fbuser', '')}</label>
            <input type="text" id="fb-fbuser" autocomplete="off" value="admin">
          </div>
          <div class="form-row">
            <label>${t('filebrowser.form.fbpass', '')}</label>
            <input type="password" id="fb-fbpass" autocomplete="new-password">
          </div>
        </div>
        <button class="btn-main primary" id="btn-filebrowser" disabled>${t('filebrowser.btn', '📁 Instalar File Browser')}</button>
        <p class="error-msg" id="fb-error" style="display:none;"></p>
        <div class="log-box" id="fb-log" style="display:none;"></div>
      </div>
    `;
}

function wireFilebrowser() {
    const btn = document.getElementById('btn-filebrowser');
    const err = document.getElementById('fb-error');
    const logBox = document.getElementById('fb-log');
    const chk = document.getElementById('fb-check-entendido');
    // O botón queda bloqueado ata que se marque o checkbox: "admin" (File
    // Browser) e "director" (Tao) confúndense a miúdo, e isto obriga a lelo
    // antes de crear a conta.
    chk.addEventListener('change', () => { btn.disabled = !chk.checked; });
    btn.addEventListener('click', () => {
        const fbUser = document.getElementById('fb-fbuser').value.trim();
        const fbPass = document.getElementById('fb-fbpass').value;
        err.style.display = 'none';
        if (!fbUser || !fbPass) {
            err.textContent = t('filebrowser.faltan', '');
            err.style.display = 'block';
            return;
        }
        // O nome de usuario admite só letras, díxitos, punto, guión e guión
        // baixo. Ademais de ser o que espera File Browser, isto detén o erro
        // clásico de escribir AQUÍ o contrasinal (que adoita levar símbolos):
        // un login de proba non o detectaría, porque a conta créase con
        // exactamente o que se teclee e autenticaría igual.
        if (!/^[A-Za-z0-9._-]+$/.test(fbUser)) {
            err.textContent = t('filebrowser.user.invalido', '');
            err.style.display = 'block';
            return;
        }
        if (fbPass.length < 12) {
            err.textContent = t('filebrowser.pass.curto', '');
            err.style.display = 'block';
            return;
        }
        btn.disabled = true;
        logBox.style.display = 'block';
        logBox.innerHTML = '';
        InstalarFileBrowser(fbUser, fbPass);
    });
}

EventsOn('filebrowser_log', (line) => {
    const box = document.getElementById('fb-log');
    if (box) addLogLine(box, line);
});
EventsOn('filebrowser_completo', () => {
    const btn = document.getElementById('btn-filebrowser');
    if (btn) btn.disabled = false;
});

// ── Paso 3: Completado ────────────────────────────────────────────────────────
function completadoHTML() {
    return `
      <div class="card">
        <h3>${t('fin.card', '')}</h3>
        <p>${finDescCache}</p>
        <button class="btn-main primary" id="btn-shortcuts">${t('fin.btn', '')}</button>
        <p id="fin-status"></p>
      </div>
    `;
}

function wireCompletado() {
    const btn = document.getElementById('btn-shortcuts');
    const status = document.getElementById('fin-status');
    btn.addEventListener('click', async () => {
        btn.disabled = true;
        status.className = '';
        status.textContent = '';
        try {
            await CrearAccesosDirectos();
            status.className = 'success-msg';
            status.textContent = t('fin.creados', '');
        } catch (e) {
            status.className = 'error-msg';
            status.textContent = t('fin.erro', 'Erro: %s').replace('%s', e);
        }
        btn.disabled = false;
    });
}

// ── Zona de perigo: desinstalación ──────────────────────────────────────────
// Tres botóns independentes, separados dos pasos do asistente (só se accede
// por aquí dende a barra lateral, non dende o fluxo "Seguinte →"): cada un
// require unha confirmación explícita porque son accións irreversibles.
//
// Orde das tarxetas = inversa á da instalación (Piztu → LAMP → File Browser):
// primeiro File Browser, logo LAMP e Piztu de último. Así, quen segue as
// tarxetas de arriba a abaixo desinstala Piztu ao final e non antes — se o
// fixese primeiro, DesinstalarPiztu borra /opt/piztu enteiro (cos playbooks
// desinstalar_*.yaml dentro). Funcionar, funciona igual (playbookDesinstalador
// cae á copia embebida no instalador), pero a orde visible non debe convidar
// a deixar LAMP/File Browser sen xeito de quitar dende o disco.
function desinstalarHTML() {
    return `
      <h1 class="titulo">${t('desinst.titulo', '⚠️ Zona de perigo')}</h1>
      <p class="desc">${t('desinst.intro', '')}</p>

      <div class="card danger">
        <h3>${t('desinst.fb.card', '')}</h3>
        <p>${t('desinst.fb.desc', '')}</p>
        <div class="sep"></div>
        <button class="btn-main danger" id="btn-desinst-fb">${t('desinst.fb.btn', '')}</button>
        <div class="log-box" id="desinst-fb-log" style="display:none;"></div>
      </div>

      <div class="card danger">
        <h3>${t('desinst.lamp.card', '')}</h3>
        <p>${t('desinst.lamp.desc', '')}</p>
        <div class="sep"></div>
        <button class="btn-main danger" id="btn-desinst-lamp">${t('desinst.lamp.btn', '')}</button>
        <div class="log-box" id="desinst-lamp-log" style="display:none;"></div>
      </div>

      <div class="card danger">
        <h3>${t('desinst.piztu.card', '')}</h3>
        <p>${t('desinst.piztu.desc', '')}</p>
        <div class="sep"></div>
        <div class="form-grid">
          <div class="form-row">
            <label>${formatoNum(t('desinst.confirmar.label', 'Escribe %s para confirmar'), t('desinst.piztu.palabra', 'DESINSTALAR'))}</label>
            <input type="text" id="desinst-piztu-confirm" autocomplete="off">
          </div>
        </div>
        <button class="btn-main danger" id="btn-desinst-piztu" disabled>${t('desinst.piztu.btn', '')}</button>
        <div class="log-box" id="desinst-piztu-log" style="display:none;"></div>
      </div>
    `;
}

function wireDesinstalar() {
    // Piztu completo: o botón só se activa escribindo a palabra de
    // confirmación, e ao premelo pide ademais un confirm() nativo — é
    // irreversible (borra /opt/piztu, salt-master e as súas chaves).
    const inputPiztu = document.getElementById('desinst-piztu-confirm');
    const btnPiztu = document.getElementById('btn-desinst-piztu');
    const palabra = t('desinst.piztu.palabra', 'DESINSTALAR');
    inputPiztu.addEventListener('input', () => {
        btnPiztu.disabled = inputPiztu.value.trim().toUpperCase() !== palabra.toUpperCase();
    });
    btnPiztu.addEventListener('click', () => {
        if (!window.confirm(t('desinst.piztu.confirm', ''))) return;
        btnPiztu.disabled = true;
        inputPiztu.disabled = true;
        const logBox = document.getElementById('desinst-piztu-log');
        logBox.style.display = 'block';
        logBox.innerHTML = '';
        DesinstalarPiztu();
    });

    const btnFb = document.getElementById('btn-desinst-fb');
    btnFb.addEventListener('click', () => {
        if (!window.confirm(t('desinst.fb.confirm', ''))) return;
        btnFb.disabled = true;
        const logBox = document.getElementById('desinst-fb-log');
        logBox.style.display = 'block';
        logBox.innerHTML = '';
        DesinstalarFileBrowser();
    });

    const btnLamp = document.getElementById('btn-desinst-lamp');
    btnLamp.addEventListener('click', () => {
        if (!window.confirm(t('desinst.lamp.confirm', ''))) return;
        btnLamp.disabled = true;
        const logBox = document.getElementById('desinst-lamp-log');
        logBox.style.display = 'block';
        logBox.innerHTML = '';
        DesinstalarLamp();
    });
}

EventsOn('desinstpiztu_log', (line) => {
    const box = document.getElementById('desinst-piztu-log');
    if (box) addLogLine(box, line);
});
EventsOn('desinstpiztu_completo', () => {
    // Non se reactiva o botón: se fallou, hai que revisar o log antes de
    // volver intentalo, e se non fallou xa non hai nada que desinstalar.
    const input = document.getElementById('desinst-piztu-confirm');
    if (input) input.disabled = false;
});

EventsOn('desinstfb_log', (line) => {
    const box = document.getElementById('desinst-fb-log');
    if (box) addLogLine(box, line);
});
EventsOn('desinstfb_completo', () => {
    const btn = document.getElementById('btn-desinst-fb');
    if (btn) btn.disabled = false;
});

EventsOn('desinstlamp_log', (line) => {
    const box = document.getElementById('desinst-lamp-log');
    if (box) addLogLine(box, line);
});
EventsOn('desinstlamp_completo', () => {
    const btn = document.getElementById('btn-desinst-lamp');
    if (btn) btn.disabled = false;
});

// ── Arranque ──────────────────────────────────────────────────────────────────
async function bootstrap() {
    const permisoDenegado = await PermisoDenegado().catch(() => false);
    if (permisoDenegado) {
        idiomaActual = await Idioma().catch(() => 'gl');
        I18N = await Traducions().catch(() => ({}));
        renderPermisoDenegado();
        return;
    }

    [idiomaActual, I18N, IDIOMAS, esRootCache, destDirCache, instDescCache, finDescCache, versionCache] = await Promise.all([
        Idioma(), Traducions(), Idiomas(), EsRoot(), DestDir(), InstalacionDesc(), CompletadoDesc(), Version().catch(() => ''),
    ]);
    render();
}

bootstrap();
