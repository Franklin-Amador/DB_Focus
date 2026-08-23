// ─── Pestañas de consulta ─────────────────────────────────────────────────────
// Un único CodeMirror; cada pestaña es un CodeMirror.Doc (undo/redo propio) que
// se activa con swapDoc(). Estado persistido (debounced) en localStorage.
import { state } from './state.js';
import { render, escHtml } from './dom.js';

const LS_KEY = 'focusdb.tabs.v1';
const MAX_TABS = 12;

let tabs = [];        // [{id, title, doc}]
let activeId = null;
let saveTimer = null;
let counter = 1;

function loadPersisted() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return null;
    const data = JSON.parse(raw);
    if (!data || !Array.isArray(data.tabs) || !data.tabs.length) return null;
    return data;
  } catch (_) { return null; }
}

function persist() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(LS_KEY, JSON.stringify({
        tabs: tabs.map(t => ({ id: t.id, title: t.title, sql: t.doc.getValue() })),
        activeId,
      }));
    } catch (_) {}
  }, 500);
}

function newDoc(sql) {
  return CodeMirror.Doc(sql || '', 'text/x-sql');
}

function nextTitle() {
  return `Consulta ${counter++}`;
}

// initTabs se llama tras crear el editor. Migración: si no hay estado guardado,
// el contenido actual del editor pasa a ser la primera pestaña.
export function initTabs() {
  const saved = loadPersisted();
  if (saved) {
    tabs = saved.tabs.slice(0, MAX_TABS).map(t => ({ id: t.id, title: t.title, doc: newDoc(t.sql) }));
    const ids = tabs.map(t => t.id);
    activeId = ids.includes(saved.activeId) ? saved.activeId : ids[0];
    // counter continúa después del mayor "Consulta N" existente
    const nums = tabs.map(t => { const m = t.title.match(/^Consulta (\d+)$/); return m ? +m[1] : 0; });
    counter = Math.max(1, ...nums) + 1;
    const active = tabs.find(t => t.id === activeId);
    state.editor.swapDoc(active.doc);
  } else {
    const t = { id: 'tab-' + Date.now(), title: nextTitle(), doc: state.editor.getDoc() };
    tabs = [t];
    activeId = t.id;
  }
  state.editor.on('change', persist);
  renderTabStrip();
}

export function addTab(sql = '') {
  if (tabs.length >= MAX_TABS) return;
  const t = { id: 'tab-' + Date.now() + '-' + Math.random().toString(36).slice(2, 6), title: nextTitle(), doc: newDoc(sql) };
  tabs.push(t);
  selectTab(t.id);
}

export function selectTab(id) {
  const t = tabs.find(x => x.id === id);
  if (!t) return;
  activeId = id;
  state.editor.swapDoc(t.doc);
  state.editor.focus();
  renderTabStrip();
  persist();
}

export function closeTab(id) {
  const idx = tabs.findIndex(x => x.id === id);
  if (idx === -1) return;
  tabs.splice(idx, 1);
  if (!tabs.length) {
    // siempre queda al menos una pestaña
    const t = { id: 'tab-' + Date.now(), title: nextTitle(), doc: newDoc('') };
    tabs = [t];
  }
  if (activeId === id) {
    const next = tabs[Math.min(idx, tabs.length - 1)];
    activeId = next.id;
    state.editor.swapDoc(next.doc);
  }
  renderTabStrip();
  persist();
}

export function renameTab(id) {
  const t = tabs.find(x => x.id === id);
  if (!t) return;
  const name = prompt('Nombre de la pestaña:', t.title);
  if (name && name.trim()) {
    t.title = name.trim().slice(0, 40);
    renderTabStrip();
    persist();
  }
}

function renderTabStrip() {
  const strip = document.getElementById('tab-strip');
  if (!strip) return;
  const html = tabs.map(t => `
    <div class="qtab ${t.id === activeId ? 'active' : ''}" data-tab="${t.id}"
         onclick="selectTab('${t.id}')" ondblclick="renameTab('${t.id}')" title="Doble clic para renombrar">
      <span class="qtab-leaf"><svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M8 14C8 9 8 6 8 3"/><path d="M8 9C5 8.5 3.5 6.5 3.4 4 6 4.2 7.6 6 8 8.4Z"/></svg></span>
      <span class="qtab-title">${escHtml(t.title)}</span>
      <span class="qtab-close" onclick="event.stopPropagation();closeTab('${t.id}')" title="Cerrar">×</span>
    </div>`).join('');
  render(strip, html + `<button class="qtab-add" onclick="addTab()" title="Nueva pestaña (Ctrl+T)">+</button>`);
}
