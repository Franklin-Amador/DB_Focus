// ─── Panel de resultados ──────────────────────────────────────────────────────
// Pipeline de vista: lastResult (fuente) → filtro rápido → sort → render
// paginado (bloques de PAGE filas, append sin re-render de lo ya pintado).
import { render, escHtml, escAttr, showToast, downloadBlob } from './dom.js';
import { activateTab } from './app.js';

const PAGE = 500;

let lastResult = null;  // {cols, rows, tag, ms, sortIdx, sortDir, truncated, sourceSql}
let filterText = '';
let filterTimer = null;
let paintedCount = 0;   // filas ya pintadas de la vista actual
let currentView = [];   // filas tras filtro+sort

export function showResults(cols, rows, tag, ms, opts = {}) {
  activateTab('results');
  lastResult = {
    cols, rows, tag, ms,
    sortIdx: -1, sortDir: 0,
    truncated: !!opts.truncated,
    sourceSql: opts.sourceSql || '',
  };
  filterText = '';
  const f = document.getElementById('results-filter');
  if (f) f.value = '';
  paintResults();
}

// Estado de carga orgánico mientras corre la consulta
export function showLoading() {
  activateTab('results');
  toggleResultsToolbar(false);
  render(document.getElementById('results-wrap'),
    `<div class="loading-garden">
       <svg class="grow" viewBox="0 0 32 40" fill="none" stroke="currentColor"
            stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
         <path d="M16 38V15"/>
         <path d="M16 25C9 23 6 18 6 12 13 13 16 18 16 24Z"/>
         <path d="M16 21C23 19 26 14 26 9 19 10 16 15 16 20Z"/>
       </svg>
       <p>Creciendo…</p>
     </div>`);
  document.getElementById('results-footer').replaceChildren();
}

// Vacía el panel al estado inicial "jardín en calma".
export function clearResults() {
  lastResult = null;
  toggleResultsToolbar(false);
  render(document.getElementById('results-wrap'),
    `<div class="empty-state">
       <svg class="sprig" viewBox="0 0 64 64" fill="none" stroke="#7bb661" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
         <path d="M32 61 C32 46 32 32 32 15"/>
         <path d="M32 45 C22 43 16 37 15 28 C24 28 30 33 32 41Z" fill="rgba(123,182,97,.20)"/>
         <path d="M32 37 C42 35 48 29 49 20 C40 20 34 25 32 33Z" fill="rgba(224,164,88,.18)" stroke="#e0a458"/>
         <path d="M32 25 C24 23 19 18 18 10 C26 10 31 15 32 22Z" fill="rgba(123,182,97,.20)"/>
       </svg>
       <h3>El jardín está en calma</h3>
       <p>Ejecutá una consulta para ver brotar los resultados</p>
     </div>`);
  document.getElementById('results-footer').replaceChildren();
}

function toggleResultsToolbar(show) {
  const tb = document.getElementById('results-toolbar');
  if (tb) tb.style.display = show ? '' : 'none';
}

// Comparador de celdas: numérico si ambos parsean como número (sanitizeRows
// convierte int64 a string), si no lexicográfico; NULL al final.
function cmpCell(a, b) {
  const an = a === null || a === undefined, bn = b === null || b === undefined;
  if (an && bn) return 0;
  if (an) return 1;
  if (bn) return -1;
  const na = typeof a === 'number' ? a : parseFloat(a);
  const nb = typeof b === 'number' ? b : parseFloat(b);
  if (!Number.isNaN(na) && !Number.isNaN(nb) && String(na) === String(a).trim() && String(nb) === String(b).trim()) {
    return na - nb;
  }
  return String(a).localeCompare(String(b), undefined, { numeric: true });
}

export function sortResults(idx) {
  if (!lastResult) return;
  if (lastResult.sortIdx !== idx)      { lastResult.sortIdx = idx; lastResult.sortDir = 1; }
  else if (lastResult.sortDir === 1)   { lastResult.sortDir = -1; }
  else                                 { lastResult.sortIdx = -1; lastResult.sortDir = 0; }
  paintResults();
}

// Filtro rápido client-side (debounced): substring sobre todas las celdas.
export function filterResults(v) {
  clearTimeout(filterTimer);
  filterTimer = setTimeout(() => {
    filterText = (v || '').toLowerCase();
    paintResults();
  }, 150);
}

function computeView() {
  const { rows, sortIdx, sortDir } = lastResult;
  let view = rows;
  if (filterText) {
    view = rows.filter(row => (row || []).some(c =>
      c !== null && c !== undefined && String(c).toLowerCase().includes(filterText)));
  }
  if (sortIdx >= 0) {
    view = view.slice().sort((a, b) => cmpCell((a||[])[sortIdx], (b||[])[sortIdx]) * sortDir);
  }
  return view;
}

function cellTD(v) {
  if (v === null || v === undefined) return `<td class="v-null" data-raw="">NULL</td>`;
  if (typeof v === 'number')  return `<td class="v-num" data-raw="${escAttr(String(v))}">${v}</td>`;
  if (typeof v === 'boolean') return `<td class="v-bool" data-raw="${v}">${v}</td>`;
  const s = String(v);
  const shown = s.length > 300 ? s.slice(0, 300) + '…' : s;
  return `<td data-raw="${escAttr(s)}">${escHtml(shown)}</td>`;
}

function rowsHTML(view, from, to) {
  let html = '';
  for (let i = from; i < to; i++) {
    const row = view[i] || [];
    html += `<tr data-row="${i}"><td class="row-tools"><button class="row-insert" onclick="copyRowInsert(${i})" title="Copiar como INSERT">+</button></td>${row.map(cellTD).join('')}</tr>`;
  }
  return html;
}

function footerHTML(view) {
  const { tag, ms, rows, truncated } = lastResult;
  const shown = Math.min(paintedCount, view.length);
  let parts = `<span class="ok">${escHtml(tag)}</span>`;
  if (filterText) parts += `<span>${shown} de ${view.length} (filtradas de ${rows.length})</span>`;
  else parts += `<span>${shown} de ${view.length} fila${view.length!==1?'s':''}</span>`;
  parts += `<span>${ms}ms</span>`;
  if (truncated) parts += `<span class="trunc-badge">resultado truncado</span>`;
  return parts;
}

export function paintResults() {
  const wrap   = document.getElementById('results-wrap');
  const footer = document.getElementById('results-footer');
  const { cols, rows, tag, ms } = lastResult;

  if (!cols.length && !rows.length) {
    toggleResultsToolbar(false);
    render(wrap, `<div class="empty-state">
        <svg class="sprig" viewBox="0 0 64 64" fill="none" stroke="#7bb661" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M32 61 C32 46 32 32 32 15"/>
          <path d="M32 45 C22 43 16 37 15 28 C24 28 30 33 32 41Z" fill="rgba(123,182,97,.20)"/>
          <path d="M32 37 C42 35 48 29 49 20 C40 20 34 25 32 33Z" fill="rgba(224,164,88,.18)" stroke="#e0a458"/>
        </svg>
        <h3>${escHtml(tag)}</h3>
        <p>La operación se completó en calma</p>
      </div>`);
    render(footer, `<span class="ok">${escHtml(tag)}</span><span>${ms}ms</span>`);
    setStatus(tag, false);
    return;
  }

  toggleResultsToolbar(true);
  const view = computeView();
  currentView = view;
  paintedCount = Math.min(PAGE, view.length);

  const { sortIdx, sortDir } = lastResult;
  const th = cols.map((c, i) => {
    const cls = 'sortable' + (i === sortIdx ? (sortDir > 0 ? ' sort-asc' : ' sort-desc') : '');
    return `<th class="${cls}" data-idx="${i}" title="Ordenar por ${escAttr(c)}">${escHtml(c)}</th>`;
  }).join('');

  const more = view.length > paintedCount
    ? `<div class="load-more"><button class="btn btn-clear" onclick="loadMoreRows()">Cargar ${Math.min(PAGE, view.length - paintedCount)} más (${view.length - paintedCount} restantes)</button></div>`
    : '';

  render(wrap, `<table><thead><tr><th class="row-tools-h"></th>${th}</tr></thead><tbody>${rowsHTML(view, 0, paintedCount)}</tbody></table>${more}`);
  wireTableEvents(wrap);
  render(footer, footerHTML(view));
  setStatus(tag, false);
}

function wireTableEvents(wrap) {
  wrap.querySelectorAll('thead th.sortable').forEach(h =>
    h.addEventListener('click', () => sortResults(+h.dataset.idx)));
  const tbody = wrap.querySelector('tbody');
  if (tbody) {
    tbody.addEventListener('click', onCellClick);
    tbody.addEventListener('dblclick', onCellDblClick);
  }
}

// Append del siguiente bloque de filas, sin re-render de lo ya pintado.
export function loadMoreRows() {
  const wrap  = document.getElementById('results-wrap');
  const tbody = wrap.querySelector('tbody');
  if (!tbody || !lastResult) return;
  const next = Math.min(paintedCount + PAGE, currentView.length);
  const doc = new DOMParser().parseFromString(
    `<table><tbody>${rowsHTML(currentView, paintedCount, next)}</tbody></table>`, 'text/html');
  Array.from(doc.querySelector('tbody').children).forEach(tr => tbody.appendChild(tr));
  paintedCount = next;

  const moreWrap = wrap.querySelector('.load-more');
  if (moreWrap) {
    if (paintedCount >= currentView.length) moreWrap.remove();
    else render(moreWrap, `<button class="btn btn-clear" onclick="loadMoreRows()">Cargar ${Math.min(PAGE, currentView.length - paintedCount)} más (${currentView.length - paintedCount} restantes)</button>`);
  }
  render(document.getElementById('results-footer'), footerHTML(currentView));
}

// ─── Interacción con celdas ───────────────────────────────────────────────────
function onCellClick(e) {
  const td = e.target.closest('td');
  if (!td || td.classList.contains('row-tools') || !navigator.clipboard) return;
  navigator.clipboard.writeText(td.dataset.raw ?? td.textContent)
    .then(() => showToast('Celda copiada'))
    .catch(() => {});
}

function onCellDblClick(e) {
  const td = e.target.closest('td');
  if (!td || td.classList.contains('row-tools')) return;
  openCellModal(td.dataset.raw ?? td.textContent);
}

// Modal de celda expandida (pretty-print si es JSON válido).
function openCellModal(raw) {
  let pretty = raw, isJSON = false;
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed === 'object' && parsed !== null) {
      pretty = JSON.stringify(parsed, null, 2);
      isJSON = true;
    }
  } catch (_) {}

  let host = document.getElementById('cell-modal-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'cell-modal-host';
    document.body.appendChild(host);
  }
  render(host, `
    <div class="modal-backdrop" onclick="closeCellModal()">
      <div class="modal" onclick="event.stopPropagation()">
        <div class="modal-hdr">
          <span>Celda${isJSON ? ' · JSON' : ''} · ${raw.length} caracteres</span>
          <button class="modal-close" onclick="closeCellModal()">×</button>
        </div>
        <pre class="modal-body">${escHtml(pretty)}</pre>
        <div class="modal-foot">
          <button class="btn btn-clear" onclick="copyCellModal()">Copiar</button>
        </div>
      </div>
    </div>`);
  host.dataset.raw = raw;
}

export function closeCellModal() {
  const host = document.getElementById('cell-modal-host');
  if (host) host.replaceChildren();
}

export function copyCellModal() {
  const host = document.getElementById('cell-modal-host');
  if (host && navigator.clipboard) {
    navigator.clipboard.writeText(host.dataset.raw || '')
      .then(() => showToast('Celda copiada'));
  }
}

// ─── Copiar fila como INSERT ──────────────────────────────────────────────────
function sqlLit(v) {
  if (v === null || v === undefined) return 'NULL';
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  const s = String(v);
  if (/^-?\d+(\.\d+)?$/.test(s)) return s;  // numérico serializado como string
  return `'${s.replace(/'/g, "''")}'`;
}

export function copyRowInsert(viewIdx) {
  if (!lastResult || !navigator.clipboard) return;
  const row = currentView[viewIdx] || [];
  const m = (lastResult.sourceSql || '').match(/\bFROM\s+([A-Za-z_][\w]*)/i);
  const table = m ? m[1] : '<tabla>';
  const sql = `INSERT INTO ${table} (${lastResult.cols.join(', ')}) VALUES (${row.map(sqlLit).join(', ')});`;
  navigator.clipboard.writeText(sql).then(() => showToast('INSERT copiado'));
}

// ─── Export CSV / JSON (conjunto filtrado + ordenado actual) ──────────────────
function csvField(v) {
  if (v === null || v === undefined) return '';
  const s = String(v);
  if (/[",\r\n]/.test(s)) return '"' + s.replace(/"/g, '""') + '"';
  return s;
}

export function exportResultsCSV() {
  if (!lastResult) return;
  const view = computeView();
  const lines = [lastResult.cols.map(csvField).join(',')];
  for (const row of view) lines.push((row || []).map(csvField).join(','));
  downloadBlob(new Blob(['﻿' + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' }), 'focusdb-results.csv');
  showToast(`CSV exportado (${view.length} filas)`);
}

export function exportResultsJSON() {
  if (!lastResult) return;
  const view = computeView();
  const objs = view.map(row => {
    const o = {};
    lastResult.cols.forEach((c, i) => { o[c] = (row || [])[i] ?? null; });
    return o;
  });
  downloadBlob(new Blob([JSON.stringify(objs, null, 2)], { type: 'application/json' }), 'focusdb-results.json');
  showToast(`JSON exportado (${view.length} filas)`);
}

// ─── Resultados de script (un acordeón por statement) ─────────────────────────
export function showScriptResults(data) {
  activateTab('results');
  toggleResultsToolbar(false);
  lastResult = null;
  const wrap   = document.getElementById('results-wrap');
  const footer = document.getElementById('results-footer');
  const results = data.results || [];

  let html = '<div class="script-results">';
  for (const r of results) {
    const hasRows = (r.columns || []).length > 0;
    const meta = `${escHtml(r.tag || '')} · ${r.elapsedMs}ms${r.truncated ? ' · truncado' : ''}`;
    let body = '';
    if (hasRows) {
      const th = r.columns.map(c => `<th>${escHtml(c)}</th>`).join('');
      const shown = (r.rows || []).slice(0, 200);
      const tb = shown.map(row => `<tr>${(row||[]).map(cellTD).join('')}</tr>`).join('');
      const extra = (r.rows || []).length > 200 ? `<div class="script-more">… ${(r.rows.length - 200)} filas más (ejecutá el statement solo para verlas todas)</div>` : '';
      body = `<div class="script-grid"><table><thead><tr>${th}</tr></thead><tbody>${tb}</tbody></table>${extra}</div>`;
    }
    html += `
      <details class="script-stmt ok-stmt" ${hasRows ? 'open' : ''}>
        <summary><span class="stmt-badge ok">✓ ${r.index + 1}</span><code>${escHtml(truncSQL(r.sql))}</code><span class="stmt-meta">${meta}</span></summary>
        ${body}
      </details>`;
  }
  if (data.failedIndex >= 0) {
    html += `
      <details class="script-stmt err-stmt" open>
        <summary><span class="stmt-badge err">✕ ${data.failedIndex + 1}</span><code>${escHtml(truncSQL(data.failedSql || ''))}</code><span class="stmt-meta">falló</span></summary>
        <div class="script-error">${escHtml(data.error || 'error')}</div>
      </details>`;
  }
  html += '</div>';

  render(wrap, html);
  const okN = results.length;
  const failed = data.failedIndex >= 0;
  render(footer, failed
    ? `<span class="ok">${okN} ok</span><span class="err">falló el statement ${data.failedIndex + 1}</span>`
    : `<span class="ok">${okN} statement${okN!==1?'s':''} ejecutados</span>`);
  setStatus(failed ? 'ERROR' : `${okN} statements`, failed);
}

function truncSQL(s) {
  const one = s.replace(/\s+/g, ' ').trim();
  return one.length > 80 ? one.slice(0, 80) + '…' : one;
}

export function showError(msg, ms) {
  activateTab('results');
  toggleResultsToolbar(false);
  render(document.getElementById('results-wrap'),
    `<div class="empty-state">
       <svg class="sprig" viewBox="0 0 64 64" fill="none" stroke="var(--red)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="opacity:.85">
         <path d="M32 60 C32 47 32 34 32 20"/>
         <path d="M32 44 C24 44 18 39 16 31 C24 30 30 35 32 42Z" fill="rgba(224,122,95,.16)"/>
         <path d="M32 36 C40 34 45 28 46 21 C39 22 34 27 32 34Z" fill="rgba(224,122,95,.12)"/>
         <path d="M23 12 L41 30 M41 12 L23 30" stroke-width="2.4"/>
       </svg>
       <h3 style="color:var(--red)">La consulta no prosperó</h3>
       <p style="max-width:480px;text-align:center;word-break:break-word;color:var(--dim)">${escHtml(msg)}</p>
     </div>`);
  render(document.getElementById('results-footer'),
    `<span class="err">ERROR</span><span>${ms}ms</span>`);
  setStatus('ERROR', true);
}

export function setStatus(msg, isErr) {
  const el = document.getElementById('run-status');
  el.className = isErr ? 'err' : 'ok';
  el.textContent = isErr ? '✕ Error' : '✓ ' + msg;
}

// Cerrar modal con Escape
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeCellModal();
});
