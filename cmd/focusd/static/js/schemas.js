// ─── Esquemas: selector de esquema activo + crear / eliminar ──────────────────
// En FocusDB una "base de datos" es un alias de esquema (mismo catálogo y
// almacenamiento), así que el gestor expone esquemas: el activo califica las
// consultas del editor, el explorador y el diagrama.
import { state, persistSchema } from './state.js';
import { render, escHtml, escAttr, showToast } from './dom.js';
import { apiSchemas, apiQuery } from './api.js';
import { closeCellModal } from './results.js';

let schemas = [];   // [{name, tables, views, isDefault}]

export function getSchemas() { return schemas; }

// Carga la lista y sincroniza el selector del header. Si el esquema activo
// desapareció (DROP SCHEMA desde SQL), vuelve a public.
export async function loadSchemas() {
  schemas = (await apiSchemas()) || [];
  if (!schemas.some(s => s.name === state.schema)) {
    persistSchema('public');
  }
  renderSchemaSelect();
  return schemas;
}

function renderSchemaSelect() {
  const sel = document.getElementById('schema-select');
  if (!sel) return;
  const opts = schemas.map(s => {
    const o = document.createElement('option');
    o.value = s.name;
    o.textContent = s.name;
    o.selected = s.name === state.schema;
    return o;
  });
  sel.replaceChildren(...opts);
}

export async function setActiveSchema(name) {
  if (!name || name === state.schema) return;
  persistSchema(name);
  renderSchemaSelect();
  showToast(`Esquema activo: ${name}`);
  const { onSchemaChanged } = await import('./app.js');
  onSchemaChanged();
}

// ─── Modales ──────────────────────────────────────────────────────────────────
function modalHost() {
  let host = document.getElementById('cell-modal-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'cell-modal-host';
    document.body.appendChild(host);
  }
  return host;
}

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function schemaCreateOpen() {
  const host = modalHost();
  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:440px">
        <div class="modal-hdr"><span>Nuevo esquema</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">
          <label class="ins-field"><span>Nombre <em>letras, dígitos y _</em></span>
            <input id="schema-new-name" placeholder="p. ej. ventas_2026" autocomplete="off"></label>
          <label class="chk-row"><input type="checkbox" id="schema-new-activate" checked> Activar al crear</label>
          <p class="modal-note">Equivale a <code>CREATE SCHEMA nombre</code>. Las tablas, vistas e índices viven dentro del esquema; procedimientos, triggers y jobs son globales.</p>
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
  if (schemas.some(s => s.name.toLowerCase() === name.toLowerCase())) { showToast(`El esquema ${name} ya existe`, 'err'); return; }
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
          <p class="modal-note">Equivale a <code>DROP SCHEMA ${escHtml(name)}${objs ? ' [CASCADE]' : ''}</code>.</p>
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
