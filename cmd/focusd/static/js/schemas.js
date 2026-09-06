// ─── Bases de datos y esquemas: selectores activos + crear / eliminar ─────────
// Jerarquía: servidor → bases de datos → esquemas → tablas. La base activa
// contiene los esquemas visibles; el esquema activo califica las consultas del
// editor, el explorador y el diagrama. Ambos persisten en localStorage.
import { state, persistSchema, persistDatabase } from './state.js';
import { render, escHtml, escAttr, showToast } from './dom.js';
import { apiDatabases, apiSchemas, apiQuery } from './api.js';
import { closeCellModal } from './results.js';

let databases = [];  // [{name, schemas, tables, views, isDefault}]
let schemas = [];    // [{name, tables, views, isDefault}] de la base activa

export function getDatabases() { return databases; }
export function getSchemas() { return schemas; }

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

// ─── Bases de datos ───────────────────────────────────────────────────────────
// Carga la lista y sincroniza el selector. Si la base activa desapareció
// (DROP DATABASE desde SQL o desde otra sesión), vuelve a postgres.
export async function loadDatabases() {
  databases = (await apiDatabases()) || [];
  if (!databases.some(d => d.name === state.database)) {
    persistDatabase('postgres');
  }
  renderSelect('database-select', databases, state.database);
  return databases;
}

export async function setActiveDatabase(name) {
  if (!name || name === state.database) return;
  persistDatabase(name);           // el esquema activo vuelve a public
  renderSelect('database-select', databases, state.database);
  showToast(`Base de datos activa: ${name}`);
  const { onSchemaChanged } = await import('./app.js');
  onSchemaChanged();
}

export function databaseCreateOpen() {
  const host = modalHost();
  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:440px">
        <div class="modal-hdr"><span>Nueva base de datos</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">
          <label class="ins-field"><span>Nombre <em>letras, dígitos y _</em></span>
            <input id="db-new-name" placeholder="p. ej. inventario" autocomplete="off"></label>
          <label class="chk-row"><input type="checkbox" id="db-new-activate" checked> Activar al crear</label>
          <p class="modal-note">Equivale a <code>CREATE DATABASE nombre</code>. Cada base es un contenedor aislado con su esquema <code>public</code>, sus procedimientos, triggers y jobs. Una consulta no puede cruzar bases.</p>
        </div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Cancelar</button>
          <button class="btn btn-run" onclick="databaseCreateConfirm()">Crear</button>
        </div>
      </div>
    </div>`);
  const input = host.querySelector('#db-new-name');
  input?.focus();
  input?.addEventListener('keydown', e => { if (e.key === 'Enter') databaseCreateConfirm(); });
}

export async function databaseCreateConfirm() {
  const host = document.getElementById('cell-modal-host');
  const input = host?.querySelector('#db-new-name');
  const name = (input?.value || '').trim();
  if (!NAME_RE.test(name)) { showToast('Nombre inválido: usá letras, dígitos y _', 'err'); input?.focus(); return; }
  if (databases.some(d => d.name === name)) { showToast(`La base ${name} ya existe`, 'err'); return; }
  const activate = !!host.querySelector('#db-new-activate')?.checked;

  const res = await apiQuery(`CREATE DATABASE ${name}`);
  if (res.error) { showToast(res.error, 'err'); return; }
  closeCellModal();
  showToast(`Base de datos ${name} creada`);
  await loadDatabases();
  if (activate) {
    await setActiveDatabase(name);
  } else {
    const { refreshSidebar } = await import('./app.js');
    refreshSidebar();
  }
}

export function databaseDropOpen(name) {
  const info = databases.find(d => d.name === name);
  if (!info || info.isDefault) return;
  const objs = (info.tables || 0) + (info.views || 0);
  render(modalHost(), `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:460px">
        <div class="modal-hdr"><span>Eliminar base de datos ${escHtml(name)}</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">
          <p>Contiene <b>${info.schemas}</b> esquema${info.schemas !== 1 ? 's' : ''}, <b>${info.tables}</b> tabla${info.tables !== 1 ? 's' : ''} y <b>${info.views}</b> vista${info.views !== 1 ? 's' : ''}${objs ? ', además de sus procedimientos, triggers y jobs' : ''}. Se borra <b>todo</b> su contenido y no se puede deshacer.</p>
          <label class="chk-row"><input type="checkbox" id="db-drop-confirm"> <span>Entiendo, eliminar <code>${escHtml(name)}</code> con todo su contenido</span></label>
          <p class="modal-note">Equivale a <code>DROP DATABASE ${escHtml(name)}</code> ejecutado desde <code>postgres</code>.</p>
        </div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Conservar</button>
          <button class="btn btn-cancel" style="display:inline-flex" onclick="databaseDropConfirm('${escAttr(name)}')">Eliminar</button>
        </div>
      </div>
    </div>`);
}

export async function databaseDropConfirm(name) {
  const host = document.getElementById('cell-modal-host');
  if (!host?.querySelector('#db-drop-confirm')?.checked) { showToast('Marcá la casilla para confirmar el borrado', 'err'); return; }

  // DROP DATABASE no puede correr dentro de la base que se elimina: va por postgres.
  const res = await apiQuery(`DROP DATABASE ${name}`, { database: 'postgres', schema: 'public' });
  if (res.error) { showToast(res.error, 'err'); return; }
  closeCellModal();
  showToast(`Base de datos ${name} eliminada`);
  const wasActive = state.database === name;
  await loadDatabases();
  const { onSchemaChanged, refreshSidebar } = await import('./app.js');
  if (wasActive) onSchemaChanged(); else refreshSidebar();
}

// ─── Esquemas (de la base activa) ─────────────────────────────────────────────
// Carga la lista y sincroniza el selector del header. Si el esquema activo
// desapareció (DROP SCHEMA desde SQL), vuelve a public.
export async function loadSchemas() {
  schemas = (await apiSchemas()) || [];
  if (!Array.isArray(schemas)) schemas = [];   // 404: la base ya no existe
  if (!schemas.some(s => s.name === state.schema)) {
    persistSchema('public');
  }
  renderSelect('schema-select', schemas, state.schema);
  return schemas;
}

export async function setActiveSchema(name) {
  if (!name || name === state.schema) return;
  persistSchema(name);
  renderSelect('schema-select', schemas, state.schema);
  showToast(`Esquema activo: ${name}`);
  const { onSchemaChanged } = await import('./app.js');
  onSchemaChanged();
}

export function schemaCreateOpen() {
  const host = modalHost();
  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:440px">
        <div class="modal-hdr"><span>Nuevo esquema en ${escHtml(state.database)}</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">
          <label class="ins-field"><span>Nombre <em>letras, dígitos y _</em></span>
            <input id="schema-new-name" placeholder="p. ej. ventas_2026" autocomplete="off"></label>
          <label class="chk-row"><input type="checkbox" id="schema-new-activate" checked> Activar al crear</label>
          <p class="modal-note">Equivale a <code>CREATE SCHEMA nombre</code> dentro de la base activa. Las tablas, vistas e índices viven dentro del esquema; procedimientos, triggers y jobs pertenecen a la base.</p>
        </div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Cancelar</button>
          <button class="btn btn-run" onclick="schemaCreateConfirm()">Crear</button>
        </div>
      </div>
    </div>`);
  const input = host.querySelector('#schema-new-name');
  input?.focus();
  input?.addEventListener('keydown', e => { if (e.key === 'Enter') schemaCreateConfirm(); });
}

export async function schemaCreateConfirm() {
  const host = document.getElementById('cell-modal-host');
  const input = host?.querySelector('#schema-new-name');
  const name = (input?.value || '').trim();
  if (!NAME_RE.test(name)) { showToast('Nombre inválido: usá letras, dígitos y _', 'err'); input?.focus(); return; }
  if (schemas.some(s => s.name === name)) { showToast(`El esquema ${name} ya existe`, 'err'); return; }
  const activate = !!host.querySelector('#schema-new-activate')?.checked;

  const res = await apiQuery(`CREATE SCHEMA ${name}`);
  if (res.error) { showToast(res.error, 'err'); return; }
  closeCellModal();
  showToast(`Esquema ${name} creado`);
  await loadSchemas();
  if (activate) {
    await setActiveSchema(name);
  } else {
    const { refreshSidebar } = await import('./app.js');
    refreshSidebar();
  }
}

export function schemaDropOpen(name) {
  const info = schemas.find(s => s.name === name);
  if (!info || info.isDefault) return;
  const objs = (info.tables || 0) + (info.views || 0);
  const detail = objs
    ? `Contiene <b>${info.tables}</b> tabla${info.tables !== 1 ? 's' : ''} y <b>${info.views}</b> vista${info.views !== 1 ? 's' : ''}. Para borrarlo con todo su contenido marcá CASCADE.`
    : 'El esquema está vacío.';
  render(modalHost(), `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:460px">
        <div class="modal-hdr"><span>Eliminar esquema ${escHtml(name)}</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">
          <p>${detail}</p>
          ${objs ? `<label class="chk-row"><input type="checkbox" id="schema-drop-cascade"> <span>CASCADE — borrar también sus ${objs} objeto${objs !== 1 ? 's' : ''} (no se puede deshacer)</span></label>` : ''}
          <p class="modal-note">Equivale a <code>DROP SCHEMA ${escHtml(name)}${objs ? ' [CASCADE]' : ''}</code> en la base <code>${escHtml(state.database)}</code>.</p>
        </div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Conservar</button>
          <button class="btn btn-cancel" style="display:inline-flex" onclick="schemaDropConfirm('${escAttr(name)}')">Eliminar</button>
        </div>
      </div>
    </div>`);
}

export async function schemaDropConfirm(name) {
  const host = document.getElementById('cell-modal-host');
  const cascade = !!host?.querySelector('#schema-drop-cascade')?.checked;
  const info = schemas.find(s => s.name === name);
  const objs = info ? (info.tables || 0) + (info.views || 0) : 0;
  if (objs && !cascade) { showToast('El esquema no está vacío: marcá CASCADE para borrarlo con su contenido', 'err'); return; }

  const res = await apiQuery(`DROP SCHEMA ${name}${cascade ? ' CASCADE' : ''}`);
  if (res.error) { showToast(res.error, 'err'); return; }
  closeCellModal();
  showToast(`Esquema ${name} eliminado`);
  const wasActive = state.schema === name;
  await loadSchemas();
  const { onSchemaChanged, refreshSidebar } = await import('./app.js');
  if (wasActive) onSchemaChanged(); else refreshSidebar();
}

// ─── Helpers ──────────────────────────────────────────────────────────────────
function renderSelect(id, items, active) {
  const sel = document.getElementById(id);
  if (!sel) return;
  const opts = items.map(it => {
    const o = document.createElement('option');
    o.value = it.name;
    o.textContent = it.name;
    o.selected = it.name === active;
    return o;
  });
  sel.replaceChildren(...opts);
}

function modalHost() {
  let host = document.getElementById('cell-modal-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'cell-modal-host';
    document.body.appendChild(host);
  }
  return host;
}
