// ─── Editor SQL: CodeMirror, ejecución, validación e historial ────────────────
import { state } from './state.js';
import { render, escHtml, showToast } from './dom.js';
import { apiQuery, apiScript, apiValidate } from './api.js';
import { showResults, showLoading, showError, clearResults, showScriptResults } from './results.js';
import { buildHintTables } from './sidebar.js';
import { activateTab, refreshSidebar } from './app.js';
import { initTabs, addTab } from './tabs.js';

const HISTORY_KEY = 'focusdb.history.v1';

let history = loadHistory();
let validateTimer = null, errorMarks = [], stmtMark = null;
let scriptMarks = [];  // marcas de error de ejecución de script (separadas de las de sintaxis)
let runAbort = null;   // AbortController de la ejecución en curso

function loadHistory() {
  try {
    const raw = localStorage.getItem(HISTORY_KEY);
    if (!raw) return [];
    return JSON.parse(raw).map(h => ({ ...h, ts: new Date(h.ts) }));
  } catch (_) { return []; }
}

function persistHistory() {
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(
      history.map(h => ({ ...h, ts: h.ts.toISOString() }))
    ));
  } catch (_) {}
}

// ─── Init ─────────────────────────────────────────────────────────────────────
// Modo SQL de CodeMirror extendido con las keywords propias del motor que el
// diccionario vendoreado no conoce (ventanas y QUALIFY). Se registra como MIME
// propio para que el editor inicial y cada CodeMirror.Doc de las pestañas
// (tabs.js) compartan exactamente el mismo modo.
export const SQL_MIME = 'text/x-focusdb';
const EXTRA_KEYWORDS = ['qualify', 'over', 'partition', 'row_number', 'rank', 'dense_rank'];
(function defineSqlMode() {
  const base = CodeMirror.resolveMode('text/x-sql') || {};
  const keywords = { ...(base.keywords || {}) };
  EXTRA_KEYWORDS.forEach(k => { keywords[k] = true; });
  CodeMirror.defineMIME(SQL_MIME, { ...base, name: 'sql', keywords });
})();

export function initEditor() {
  state.editor = CodeMirror.fromTextArea(document.getElementById('sql-input'), {
    mode: SQL_MIME,
    theme: 'dracula',
    lineNumbers: true,
    matchBrackets: true,
    autoCloseBrackets: true,
    styleActiveLine: true,
    indentWithTabs: false,
    tabSize: 2,
    extraKeys: {
      'F5':               runQuery,
      'Ctrl-Enter':       runQuery,
      'Cmd-Enter':        runQuery,
      'Ctrl-Shift-Enter': runAll,
      'Cmd-Shift-Enter':  runAll,
      'Ctrl-T':           () => addTab(),
      'Ctrl-Space':       smartHint,
    },
  });

  setEditorHeight();
  window.addEventListener('resize', setEditorHeight);

  // Validate + update statement highlight on change/cursor move
  state.editor.on('change', () => {
    clearTimeout(validateTimer);
    validateTimer = setTimeout(validateSQL, 600);
    clearScriptMarks();
    updateStmtHighlight();
  });
  state.editor.on('cursorActivity', updateStmtHighlight);

  initTabs();
  renderHistory();
}

// ─── Autocompletado: tablas + columnas + alias del statement + snippets ───────
const SNIPPETS = [
  { text: 'SELECT * FROM ',                          displayText: 'SELECT * FROM …' },
  { text: 'SELECT COUNT(*) FROM ',                   displayText: 'SELECT COUNT(*) FROM …' },
  { text: 'INSERT INTO  () VALUES ()',               displayText: 'INSERT INTO … VALUES …' },
  { text: 'UPDATE  SET  =  WHERE ',                  displayText: 'UPDATE … SET … WHERE …' },
  { text: 'DELETE FROM  WHERE ',                     displayText: 'DELETE FROM … WHERE …' },
  { text: 'CREATE TABLE  (id INTEGER IDENTITY PRIMARY KEY)', displayText: 'CREATE TABLE …' },
  { text: 'INNER JOIN  ON ',                         displayText: 'INNER JOIN … ON …' },
  { text: 'LEFT JOIN  ON ',                          displayText: 'LEFT JOIN … ON …' },
  { text: 'GROUP BY ',                               displayText: 'GROUP BY …' },
  { text: 'ORDER BY  DESC',                          displayText: 'ORDER BY … DESC' },
  { text: 'ROW_NUMBER() OVER (PARTITION BY  ORDER BY  DESC) AS rn', displayText: 'ROW_NUMBER() OVER (…) AS rn' },
  { text: 'QUALIFY ',                                displayText: 'QUALIFY …' },
];

// Resuelve alias del statement bajo el cursor: FROM t AS x / JOIN t x → {x: cols de t}
function aliasTables(base) {
  const { sql } = getCurrentStatement();
  const out = {};
  const re = /(?:FROM|JOIN)\s+([A-Za-z_][\w]*)(?:\s+(?:AS\s+)?([A-Za-z_][\w]*))?/gi;
  const KEYWORDS = new Set(['ON','WHERE','INNER','LEFT','RIGHT','FULL','CROSS','NATURAL','JOIN','GROUP','ORDER','LIMIT','USING','AS','SET','QUALIFY','OVER','PARTITION']);
  let m;
  while ((m = re.exec(sql)) !== null) {
    const table = m[1], alias = m[2];
    if (alias && !KEYWORDS.has(alias.toUpperCase()) && base[table]) out[alias] = base[table];
  }
  return out;
}

function smartHint(cm) {
  const base = buildHintTables();
  const tables = { ...base, ...aliasTables(base) };
  CodeMirror.showHint(cm, cmInstance => {
    const sqlHint = CodeMirror.hint.sql(cmInstance, { tables, completeSingle: false });
    const cur = cmInstance.getCursor();
    const token = cmInstance.getTokenAt(cur);
    const word = (token.string || '').trim().toLowerCase();
    // Snippets solo al inicio de statement (token vacío o palabra corta al comienzo de línea)
    const lineStart = cmInstance.getLine(cur.line).slice(0, token.start).trim() === '';
    if (lineStart && word.length >= 0) {
      const snip = SNIPPETS.filter(s => !word || s.text.toLowerCase().startsWith(word));
      if (snip.length) {
        const from = word ? CodeMirror.Pos(cur.line, token.start) : cur;
        const list = [...(sqlHint?.list || []), ...snip];
        return { list, from: sqlHint?.from || from, to: sqlHint?.to || cur };
      }
    }
    return sqlHint || { list: [], from: cur, to: cur };
  }, { completeSingle: false });
}

function setEditorHeight() {
  const total  = document.getElementById('main').clientHeight;
  const toolbar = document.getElementById('toolbar').offsetHeight;
  const strip  = document.getElementById('tab-strip')?.offsetHeight || 0;
  const vbar   = document.getElementById('validate-bar').offsetHeight;
  const resizer = document.getElementById('resizer').offsetHeight;
  const tabs   = document.querySelector('.tabs-bar').offsetHeight;
  const foot   = document.getElementById('results-footer').offsetHeight;
  const editorH = Math.floor((total - toolbar - strip - vbar - resizer - tabs - foot) * 0.45);
  document.getElementById('editor-pane').style.height = editorH + 'px';
  state.editor.refresh();
}

// ─── Resizer drag ─────────────────────────────────────────────────────────────
export function initResizer() {
  const resizer = document.getElementById('resizer');
  const edPane  = document.getElementById('editor-pane');
  let startY, startH;

  resizer.addEventListener('mousedown', e => {
    startY = e.clientY;
    startH = edPane.offsetHeight;
    resizer.classList.add('dragging');
    document.addEventListener('mousemove', onDrag);
    document.addEventListener('mouseup', onDrop);
    e.preventDefault();
  });

  function onDrag(e) {
    const dy = e.clientY - startY;
    const newH = Math.max(80, Math.min(startH + dy, window.innerHeight - 200));
    edPane.style.height = newH + 'px';
    state.editor.refresh();
  }
  function onDrop() {
    resizer.classList.remove('dragging');
    document.removeEventListener('mousemove', onDrag);
    document.removeEventListener('mouseup', onDrop);
  }
}

// ─── Touch support for resizer ────────────────────────────────────────────────
export function initResizerTouch() {
  const resizer = document.getElementById('resizer');
  const edPane  = document.getElementById('editor-pane');
  let startY, startH;

  resizer.addEventListener('touchstart', e => {
    startY = e.touches[0].clientY;
    startH = edPane.offsetHeight;
    resizer.classList.add('dragging');
    e.preventDefault();
  }, { passive: false });

  resizer.addEventListener('touchmove', e => {
    const dy   = e.touches[0].clientY - startY;
    const newH = Math.max(80, Math.min(startH + dy, window.innerHeight - 200));
    edPane.style.height = newH + 'px';
    state.editor.refresh();
    e.preventDefault();
  }, { passive: false });

  resizer.addEventListener('touchend', () => {
    resizer.classList.remove('dragging');
  });
}

// ─── Statement detection ──────────────────────────────────────────────────────

// Returns {sql, from, to} of the statement the cursor is inside.
// Splits on semicolons, respecting single-quoted strings and line comments.
export function getCurrentStatement() {
  const editor = state.editor;
  const content = editor.getValue();
  const cursor  = editor.getCursor();

  // Convert cursor (line, ch) to absolute char offset
  let cursorPos = 0;
  for (let i = 0; i < cursor.line; i++) cursorPos += editor.getLine(i).length + 1;
  cursorPos += cursor.ch;

  // Tokenise: find semicolon boundaries outside strings/comments
  const boundaries = [0]; // statement start offsets
  let inString = false, inLineComment = false;

  for (let i = 0; i < content.length; i++) {
    const ch = content[i];

    if (inLineComment) {
      if (ch === '\n') inLineComment = false;
      continue;
    }
    if (inString) {
      if (ch === "'" && content[i+1] === "'") { i++; continue; } // escaped quote
      if (ch === "'") inString = false;
      continue;
    }
    if (ch === "'" ) { inString = true; continue; }
    if (ch === '-' && content[i+1] === '-') { inLineComment = true; continue; }

    if (ch === ';') boundaries.push(i + 1); // next statement starts after ;
  }
  boundaries.push(content.length);

  // Find which segment contains cursorPos
  for (let i = 0; i < boundaries.length - 1; i++) {
    const from = boundaries[i];
    const to   = boundaries[i + 1];
    if (cursorPos >= from && cursorPos <= to) {
      const sql = content.slice(from, to).replace(/;\s*$/, '').trim();
      if (!sql) continue;
      return { sql, from, to: to - 1 }; // to-1 skips the semicolon itself
    }
  }
  return { sql: content.trim(), from: 0, to: content.length };
}

// Convert absolute char offset to {line, ch}
function offsetToPos(offset) {
  const lines = state.editor.getValue().split('\n');
  let remaining = offset;
  for (let i = 0; i < lines.length; i++) {
    if (remaining <= lines[i].length) return { line: i, ch: remaining };
    remaining -= lines[i].length + 1;
  }
  return { line: lines.length - 1, ch: lines[lines.length-1].length };
}

// Highlight the statement that will run (blue tint) when nothing is selected
function updateStmtHighlight() {
  if (stmtMark) { stmtMark.clear(); stmtMark = null; }

  const sel = state.editor.getSelection();
  const modeEl = document.getElementById('run-mode');

  if (sel.trim()) {
    modeEl.textContent = '▸ selección';
    return;
  }

  const { sql, from, to } = getCurrentStatement();
  if (!sql) { modeEl.textContent = ''; return; }

  modeEl.textContent = '▸ statement actual';
  stmtMark = state.editor.markText(
    offsetToPos(from),
    offsetToPos(to),
    { className: 'cm-stmt-highlight' }
  );
}

// Decide what SQL to run: selection > current statement
function getQueryToRun() {
  const sel = state.editor.getSelection().trim();
  if (sel) return { sql: sel, mode: 'selection' };
  const { sql } = getCurrentStatement();
  return { sql, mode: 'statement' };
}

// ─── Run query ────────────────────────────────────────────────────────────────
function beginRun(btnId) {
  const statusEl = document.getElementById('run-status');
  statusEl.className = '';
  statusEl.textContent = 'Ejecutando…';
  document.getElementById(btnId).disabled = true;
  const cancelBtn = document.getElementById('btn-cancel');
  if (cancelBtn) cancelBtn.style.display = '';
  clearScriptMarks();
  runAbort = new AbortController();
  showLoading();
  return runAbort.signal;
}

function endRun(btnId) {
  document.getElementById(btnId).disabled = false;
  document.getElementById('run-status').textContent = '';
  const cancelBtn = document.getElementById('btn-cancel');
  if (cancelBtn) cancelBtn.style.display = 'none';
  runAbort = null;
}

// Cancela la ejecución en curso (aborta el fetch → el backend cancela el contexto).
export function cancelRun() {
  if (runAbort) {
    runAbort.abort();
    showToast('Consulta cancelada');
  }
}

export async function runQuery() {
  const { sql } = getQueryToRun();
  if (!sql) return;

  const signal = beginRun('btn-run');
  const t0 = Date.now();
  try {
    const data = await apiQuery(sql, { signal });

    if (data.error) {
      showError(data.error, data.elapsed_ms ?? (Date.now()-t0));
      addHistory(sql, false, data.error, data.elapsed_ms ?? 0);
      showToast(data.error, 'err');
    } else {
      showResults(data.columns || [], data.rows || [], data.tag || '', data.elapsed_ms ?? (Date.now()-t0), { truncated: data.truncated, sourceSql: sql });
      addHistory(sql, true, data.tag, data.elapsed_ms ?? 0);
      if (data.tag) showToast(data.tag);
      refreshSidebar();
    }
  } catch(e) {
    if (e.name === 'AbortError') {
      showError('Consulta cancelada por el usuario', Date.now()-t0);
    } else {
      showError('Error de red: ' + e.message, Date.now()-t0);
      showToast('Sin conexión con el servidor', 'err');
    }
  } finally {
    endRun('btn-run');
  }
}

// Ejecuta todo el script vía /api/script: un resultado por statement,
// se detiene en el primer error y lo señala en el editor.
export async function runAll() {
  const sql = state.editor.getValue().trim();
  if (!sql) return;

  const signal = beginRun('btn-runall');
  const t0 = Date.now();
  try {
    const data = await apiScript(sql, { signal });
    const n = (data.results || []).length;

    showScriptResults(data);

    if (data.failedIndex >= 0) {
      addHistory(sql, false, data.error || 'error', Date.now()-t0);
      showToast(`Falló el statement ${data.failedIndex + 1}: ${data.error || ''}`, 'err');
      if (data.failedSql) markScriptError(data.failedSql, data.error || '');
    } else {
      addHistory(sql, true, `${n} statement${n !== 1 ? 's' : ''}`, Date.now()-t0);
      showToast(`Script completo: ${n} statement${n !== 1 ? 's' : ''}`);
      refreshSidebar();
    }
    if (n > 0) refreshSidebar();
  } catch(e) {
    if (e.name === 'AbortError') {
      showError('Script cancelado por el usuario', Date.now()-t0);
    } else {
      showError('Error de red: ' + e.message, Date.now()-t0);
      showToast('Sin conexión con el servidor', 'err');
    }
  } finally {
    endRun('btn-runall');
  }
}

// Subraya el statement fallido localizándolo textualmente en el editor.
// Usa una lista de marcas propia: la validación de sintaxis (asíncrona) no debe
// borrar este subrayado semántico; se limpia al editar o al re-ejecutar.
function markScriptError(failedSql, errMsg) {
  const content = state.editor.getValue();
  const at = content.indexOf(failedSql.trim());
  if (at === -1) { markError(errMsg); return; }
  const mark = state.editor.markText(
    offsetToPos(at),
    offsetToPos(at + failedSql.trim().length),
    { className: 'cm-syntax-error', title: errMsg }
  );
  scriptMarks.push(mark);
  state.editor.scrollIntoView(offsetToPos(at), 80);
}

function clearScriptMarks() {
  scriptMarks.forEach(m => m.clear());
  scriptMarks = [];
}

export function clearEditor() {
  state.editor.setValue('');
  state.editor.focus();
  if (stmtMark) { stmtMark.clear(); stmtMark = null; }
  document.getElementById('run-mode').textContent = '';
  clearResults();
  document.getElementById('run-status').textContent = '';
  document.getElementById('run-status').className = '';
  setValidateBar('idle', 'En reposo');
}

// ─── Syntax validation ────────────────────────────────────────────────────────
export function setValidateBar(vstate, msg) {
  const bar = document.getElementById('validate-bar');
  bar.className = vstate;
  bar.textContent = msg;
}

export async function validateSQL() {
  const sql = state.editor.getValue().trim();
  clearErrorMarks();
  if (!sql || sql.startsWith('--')) { setValidateBar('idle', 'En reposo'); return; }

  try {
    const data = await apiValidate(sql);
    if (data.valid) {
      setValidateBar('ok', '✓ Sintaxis válida');
    } else {
      setValidateBar('err', '✕ ' + (data.error || 'Error de sintaxis'));
      markError(data.error);
    }
  } catch(_) {
    setValidateBar('idle', 'En reposo');
  }
}

function markError(errMsg) {
  // Try to extract line/col info from "line X col Y" patterns
  const lineMatch = String(errMsg).match(/line\s+(\d+)/i);
  const colMatch  = String(errMsg).match(/col(?:umn)?\s+(\d+)/i);
  if (lineMatch) {
    const line = parseInt(lineMatch[1], 10) - 1;
    const col  = colMatch ? parseInt(colMatch[1], 10) - 1 : 0;
    const mark = state.editor.markText(
      {line, ch: col},
      {line, ch: col + 1},
      {className: 'cm-syntax-error', title: errMsg}
    );
    errorMarks.push(mark);
  }
}

function clearErrorMarks() {
  errorMarks.forEach(m => m.clear());
  errorMarks = [];
}

// ─── History (persistente) ────────────────────────────────────────────────────
function addHistory(sql, ok, tagOrErr, ms) {
  // dedup consecutivo: mismo SQL seguido no se apila
  if (history.length && history[0].sql === sql && history[0].ok === ok) {
    history[0] = {sql, ok, tagOrErr, ms, ts: new Date()};
  } else {
    history.unshift({sql, ok, tagOrErr, ms, ts: new Date()});
    if (history.length > 100) history.pop();
  }
  persistHistory();
  renderHistory();
}

function renderHistory() {
  const wrap = document.getElementById('history-wrap');
  if (!history.length) {
    render(wrap, '<div class="empty-state"><p>Aún no hay consultas</p></div>');
    return;
  }
  render(wrap, history.map((h, i) => {
    const timeStr = h.ts.toLocaleTimeString();
    const tag = h.ok
      ? `<span class="ok">✓ ${escHtml(h.tagOrErr)}</span>`
      : `<span class="err">✕ error</span>`;
    return `
      <div class="h-item" onclick="restoreHistory(${i})">
        <div class="h-sql">${escHtml(h.sql)}</div>
        <div class="h-meta">${tag}<span>${h.ms}ms</span><span>${timeStr}</span></div>
      </div>`;
  }).join(''));
}

export function restoreHistory(i) {
  state.editor.setValue(history[i].sql);
  activateTab('results');
  state.editor.focus();
}
