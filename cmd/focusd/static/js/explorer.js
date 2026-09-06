// ─── Explorador de datos ──────────────────────────────────────────────────────
// Grilla paginada server-side (/api/table-data). Las escrituras van por
// /api/query para pasar por executor → triggers → persistencia. La edición
// inline requiere PRIMARY KEY; sin PK la tabla queda en solo-lectura.
import { render, escHtml, escAttr, showToast } from './dom.js';
import { apiTableData, apiQuery } from './api.js';
import { refreshSidebar } from './app.js';
import { state } from './state.js';

const LIMIT = 100;

let ex = null;  // {database, schema, table, offset, total, columns, pk, rows}

// Las escrituras corren en la base y el esquema del explorador (capturados al
// abrirlo), aunque el usuario cambie los activos mientras tanto.
function runWrite(sql) { return apiQuery(sql, { database: ex.database, schema: ex.schema }); }

function quoteIdent(name) {
  return /^[a-z_][a-z0-9_]*$/.test(name) ? name : '"' + name.replace(/"/g, '""') + '"';
}

function sqlLiteral(v, type) {
  if (v === null || v === undefined || v === '') return 'NULL';
  const t = (type || '').toUpperCase();
  const isNum = t.includes('INT') || t.includes('NUMERIC') || t.includes('FLOAT') || t.includes('DECIMAL') || t.includes('REAL');
  if (isNum && /^-?\d+(\.\d+)?$/.test(String(v).trim())) return String(v).trim();
  return `'${String(v).replace(/'/g, "''")}'`;
}

// WHERE por PK de una fila (todas las columnas PK en AND).
function pkWhere(row) {
  return ex.pk.map(pkCol => {
    const idx = ex.columns.findIndex(c => c.name === pkCol);
    return `${quoteIdent(pkCol)} = ${sqlLiteral(row[idx], ex.columns[idx]?.type)}`;
  }).join(' AND ');
}

export async function openExplorer(table) {
  const { showView } = await import('./app.js');
  showView('explorer');
  ex = { database: state.database, schema: state.schema, table, offset: 0, total: 0, columns: [], pk: [], rows: [] };
  await loadExplorerPage(0);
}

export async function loadExplorerPage(offset) {
  if (!ex) return;
  const statusEl = document.getElementById('explorer-status');
  if (statusEl) statusEl.textContent = 'Cargando…';
  try {
    const data = await apiTableData(ex.table, offset, LIMIT, ex.schema, ex.database);
    if (data.error) {
      showToast(data.error, 'err');
      if (statusEl) statusEl.textContent = data.error;
      return;
    }
    ex.offset = data.offset;
    ex.total = data.total;
    ex.columns = data.columns || [];
    ex.pk = data.pk || [];
    ex.rows = data.rows || [];
    renderExplorer();
  } catch (e) {
    showToast('Error de red: ' + e.message, 'err');
  }
}

export function explorerRefresh() {
  if (ex) loadExplorerPage(ex.offset);
}

export function explorerPage(delta) {
  if (!ex) return;
  const next = Math.max(0, ex.offset + delta * LIMIT);
  if (next >= ex.total && delta > 0) return;
  loadExplorerPage(next);
}

function renderExplorer() {
  const title = document.getElementById('explorer-title');
  if (title) {
    const qualified = ex.schema === 'public' ? ex.table : `${ex.schema}.${ex.table}`;
    title.textContent = ex.database === 'postgres' ? qualified : `${ex.database} › ${qualified}`;
  }

  const editable = ex.pk.length > 0;
  const page = Math.floor(ex.offset / LIMIT) + 1;
  const pages = Math.max(1, Math.ceil(ex.total / LIMIT));

  const status = document.getElementById('explorer-status');
  if (status) {
    status.textContent = `${ex.total} fila${ex.total !== 1 ? 's' : ''} · página ${page} de ${pages}` +
      (editable ? '' : ' · solo lectura (sin PRIMARY KEY)');
  }
  const pageEl = document.getElementById('explorer-page');
  if (pageEl) pageEl.textContent = `${page} / ${pages}`;

  const th = ex.columns.map(c => {
    let badges = '';
    if (c.isPK) badges += '<span class="col-badge pk">PK</span>';
    if (c.identity) badges += '<span class="col-badge idn">ID</span>';
    if (c.notNull) badges += '<span class="col-badge nn">NN</span>';
    return `<th title="${escAttr(c.type)}">${escHtml(c.name)}${badges}</th>`;
  }).join('');

  const tb = ex.rows.map((row, ri) => {
    const tds = row.map((v, ci) => {
      const cls = v === null || v === undefined ? 'v-null' : (typeof v === 'number' ? 'v-num' : '');
      const shown = v === null || v === undefined ? 'NULL' : escHtml(String(v).length > 200 ? String(v).slice(0, 200) + '…' : String(v));
      const editAttr = editable && !ex.columns[ci]?.identity ? `ondblclick="explorerEditCell(${ri},${ci},this)"` : '';
      return `<td class="${cls}" data-raw="${escAttr(v === null || v === undefined ? '' : String(v))}" ${editAttr}>${shown}</td>`;
    }).join('');
    const del = editable
      ? `<td class="row-tools"><button class="row-del" onclick="explorerDeleteRow(${ri})" title="Borrar fila">×</button></td>`
      : '<td class="row-tools"></td>';
    return `<tr data-row="${ri}">${del}${tds}</tr>`;
  }).join('');

  const empty = ex.total === 0
    ? `<div class="empty-state" style="padding:40px 0"><p>La tabla está vacía</p></div>` : '';

  render(document.getElementById('explorer-grid'),
    `<table><thead><tr><th class="row-tools-h"></th>${th}</tr></thead><tbody>${tb}</tbody></table>${empty}`);

  const insBtn = document.getElementById('explorer-insert-btn');
  if (insBtn) insBtn.style.display = editable ? '' : 'none';
}

// ─── Edición inline ───────────────────────────────────────────────────────────
export function explorerEditCell(ri, ci, td) {
  if (!ex || td.querySelector('input')) return;
  const col = ex.columns[ci];
  const original = ex.rows[ri][ci];
  const input = document.createElement('input');
  input.className = 'cell-edit';
  input.value = original === null || original === undefined ? '' : String(original);
  td.replaceChildren(input);
  input.focus();
  input.select();

  let done = false;
  const finish = async (commit) => {
    if (done) return;
    done = true;
    const newVal = input.value;
    if (!commit || newVal === String(original ?? '')) {
      renderExplorer();
      return;
    }
    const sql = `UPDATE ${quoteIdent(ex.table)} SET ${quoteIdent(col.name)} = ${sqlLiteral(newVal, col.type)} WHERE ${pkWhere(ex.rows[ri])}`;
    const res = await runWrite(sql);
    if (res.error) {
      showToast(res.error, 'err');
      renderExplorer();
    } else {
      showToast(`${col.name} actualizado`);
      await loadExplorerPage(ex.offset);
      refreshSidebar();
    }
  };
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') finish(true);
    if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(false));
}

// ─── Borrar fila ──────────────────────────────────────────────────────────────
export function explorerDeleteRow(ri) {
  if (!ex) return;
  const row = ex.rows[ri];
  const preview = ex.columns.map((c, i) => `${c.name}: ${row[i] ?? 'NULL'}`).slice(0, 5).join(' · ');

  let host = document.getElementById('cell-modal-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'cell-modal-host';
    document.body.appendChild(host);
  }
  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:520px">
        <div class="modal-hdr"><span>Borrar fila de ${escHtml(ex.table)}</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal">
          <p style="margin-bottom:10px">¿Borrar esta fila? La acción no se puede deshacer.</p>
          <code style="font-size:11px;color:var(--dim)">${escHtml(preview)}</code>
        </div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Conservar</button>
          <button class="btn btn-cancel" style="display:inline-flex" onclick="explorerDeleteConfirm(${ri})">Borrar</button>
        </div>
      </div>
    </div>`);
}

export async function explorerDeleteConfirm(ri) {
  const { closeCellModal } = await import('./results.js');
  closeCellModal();
  if (!ex) return;
  const sql = `DELETE FROM ${quoteIdent(ex.table)} WHERE ${pkWhere(ex.rows[ri])}`;
  const res = await runWrite(sql);
  if (res.error) {
    showToast(res.error, 'err');
  } else {
    showToast('Fila borrada');
    await loadExplorerPage(ex.offset);
    refreshSidebar();
  }
}

// ─── Insertar fila ────────────────────────────────────────────────────────────
export function explorerInsertOpen() {
  if (!ex) return;
  let host = document.getElementById('cell-modal-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'cell-modal-host';
    document.body.appendChild(host);
  }
  const fields = ex.columns.map((c, i) => {
    const req = c.notNull && !c.identity ? ' <span style="color:var(--accent)">*</span>' : '';
    if (c.identity) {
      return `<label class="ins-field"><span>${escHtml(c.name)} <span class="col-badge idn">ID</span></span>
        <input data-col="${i}" placeholder="(autogenerado)" disabled></label>`;
    }
    return `<label class="ins-field"><span>${escHtml(c.name)}${req} <em>${escHtml(c.type)}</em></span>
      <input data-col="${i}" placeholder="${c.notNull ? 'requerido' : 'NULL si vacío'}"></label>`;
  }).join('');

  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()" style="max-width:520px">
        <div class="modal-hdr"><span>Insertar en ${escHtml(ex.table)}</span>
          <button class="modal-close" onclick="closeCellModal()">×</button></div>
        <div class="modal-body" style="white-space:normal;display:flex;flex-direction:column;gap:10px">${fields}</div>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="closeCellModal()">Cancelar</button>
          <button class="btn btn-run" onclick="explorerInsertConfirm()">Insertar</button>
        </div>
      </div>
    </div>`);
  host.querySelector('input:not([disabled])')?.focus();
}

export async function explorerInsertConfirm() {
  const host = document.getElementById('cell-modal-host');
  if (!host || !ex) return;
  const cols = [], vals = [];
  host.querySelectorAll('input[data-col]:not([disabled])').forEach(input => {
    const ci = +input.dataset.col;
    const col = ex.columns[ci];
    if (input.value === '' && !col.notNull) return; // NULL implícito
    cols.push(quoteIdent(col.name));
    vals.push(sqlLiteral(input.value, col.type));
  });
  if (!cols.length) { showToast('Completá al menos un campo', 'err'); return; }

  const sql = `INSERT INTO ${quoteIdent(ex.table)} (${cols.join(', ')}) VALUES (${vals.join(', ')})`;
  const res = await runWrite(sql);
  if (res.error) {
    showToast(res.error, 'err');
  } else {
    const { closeCellModal } = await import('./results.js');
    closeCellModal();
    showToast('Fila insertada');
    await loadExplorerPage(ex.offset);
    refreshSidebar();
  }
}
