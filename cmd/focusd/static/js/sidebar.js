// ─── Sidebar: árbol de objetos ────────────────────────────────────────────────
import { state } from './state.js';
import { icon } from './icons.js';
import { render, escHtml, escAttr } from './dom.js';
import { apiSchema, apiObjects } from './api.js';
import { runQuery } from './editor.js';
import { loadSchemas, getSchemas } from './schemas.js';

let schemaData  = [];
let objectsData = { triggers: [], jobs: [], procedures: [] };
let tableNames = [], colNames = [];

export async function loadSidebar() {
  try {
    // Primero los esquemas (puede corregir state.schema si el activo ya no existe).
    await loadSchemas();
    const [schemaRes, objRes] = await Promise.all([apiSchema(), apiObjects()]);
    schemaData  = Array.isArray(schemaRes) ? schemaRes : [];
    objectsData = objRes    || { triggers: [], jobs: [], procedures: [] };

    tableNames = schemaData.map(t => t.name);
    colNames   = schemaData.flatMap(t => (t.columns||[]).map(c => c.name));

    renderSidebar('');
    document.getElementById('conn-badge').textContent = '● conectado';
  } catch(e) {
    document.getElementById('conn-badge').style.color = 'var(--red)';
    document.getElementById('conn-badge').textContent = '● sin conexión';
    render(document.getElementById('sb-tree'), '<div class="empty-state" style="height:120px"><p>No se pudo alcanzar el servidor</p></div>');
  }
}

export function buildHintTables() {
  const tables = {};
  schemaData.forEach(t => {
    tables[t.name] = (t.columns||[]).map(c => c.name);
  });
  return tables;
}

export function renderSidebar(filter) {
  const tree = document.getElementById('sb-tree');
  const f = filter.toLowerCase();

  let html = '';

  // ── Schemas: el activo se resalta; click cambia el esquema activo.
  const schemaItems = getSchemas().filter(s => !f || s.name.toLowerCase().includes(f));
  html += sectionHTML(icon('schema'), 'Schemas', 'sec-schemas', schemaItems.map(s => {
    const active = s.name === state.schema;
    const summary = `${s.tables} tabla${s.tables !== 1 ? 's' : ''} · ${s.views} vista${s.views !== 1 ? 's' : ''}`;
    return `
      <div class="t-item ${active ? 'selected' : ''}" onclick="setActiveSchema('${escAttr(s.name)}')"
           title="${escAttr(summary + (active ? ' · activo' : ' · click para activar'))}">
        <span class="ico">${icon('schema', 13)}</span>
        <span class="lbl">${escHtml(s.name)}</span>
        <span class="cnt">${s.tables + s.views}</span>
        ${s.isDefault ? '' : `<button class="t-act" title="Eliminar esquema"
          onclick="event.stopPropagation();schemaDropOpen('${escAttr(s.name)}')">×</button>`}
      </div>`;
  }), schemaItems.length,
    `<button class="sec-act" title="Nuevo esquema" onclick="event.stopPropagation();schemaCreateOpen()">+</button>`);

  // ── Tables / Views section (del esquema activo)
  const tableItems = schemaData.filter(t =>
    t.kind === 'BASE TABLE' && (!f || t.name.toLowerCase().includes(f))
  );
  const viewItems = schemaData.filter(t =>
    t.kind === 'VIEW' && (!f || t.name.toLowerCase().includes(f))
  );

  html += sectionHTML(icon('table'), 'Tables', 'sec-tables', tableItems.map(t => {
    const colsHTML = (t.columns||[]).map(c => {
      let marks = '';
      if (c.isPK) marks += '<span class="pk" title="PRIMARY KEY">●</span>';
      return `<div class="col-row" onclick="insertColRef('${t.name}','${c.name}')">
        <span>${c.name}</span>${marks}
        <span class="col-type">${escHtml(c.type)}</span>
      </div>`;
    }).join('');
    const rc = typeof t.rowCount === 'number' ? t.rowCount : null;
    return `
      <div class="t-item" onclick="toggleCols(this)" data-table="${escAttr(t.name)}">
        <span class="ico">${icon('caret', 12, 'ico-caret')}</span>
        <span class="lbl">${escHtml(t.name)}</span>
        ${rc !== null ? `<span class="cnt" title="${rc} filas">${rc}</span>` : ''}
        <button class="t-act" title="Explorar datos"
          onclick="event.stopPropagation();openExplorer('${escAttr(t.name)}')">${icon('table', 12)}</button>
        <button class="t-act" title="SELECT *"
          onclick="event.stopPropagation();quickSelect('${escAttr(t.name)}')">${icon('caret', 11)}</button>
      </div>
      <div class="col-block" style="display:none">${colsHTML}</div>`;
  }), tableItems.length);

  html += sectionHTML(icon('view'), 'Views', 'sec-views', viewItems.map(t =>
    `<div class="t-item" onclick="quickSelect('${escAttr(t.name)}')">
       <span class="ico">${icon('view', 13)}</span>
       <span class="lbl">${escHtml(t.name)}</span>
     </div>`
  ), viewItems.length);

  // ── Procedures
  const procs = objectsData.procedures.filter(p => !f || p.name.toLowerCase().includes(f));
  html += sectionHTML(icon('proc'), 'Procedures', 'sec-procs', procs.map(p => {
    const sig = p.params && p.params.length ? p.params.join(', ') : 'no params';
    return `<div class="t-item" title="${escAttr(sig)}" onclick="insertCall('${escAttr(p.name)}')">
              <span class="ico">λ</span>
              <span class="lbl">${escHtml(p.name)}</span>
            </div>`;
  }), procs.length);

  // ── Triggers
  const trigs = objectsData.triggers.filter(t => !f || t.name.toLowerCase().includes(f));
  html += sectionHTML(icon('trigger'), 'Triggers', 'sec-trigs', trigs.map(t =>
    `<div class="t-item" title="${escAttr(t.timing+' '+t.event+' ON '+t.table)}">
       <span class="ico">${icon('trigger', 13)}</span>
       <span class="lbl">${escHtml(t.name)}</span>
       <span class="cnt" style="color:var(--yellow)">${t.event}</span>
     </div>`
  ), trigs.length);

  // ── Jobs
  const jobs = objectsData.jobs.filter(j => !f || j.name.toLowerCase().includes(f));
  html += sectionHTML(icon('job'), 'Jobs', 'sec-jobs', jobs.map(j => {
    const jstate = j.enabled ? 'on' : 'off';
    return `<div class="t-item" title="cada ${j.interval} ${j.unit}${j.enabled ? '' : ' · en pausa'}">
              <span class="ico"><span class="dot ${jstate}"></span></span>
              <span class="lbl">${escHtml(j.name)}</span>
              <span class="cnt">cada ${j.interval} ${j.unit.toLowerCase()}</span>
            </div>`;
  }), jobs.length);

  render(tree, html || '<div class="empty-state" style="height:80px"><p>Sin objetos</p></div>');
}

function sectionHTML(icon, label, id, itemsHTML, count, actionHTML = '') {
  return `
    <div class="tree-section">
      <div class="sec-hdr" onclick="toggleSection('${id}')">
        <span class="arr">▾</span> <span class="sec-ico">${icon}</span> ${label}
        ${count > 0 ? `<span class="cnt" style="margin-left:auto">${count}</span>` : ''}
        ${actionHTML}
      </div>
      <div class="sec-body" id="${id}">
        ${itemsHTML.length ? itemsHTML.join('') : '<div style="padding:4px 18px;font-size:11px;color:var(--dim)">vacío</div>'}
      </div>
    </div>`;
}

export function toggleSection(id) {
  const body = document.getElementById(id);
  const hdr  = body.previousElementSibling;
  body.classList.toggle('hidden');
  hdr.classList.toggle('closed');
}

export function toggleCols(el) {
  const block = el.nextElementSibling;
  const caret = el.querySelector('.ico-caret');
  const open  = block.style.display === 'none';
  block.style.display = open ? 'block' : 'none';
  if (caret) caret.classList.toggle('open', open);
}

export function filterTree(v) { renderSidebar(v); }

export function quickSelect(name) {
  state.editor.setValue(`SELECT *\nFROM ${name}\nLIMIT 100;`);
  state.editor.focus();
  runQuery();
}

export function insertColRef(table, col) {
  state.editor.replaceSelection(col);
  state.editor.focus();
}

export function insertCall(name) {
  state.editor.setValue(`CALL ${name}();`);
  state.editor.focus();
}

// ─── Sidebar responsive toggle ────────────────────────────────────────────────
export function toggleSidebar() {
  const sb  = document.getElementById('sidebar');
  const bd  = document.getElementById('sidebar-backdrop');
  const open = sb.classList.toggle('open');
  bd.classList.toggle('open', open);
}

export function closeSidebar() {
  document.getElementById('sidebar').classList.remove('open');
  document.getElementById('sidebar-backdrop').classList.remove('open');
}

// Close sidebar on Escape
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeSidebar();
});
