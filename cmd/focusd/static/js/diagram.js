// ─── Diagrama ER ──────────────────────────────────────────────────────────────
// v2: posiciones/zoom persistentes por firma de schema, drag O(aristas incidentes)
// sin re-render, self-FK como bucle, minimapa, cardinalidad 1/N, rowCount y
// badges de índice, layout consciente del tamaño de las cajas, Pointer Events
// (mouse + touch + pinch).
import { state } from './state.js';
import { ICON_PATHS } from './icons.js';
import { downloadBlob, showToast } from './dom.js';
import { apiDiagram } from './api.js';
import { runQuery } from './editor.js';
import { showView } from './app.js';

// ─── Constantes ───────────────────────────────────────────────────────────────
const D = {
  W:       210,   // ancho de tabla (modo detallado)
  WC:      150,   // ancho de tabla (modo compacto)
  HDR:      38,
  ROW:      22,
  PAD:       8,
  BEND:    100,
};
const SCALE_MIN = 0.1, SCALE_MAX = 3;

let compactMode = false;
function W() { return compactMode ? D.WC : D.W; }
function tblH(nCols) { return D.HDR + nCols * D.ROW + D.PAD; }
function colCY(idx)  { return D.HDR + idx * D.ROW + D.ROW / 2; }

// Paleta del diagrama: hardcodeada a propósito (el SVG exportado debe ser
// autocontenido, sin depender de las CSS vars de la página).
const C = {
  bg: '#0f1a14', box: '#0b1410', boxStroke: '#24382b', hdr: '#132018',
  accent: '#7bb661', teal: '#6fb3a8', gold: '#e6c26a', text: '#e7e3d3',
  dim: '#7d8b78', sep: '#2c4735', grid: '#17281d', name: '#dfe8d5',
};

// ─── Estado ───────────────────────────────────────────────────────────────────
let diagData    = null;
let diagPos     = {};
let diagTx      = 0, diagTy = 0, diagScale = 1;
let selectedTable = null;
let layoutRunning = false;

// Índices para drag eficiente: qué grupos/aristas tocar sin re-render global.
let tableEls    = {};   // name -> <g class=tbl-group>
let fkEls       = [];   // fkIdx -> {g, paths[], labelG, cardA, cardB}
let edgesByTable = new Map(); // name -> Set(fkIdx)

// Punteros activos (pan/drag/pinch)
const pointers = new Map();
let dragMode = null;    // {kind:'pan'|'table'|'pinch', ...}

// ─── Persistencia (clave por firma del schema) ────────────────────────────────
function schemaSig(data) {
  const parts = (data.tables || []).map(t => t.name + ':' + t.columns.length).sort();
  let h = 5381;
  const s = parts.join('|');
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0;
  return h.toString(36);
}
// v2: la clave incluye el esquema activo (tablas homónimas en distintos esquemas
// no comparten posiciones).
function persistKey() { return 'focusdb.diagram.v2.' + state.schema + '.' + (diagData ? schemaSig(diagData) : '0'); }

let persistTimer = null;
function persistDiagram() {
  clearTimeout(persistTimer);
  persistTimer = setTimeout(() => {
    if (!diagData) return;
    try {
      localStorage.setItem(persistKey(), JSON.stringify({
        positions: diagPos, tx: diagTx, ty: diagTy, scale: diagScale, compact: compactMode,
      }));
    } catch (_) {}
  }, 400);
}

function loadPersisted(data) {
  try {
    const raw = localStorage.getItem('focusdb.diagram.v2.' + state.schema + '.' + schemaSig(data));
    if (!raw) return null;
    const st = JSON.parse(raw);
    if (!st || typeof st.positions !== 'object') return null;
    return st;
  } catch (_) { return null; }
}

// ─── Carga & render ───────────────────────────────────────────────────────────
export async function loadDiagram(force = false) {
  // Reutiliza datos y posiciones en memoria al reentrar a la vista.
  if (diagData && !force) {
    renderDiagram();
    updateStatus();
    return;
  }
  document.getElementById('diagram-status').textContent = 'Cargando…';
  try {
    const data = await apiDiagram();
    diagData = data;

    const saved = loadPersisted(data);
    if (saved && !force) {
      diagPos = saved.positions;
      diagTx = saved.tx; diagTy = saved.ty; diagScale = saved.scale;
      if (saved.compact !== compactMode) applyCompact(!!saved.compact);
      placeNewTables();
      renderDiagram();
      applyDiagramTransform();
    } else {
      await forceLayout(data);
      renderDiagram();
      fitDiagram();
    }
    updateStatus();
  } catch(e) {
    document.getElementById('diagram-status').textContent = 'Error: ' + e.message;
  }
}

function updateStatus() {
  if (!diagData) return;
  document.getElementById('diagram-status').textContent =
    `${diagData.tables.length} tabla${diagData.tables.length!==1?'s':''} · ${diagData.fks.length} relaci${diagData.fks.length!==1?'ones':'ón'}`;
}

// Tablas nuevas (sin posición guardada) se colocan cerca del centroide.
function placeNewTables() {
  const known = Object.keys(diagPos);
  let cx = 400, cy = 300;
  if (known.length) {
    cx = known.reduce((a, k) => a + diagPos[k].x, 0) / known.length;
    cy = known.reduce((a, k) => a + diagPos[k].y, 0) / known.length;
  }
  let i = 0;
  for (const t of diagData.tables) {
    if (!diagPos[t.name]) {
      diagPos[t.name] = { x: Math.round(cx + 260 + (i % 3) * 60), y: Math.round(cy + i * 90) };
      i++;
    }
  }
}

// ─── Force layout consciente del tamaño (AABB) ────────────────────────────────
// Repulsión entre rectángulos (no puntos): usa la distancia entre bordes, así
// las tablas altas no se solapan. Si N>25 trocea las iteraciones en rAF.
async function forceLayout(data) {
  const N = data.tables.length;
  if (N === 0 || layoutRunning) return;
  layoutRunning = true;

  const w = W();
  const nodes = data.tables.map((t, i) => {
    const angle = (2 * Math.PI * i) / N;
    const r = Math.max(260, N * 75);
    return {
      name: t.name, w, h: tblH(t.columns.length),
      x: 600 + r * Math.cos(angle), y: 400 + r * Math.sin(angle), vx: 0, vy: 0,
    };
  });

  const nodeIdx = {};
  nodes.forEach((n, i) => nodeIdx[n.name] = i);

  const edges = data.fks
    .map(fk => ({ src: nodeIdx[fk.fromTable], tgt: nodeIdx[fk.toTable] }))
    .filter(e => e.src !== undefined && e.tgt !== undefined && e.src !== e.tgt);

  const REPULSION = 22000, SPRING_LEN = 340, SPRING_K = 0.04, DAMP = 0.82, MARGIN = 40;

  const iterate = () => {
    // repulsión por pares usando distancia centro-a-centro compensada por tamaño
    for (let i = 0; i < N; i++) {
      for (let j = i + 1; j < N; j++) {
        const a = nodes[i], b = nodes[j];
        const acx = a.x + a.w/2, acy = a.y + a.h/2;
        const bcx = b.x + b.w/2, bcy = b.y + b.h/2;
        let dx = bcx - acx, dy = bcy - acy;
        let dist = Math.sqrt(dx * dx + dy * dy) || 1;
        // distancia efectiva entre bordes (resta la mitad de cada caja proyectada)
        const overlap = (a.w + b.w) / 2 + MARGIN - Math.abs(dx);
        const overlapY = (a.h + b.h) / 2 + MARGIN - Math.abs(dy);
        let f = REPULSION / (dist * dist);
        // empuje extra si los AABB se solapan
        if (overlap > 0 && overlapY > 0) f += 4;
        const fx = f * dx / dist, fy = f * dy / dist;
        a.vx -= fx; a.vy -= fy;
        b.vx += fx; b.vy += fy;
      }
    }
    for (const e of edges) {
      const a = nodes[e.src], b = nodes[e.tgt];
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const f = SPRING_K * (dist - SPRING_LEN);
      const fx = f * dx / dist, fy = f * dy / dist;
      a.vx += fx; a.vy += fy;
      b.vx -= fx; b.vy -= fy;
    }
    for (const n of nodes) {
      n.vx *= DAMP; n.vy *= DAMP;
      n.x  += n.vx; n.y  += n.vy;
    }
  };

  const TOTAL = 220;
  if (N > 25) {
    // trocear en frames para no congelar el hilo con esquemas grandes
    const CHUNK = 25;
    for (let done = 0; done < TOTAL; done += CHUNK) {
      for (let k = 0; k < CHUNK && done + k < TOTAL; k++) iterate();
      await new Promise(requestAnimationFrame);
    }
  } else {
    for (let iter = 0; iter < TOTAL; iter++) iterate();
  }

  diagPos = {};
  for (const n of nodes) diagPos[n.name] = { x: Math.round(n.x), y: Math.round(n.y) };
  layoutRunning = false;
}

export async function resetDiagramLayout() {
  if (!diagData || layoutRunning) return;
  try { localStorage.removeItem(persistKey()); } catch (_) {}
  document.getElementById('diagram-status').textContent = 'Organizando…';
  await forceLayout(diagData);
  renderDiagram();
  fitDiagram();
  updateStatus();
  persistDiagram();
}

// ─── Helpers SVG ──────────────────────────────────────────────────────────────
function svgEl(tag, attrs, parent, text) {
  const el = document.createElementNS('http://www.w3.org/2000/svg', tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  if (text !== undefined) el.textContent = text;
  if (parent) parent.appendChild(el);
  return el;
}

function svgGlyph(name, px, py, size, color, parent) {
  const g = svgEl('g', {
    transform: `translate(${px},${py}) scale(${size / 16})`,
    fill: 'none', stroke: color, 'stroke-width': 1.7,
    'stroke-linecap': 'round', 'stroke-linejoin': 'round',
  }, parent);
  // ICON_PATHS es contenido estático propio; se parsea vía DOMParser (sin innerHTML).
  const doc = new DOMParser().parseFromString(
    `<svg xmlns="http://www.w3.org/2000/svg">${ICON_PATHS[name] || ''}</svg>`,
    'image/svg+xml');
  Array.from(doc.documentElement.childNodes).forEach(n => g.appendChild(n));
  return g;
}

// ─── Render ───────────────────────────────────────────────────────────────────
function renderDiagram() {
  if (!diagData) return;
  const linesG  = document.getElementById('diagram-lines');
  const tablesG = document.getElementById('diagram-tables');
  linesG.replaceChildren();
  tablesG.replaceChildren();
  tableEls = {};
  fkEls = [];
  edgesByTable = new Map();

  const tableMap = {};
  for (const t of diagData.tables) tableMap[t.name] = t;

  for (let i = 0; i < diagData.fks.length; i++) {
    const fk = diagData.fks[i];
    if (!diagPos[fk.fromTable] || !diagPos[fk.toTable]) continue;
    if (!tableMap[fk.fromTable] || !tableMap[fk.toTable]) continue;
    drawFKLine(linesG, i, fk, tableMap);
    if (!edgesByTable.has(fk.fromTable)) edgesByTable.set(fk.fromTable, new Set());
    if (!edgesByTable.has(fk.toTable)) edgesByTable.set(fk.toTable, new Set());
    edgesByTable.get(fk.fromTable).add(i);
    edgesByTable.get(fk.toTable).add(i);
  }

  for (const t of diagData.tables) {
    const pos = diagPos[t.name] || { x: 100, y: 100 };
    drawTable(tablesG, t, pos);
  }

  applyDiagramTransform();
  renderMinimap();
  if (selectedTable) applyDiagramSelection(selectedTable);
  // re-aplicar búsqueda activa
  const q = document.getElementById('diag-search');
  if (q && q.value.trim()) diagSearch(q.value);
}

// Geometría de una FK (normal o self-loop). Devuelve {d, mid:{x,y}, a:{x,y}, b:{x,y}}.
function fkGeometry(fk, tableMap) {
  const srcPos = diagPos[fk.fromTable], tgtPos = diagPos[fk.toTable];
  const srcT = tableMap[fk.fromTable], tgtT = tableMap[fk.toTable];
  const srcColIdx = Math.max(0, srcT.columns.findIndex(c => c.name === fk.fromCol));
  const tgtColIdx = Math.max(0, tgtT.columns.findIndex(c => c.name === fk.toCol));
  const w = W();

  // Self-FK: bucle por el lado derecho de la caja.
  if (fk.fromTable === fk.toTable) {
    const x = srcPos.x + w;
    const y1 = srcPos.y + colCY(srcColIdx);
    const y2 = srcPos.y + colCY(tgtColIdx) + (srcColIdx === tgtColIdx ? 8 : 0);
    const loop = 44;
    const d = `M ${x} ${y1} C ${x + loop} ${y1} ${x + loop} ${y2} ${x} ${y2}`;
    return { d, mid: { x: x + loop * 0.78, y: (y1 + y2) / 2 }, a: { x, y: y1 }, b: { x, y: y2 } };
  }

  const srcCX = srcPos.x + w / 2, tgtCX = tgtPos.x + w / 2;
  const srcLeft = srcCX > tgtCX;
  const x1 = srcLeft ? srcPos.x : srcPos.x + w;
  const y1 = srcPos.y + colCY(srcColIdx);
  const x2 = srcLeft ? tgtPos.x + w : tgtPos.x;
  const y2 = tgtPos.y + colCY(tgtColIdx);
  const bend = Math.min(D.BEND, Math.abs(x2 - x1) * 0.55 + 50);
  const cp1x = srcLeft ? x1 - bend : x1 + bend;
  const cp2x = srcLeft ? x2 + bend : x2 - bend;
  const d = `M ${x1} ${y1} C ${cp1x} ${y1} ${cp2x} ${y2} ${x2} ${y2}`;
  const mx = 0.125*x1 + 0.375*cp1x + 0.375*cp2x + 0.125*x2;
  const my = 0.5*y1 + 0.5*y2;
  return { d, mid: { x: mx, y: my }, a: { x: x1, y: y1 }, b: { x: x2, y: y2 } };
}

function drawFKLine(parent, fkIdx, fk, tableMap) {
  const geo = fkGeometry(fk, tableMap);

  const g = svgEl('g', {
    class: 'fk-line-group',
    'data-fk-idx':  fkIdx,
    'data-fk-from': fk.fromTable,
    'data-fk-to':   fk.toTable,
  }, parent);

  const glow = svgEl('path', { d: geo.d, fill: 'none', stroke: C.teal, 'stroke-width': 6, opacity: '0.08' }, g);
  const hit  = svgEl('path', { d: geo.d, fill: 'none', stroke: 'transparent', 'stroke-width': 14 }, g);
  const line = svgEl('path', {
    class: 'fk-path', d: geo.d, fill: 'none', stroke: C.teal, 'stroke-width': 2,
    opacity: '0.75', 'marker-end': 'url(#fk-arrow)', 'marker-start': 'url(#fk-circle)',
  }, g);

  // Cardinalidad: N en el origen (lado FK), 1 en el destino (lado PK).
  const cardA = svgEl('text', {
    class: 'fk-card', x: geo.a.x, y: geo.a.y - 7, 'text-anchor': 'middle',
    fill: C.teal, 'font-size': 9, 'font-weight': 700,
    'font-family': 'Cascadia Code, Consolas, monospace', opacity: '0.85',
  }, g, 'N');
  const cardB = svgEl('text', {
    class: 'fk-card', x: geo.b.x, y: geo.b.y - 7, 'text-anchor': 'middle',
    fill: C.gold, 'font-size': 9, 'font-weight': 700,
    'font-family': 'Cascadia Code, Consolas, monospace', opacity: '0.85',
  }, g, '1');

  // Etiqueta flotante (hover)
  const label = `${fk.fromTable}.${fk.fromCol}`;
  const lw = Math.max(56, label.length * 5.6 + 14);
  const labelG = svgEl('g', { class: 'fk-label', transform: `translate(${geo.mid.x},${geo.mid.y})` }, g);
  svgEl('rect', {
    x: -lw/2, y: -9, width: lw, height: 16, rx: 4,
    fill: C.bg, stroke: C.teal, 'stroke-width': 1, opacity: '0.92',
  }, labelG);
  svgEl('text', {
    x: 0, y: 4, 'text-anchor': 'middle', fill: C.teal,
    'font-size': 9, 'font-weight': 600,
    'font-family': 'Cascadia Code, Fira Code, Consolas, monospace',
  }, labelG, label);

  g.addEventListener('mouseenter', () => showDiagramTooltip(`${fk.fromTable}.${fk.fromCol} → ${fk.toTable}.${fk.toCol}`));
  g.addEventListener('mouseleave', hideDiagramTooltip);

  fkEls[fkIdx] = { g, paths: [glow, hit, line], labelG, cardA, cardB };
}

// Recalcula solo la geometría de una FK existente (drag eficiente).
function refreshFKLine(fkIdx) {
  const fk = diagData.fks[fkIdx];
  const el = fkEls[fkIdx];
  if (!fk || !el) return;
  const tableMap = {};
  for (const t of diagData.tables) tableMap[t.name] = t;
  const geo = fkGeometry(fk, tableMap);
  el.paths.forEach(p => p.setAttribute('d', geo.d));
  el.labelG.setAttribute('transform', `translate(${geo.mid.x},${geo.mid.y})`);
  el.cardA.setAttribute('x', geo.a.x); el.cardA.setAttribute('y', geo.a.y - 7);
  el.cardB.setAttribute('x', geo.b.x); el.cardB.setAttribute('y', geo.b.y - 7);
}

function drawTable(parent, table, pos) {
  const w = W();
  const h = tblH(table.columns.length);
  const g = svgEl('g', {
    class: 'tbl-group',
    transform: `translate(${pos.x},${pos.y})`,
    'data-table': table.name,
  }, parent);
  tableEls[table.name] = g;

  svgEl('rect', { x: 4, y: 5, width: w, height: h, rx: 9, fill: 'rgba(0,0,0,0.45)' }, g);
  svgEl('rect', {
    class: 'tbl-box', x: 0, y: 0, width: w, height: h, rx: 9,
    fill: C.box, stroke: C.boxStroke, 'stroke-width': 1.5,
  }, g);
  svgEl('rect', { x: 0, y: 0, width: w, height: D.HDR, rx: 9, fill: C.hdr }, g);
  svgEl('rect', { x: 0, y: D.HDR - 9, width: w, height: 9, fill: C.hdr }, g);
  svgEl('rect', { x: 0, y: 0, width: w, height: 3, rx: 2, fill: C.accent }, g);

  svgEl('text', {
    x: 10, y: D.HDR - 14,
    fill: C.name, 'font-size': 13, 'font-weight': 700,
    'font-family': 'Segoe UI, system-ui, sans-serif', 'letter-spacing': '0.3',
  }, g, table.name);

  // rowCount (si el API lo trae)
  if (typeof table.rowCount === 'number') {
    svgEl('text', {
      x: 10, y: D.HDR - 3,
      fill: C.dim, 'font-size': 8.5,
      'font-family': 'Cascadia Code, Consolas, monospace',
    }, g, `${table.rowCount} fila${table.rowCount !== 1 ? 's' : ''}`);
  }

  // badge conteo de columnas
  svgEl('rect', { x: w - 28, y: 6, width: 22, height: 13, rx: 6, fill: 'rgba(123,182,97,0.2)' }, g);
  svgEl('text', {
    x: w - 17, y: 16, 'text-anchor': 'middle', fill: C.accent,
    'font-size': 9, 'font-weight': 600, 'font-family': 'Segoe UI, system-ui, sans-serif',
  }, g, table.columns.length);

  svgEl('line', { x1: 0, y1: D.HDR, x2: w, y2: D.HDR, stroke: C.sep, 'stroke-width': 1 }, g);

  // columnas con índice (para el badge)
  const idxCols = new Set();
  (table.indexes || []).forEach(ix => (ix.columns || []).forEach(c => idxCols.add(c)));

  table.columns.forEach((col, i) => {
    const ry = D.HDR + i * D.ROW;
    const isLast = i === table.columns.length - 1;
    const rowG = svgEl('g', { class: 'col-row' }, g);

    if (col.isPK) {
      svgEl('rect', { x: 1, y: ry, width: w - 1, height: isLast ? D.ROW + D.PAD : D.ROW, fill: 'rgba(230,194,106,0.06)', rx: isLast ? 8 : 0 }, rowG);
    } else if (col.isFK) {
      svgEl('rect', { x: 1, y: ry, width: w - 1, height: isLast ? D.ROW + D.PAD : D.ROW, fill: 'rgba(111,179,168,0.05)', rx: isLast ? 8 : 0 }, rowG);
    } else if (i % 2 === 0) {
      svgEl('rect', { x: 1, y: ry, width: w - 1, height: isLast ? D.ROW + D.PAD : D.ROW, fill: 'rgba(255,255,255,0.018)', rx: isLast ? 8 : 0 }, rowG);
    }

    const barColor = col.isPK ? C.gold : col.isFK ? C.teal : C.boxStroke;
    svgEl('rect', { x: 0, y: ry + 3, width: 3, height: D.ROW - 6, rx: 1.5, fill: barColor, opacity: col.isPK || col.isFK ? '0.8' : '0.3' }, rowG);

    if (col.isPK) svgGlyph('key', 7, ry + 3, 12, C.gold, rowG);
    else if (col.isFK) svgGlyph('link', 7, ry + 3, 12, C.teal, rowG);
    else svgEl('circle', { cx: 13, cy: ry + 11, r: 2, fill: '#3a5442' }, rowG);

    svgEl('text', {
      x: 26, y: ry + 15,
      fill: col.isPK ? C.gold : col.isFK ? C.teal : C.text,
      'font-size': 11.5, 'font-weight': col.isPK || col.isFK ? 600 : 400,
      'font-family': 'Cascadia Code, Fira Code, Consolas, monospace',
    }, rowG, col.name);

    // badges a la derecha: índice / unique / tipo (tipo solo en modo detallado)
    let rightX = w - 6;
    if (!compactMode) {
      const typeLabel = col.type ? col.type.toUpperCase() : '';
      const typeW = Math.max(28, typeLabel.length * 6 + 8);
      rightX -= typeW;
      svgEl('rect', {
        class: 'col-type-text', x: rightX, y: ry + 4, width: typeW, height: 13, rx: 4,
        fill: col.isPK ? 'rgba(230,194,106,0.12)' : col.isFK ? 'rgba(111,179,168,0.12)' : 'rgba(125,139,120,0.25)',
      }, rowG);
      svgEl('text', {
        class: 'col-type-text', x: rightX + typeW / 2, y: ry + 14, 'text-anchor': 'middle',
        fill: col.isPK ? C.gold : col.isFK ? C.teal : C.dim,
        'font-size': 9, 'font-weight': 600,
        'font-family': 'Cascadia Code, Fira Code, Consolas, monospace',
      }, rowG, typeLabel);
      rightX -= 4;
    }
    if (idxCols.has(col.name)) {
      rightX -= 12;
      svgEl('text', {
        x: rightX, y: ry + 14, fill: C.accent, 'font-size': 8.5, 'font-weight': 700,
        'font-family': 'Cascadia Code, Consolas, monospace', opacity: '0.8',
      }, rowG, 'ix');
    }
    if (col.isUnique) {
      rightX -= 12;
      svgEl('text', {
        x: rightX, y: ry + 14, fill: C.gold, 'font-size': 8.5, 'font-weight': 700,
        'font-family': 'Cascadia Code, Consolas, monospace', opacity: '0.8',
      }, rowG, 'u');
    }

    if (!isLast) {
      svgEl('line', { x1: 6, y1: ry + D.ROW, x2: w - 6, y2: ry + D.ROW, stroke: '#1d3325', 'stroke-width': 0.8 }, rowG);
    }
  });

  g.addEventListener('click', e => { e.stopPropagation(); selectDiagramTable(table.name); });
  g.addEventListener('dblclick', e => {
    e.stopPropagation();
    showView('query');
    state.editor.setValue(`SELECT *\nFROM ${table.name}\nLIMIT 100;`);
    setTimeout(runQuery, 60);
  });
  g.addEventListener('pointerdown', e => {
    if (e.button !== 0 && e.pointerType === 'mouse') return;
    e.stopPropagation();
    startTableDrag(e, table.name);
  });
}

// Mueve una tabla actualizando solo su transform + FKs incidentes (sin re-render).
function moveTable(name, x, y) {
  diagPos[name] = { x, y };
  const g = tableEls[name];
  if (g) g.setAttribute('transform', `translate(${x},${y})`);
  const edges = edgesByTable.get(name);
  if (edges) for (const idx of edges) refreshFKLine(idx);
}

// ─── Tooltip ─────────────────────────────────────────────────────────────────
let diagTooltip = null;
function showDiagramTooltip(text) {
  if (!diagTooltip) {
    diagTooltip = document.createElement('div');
    diagTooltip.style.cssText =
      'position:fixed;background:#17281d;color:#e7e3d3;padding:4px 10px;' +
      'border-radius:6px;font-size:11px;pointer-events:none;z-index:200;' +
      'border:1px solid #24382b;white-space:nowrap';
    document.body.appendChild(diagTooltip);
  }
  diagTooltip.textContent = text;
  diagTooltip.style.display = 'block';
}
function hideDiagramTooltip() {
  if (diagTooltip) diagTooltip.style.display = 'none';
}
document.addEventListener('mousemove', e => {
  if (diagTooltip && diagTooltip.style.display === 'block') {
    diagTooltip.style.left = (e.clientX + 14) + 'px';
    diagTooltip.style.top  = (e.clientY - 8)  + 'px';
  }
});

// ─── Transform / zoom ─────────────────────────────────────────────────────────
function applyDiagramTransform() {
  document.getElementById('diagram-root').setAttribute(
    'transform', `translate(${diagTx},${diagTy}) scale(${diagScale})`
  );
  const pctEl = document.getElementById('diag-zoom-pct');
  if (pctEl) pctEl.textContent = Math.round(diagScale * 100) + '%';
  updateMinimapViewport();
}

function getDiagramBounds() {
  if (!diagData || !diagData.tables.length) return null;
  const w = W();
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const t of diagData.tables) {
    const p = diagPos[t.name];
    if (!p) continue;
    minX = Math.min(minX, p.x);
    minY = Math.min(minY, p.y);
    maxX = Math.max(maxX, p.x + w);
    maxY = Math.max(maxY, p.y + tblH(t.columns.length));
  }
  if (minX === Infinity) return null;
  return { minX, minY, maxX, maxY };
}

export function fitDiagram() {
  const b = getDiagramBounds();
  if (!b) return;
  const svg    = document.getElementById('diagram-svg');
  const vw     = svg.clientWidth  || 900;
  const vh     = svg.clientHeight || 600;
  const margin = 60;

  const contentW = b.maxX - b.minX || 1;
  const contentH = b.maxY - b.minY || 1;
  const scale = Math.min((vw - margin*2) / contentW, (vh - margin*2) / contentH, 1.2);

  diagScale = scale;
  diagTx = (vw - contentW * scale) / 2 - b.minX * scale;
  diagTy = (vh - contentH * scale) / 2 - b.minY * scale;
  applyDiagramTransform();
  persistDiagram();
}

function zoomAt(mx, my, factor) {
  const next = Math.max(SCALE_MIN, Math.min(SCALE_MAX, diagScale * factor));
  const f = next / diagScale;
  if (f === 1) return;
  diagTx = mx - (mx - diagTx) * f;
  diagTy = my - (my - diagTy) * f;
  diagScale = next;
  applyDiagramTransform();
  persistDiagram();
}

// Zoom con rueda (con clamp, anclado al cursor)
document.getElementById('diagram-svg').addEventListener('wheel', e => {
  e.preventDefault();
  const rect = e.currentTarget.getBoundingClientRect();
  zoomAt(e.clientX - rect.left, e.clientY - rect.top, e.deltaY < 0 ? 1.12 : 1 / 1.12);
}, { passive: false });

export function diagZoomBy(delta) {
  const svg = document.getElementById('diagram-svg');
  const next = Math.max(SCALE_MIN, Math.min(SCALE_MAX, diagScale + delta));
  zoomAt(svg.clientWidth / 2, svg.clientHeight / 2, next / diagScale);
}

// ─── Pointer Events unificados: pan / drag de tabla / pinch ───────────────────
const svgRoot = document.getElementById('diagram-svg');

function capturePointer(id) {
  try { svgRoot.setPointerCapture(id); } catch (_) { /* puntero ya inactivo */ }
}

function startTableDrag(e, name) {
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
  dragMode = {
    kind: 'table', name,
    ox: diagPos[name].x, oy: diagPos[name].y,
    sx: e.clientX, sy: e.clientY,
  };
  capturePointer(e.pointerId);
}

svgRoot.addEventListener('pointerdown', e => {
  if (e.target.closest('.tbl-group')) return; // el grupo maneja su propio drag
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
  if (pointers.size === 2) {
    // pinch: guardar distancia inicial
    const [p1, p2] = [...pointers.values()];
    dragMode = { kind: 'pinch', d0: Math.hypot(p2.x - p1.x, p2.y - p1.y), s0: diagScale };
  } else {
    dragMode = { kind: 'pan', ox: diagTx, oy: diagTy, sx: e.clientX, sy: e.clientY };
    svgRoot.classList.add('panning');
  }
  capturePointer(e.pointerId);
});

svgRoot.addEventListener('pointermove', e => {
  if (!dragMode || !pointers.has(e.pointerId)) return;
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });

  if (dragMode.kind === 'pinch' && pointers.size === 2) {
    const [p1, p2] = [...pointers.values()];
    const d = Math.hypot(p2.x - p1.x, p2.y - p1.y);
    const rect = svgRoot.getBoundingClientRect();
    const cx = (p1.x + p2.x) / 2 - rect.left;
    const cy = (p1.y + p2.y) / 2 - rect.top;
    const target = Math.max(SCALE_MIN, Math.min(SCALE_MAX, dragMode.s0 * (d / dragMode.d0)));
    zoomAt(cx, cy, target / diagScale);
    return;
  }
  if (dragMode.kind === 'pan') {
    diagTx = dragMode.ox + (e.clientX - dragMode.sx);
    diagTy = dragMode.oy + (e.clientY - dragMode.sy);
    applyDiagramTransform();
  } else if (dragMode.kind === 'table') {
    const dx = (e.clientX - dragMode.sx) / diagScale;
    const dy = (e.clientY - dragMode.sy) / diagScale;
    moveTable(dragMode.name, Math.round(dragMode.ox + dx), Math.round(dragMode.oy + dy));
  }
});

function endPointer(e) {
  pointers.delete(e.pointerId);
  if (!dragMode) return;
  if (dragMode.kind === 'table') {
    // snap a rejilla de 10px
    const p = diagPos[dragMode.name];
    moveTable(dragMode.name, Math.round(p.x / 10) * 10, Math.round(p.y / 10) * 10);
    renderMinimap();
  }
  if (pointers.size === 0) {
    svgRoot.classList.remove('panning');
    dragMode = null;
    persistDiagram();
  }
}
svgRoot.addEventListener('pointerup', endPointer);
svgRoot.addEventListener('pointercancel', endPointer);

// Click en fondo → deseleccionar
svgRoot.addEventListener('click', e => {
  if (e.target.closest('.tbl-group')) return;
  selectedTable = null;
  clearDiagramSelection();
});

// ─── Selección ────────────────────────────────────────────────────────────────
function selectDiagramTable(name) {
  if (selectedTable === name) {
    selectedTable = null;
    clearDiagramSelection();
  } else {
    selectedTable = name;
    applyDiagramSelection(name);
  }
}

function clearDiagramSelection() {
  document.querySelectorAll('.tbl-group').forEach(g =>
    g.classList.remove('selected', 'connected', 'dimmed'));
  document.querySelectorAll('.fk-line-group').forEach(l =>
    l.classList.remove('selected', 'dimmed'));
}

function applyDiagramSelection(name) {
  if (!diagData) return;
  const connectedTables = new Set();
  const connectedIdx    = new Set();

  diagData.fks.forEach((fk, i) => {
    if (fk.fromTable === name || fk.toTable === name) {
      connectedTables.add(fk.fromTable === name ? fk.toTable : fk.fromTable);
      connectedIdx.add(i);
    }
  });

  document.querySelectorAll('.tbl-group').forEach(g => {
    const t = g.dataset.table;
    g.classList.remove('selected', 'connected', 'dimmed');
    if (t === name)                  g.classList.add('selected');
    else if (connectedTables.has(t)) g.classList.add('connected');
    else                             g.classList.add('dimmed');
  });

  document.querySelectorAll('.fk-line-group').forEach(l => {
    const idx = Number(l.dataset.fkIdx);
    l.classList.remove('selected', 'dimmed');
    if (connectedIdx.has(idx)) l.classList.add('selected');
    else                       l.classList.add('dimmed');
  });
}

// ─── Búsqueda ─────────────────────────────────────────────────────────────────
export function diagSearch(query) {
  if (!query.trim()) {
    document.querySelectorAll('.tbl-group').forEach(g =>
      g.classList.remove('search-match', 'search-dim'));
    return;
  }
  const q = query.toLowerCase();
  document.querySelectorAll('.tbl-group').forEach(g => {
    const match = (g.dataset.table || '').toLowerCase().includes(q);
    g.classList.toggle('search-match', match);
    g.classList.toggle('search-dim',  !match);
  });
}

// ─── Modo compacto ────────────────────────────────────────────────────────────
function applyCompact(on) {
  compactMode = on;
  document.getElementById('diagram-svg').classList.toggle('compact', on);
  const btn = document.getElementById('btn-compact');
  if (btn) btn.textContent = on ? '≡ Detallado' : '≡ Compacto';
}

export function toggleCompact() {
  applyCompact(!compactMode);
  renderDiagram();   // re-render con el ancho nuevo (W() cambia)
  persistDiagram();
}

// ─── Minimapa ─────────────────────────────────────────────────────────────────
let miniScale = 1, miniOffX = 0, miniOffY = 0;

function renderMinimap() {
  const mini = document.getElementById('diagram-minimap');
  if (!mini || !diagData) return;
  const b = getDiagramBounds();
  if (!b) { mini.replaceChildren(); return; }

  const MW = 168, MH = 120, P = 8;
  const w = W();
  const contentW = b.maxX - b.minX || 1, contentH = b.maxY - b.minY || 1;
  miniScale = Math.min((MW - P*2) / contentW, (MH - P*2) / contentH);
  miniOffX = P - b.minX * miniScale + ((MW - P*2) - contentW * miniScale) / 2;
  miniOffY = P - b.minY * miniScale + ((MH - P*2) - contentH * miniScale) / 2;

  mini.replaceChildren();
  for (const t of diagData.tables) {
    const p = diagPos[t.name];
    if (!p) continue;
    svgEl('rect', {
      x: p.x * miniScale + miniOffX,
      y: p.y * miniScale + miniOffY,
      width: Math.max(3, w * miniScale),
      height: Math.max(2, tblH(t.columns.length) * miniScale),
      rx: 1, fill: t.name === selectedTable ? C.accent : '#3a5442',
    }, mini);
  }
  svgEl('rect', { class: 'mini-viewport', x: 0, y: 0, width: 0, height: 0, fill: 'none', stroke: C.accent, 'stroke-width': 1.2, rx: 2, opacity: '0.9' }, mini);
  updateMinimapViewport();
}

function updateMinimapViewport() {
  const mini = document.getElementById('diagram-minimap');
  const vp = mini?.querySelector('.mini-viewport');
  if (!vp) return;
  const svg = document.getElementById('diagram-svg');
  // viewport en coordenadas de mundo
  const wx = -diagTx / diagScale, wy = -diagTy / diagScale;
  const ww = svg.clientWidth / diagScale, wh = svg.clientHeight / diagScale;
  vp.setAttribute('x', wx * miniScale + miniOffX);
  vp.setAttribute('y', wy * miniScale + miniOffY);
  vp.setAttribute('width', Math.max(4, ww * miniScale));
  vp.setAttribute('height', Math.max(4, wh * miniScale));
}

// Click/drag en el minimapa → centrar el viewport ahí
function miniPan(e) {
  const mini = document.getElementById('diagram-minimap');
  const rect = mini.getBoundingClientRect();
  const mx = e.clientX - rect.left, my = e.clientY - rect.top;
  const wx = (mx - miniOffX) / miniScale;
  const wy = (my - miniOffY) / miniScale;
  const svg = document.getElementById('diagram-svg');
  diagTx = svg.clientWidth / 2 - wx * diagScale;
  diagTy = svg.clientHeight / 2 - wy * diagScale;
  applyDiagramTransform();
  persistDiagram();
}
document.getElementById('diagram-minimap')?.addEventListener('pointerdown', e => {
  e.stopPropagation();
  miniPan(e);
  const move = ev => miniPan(ev);
  const up = () => { document.removeEventListener('pointermove', move); document.removeEventListener('pointerup', up); };
  document.addEventListener('pointermove', move);
  document.addEventListener('pointerup', up);
});

// ─── Export ───────────────────────────────────────────────────────────────────
function buildExportSVGString() {
  const b = getDiagramBounds();
  if (!b) return null;
  const PAD = 50;
  const EW  = b.maxX - b.minX + PAD * 2;
  const EH  = b.maxY - b.minY + PAD * 2;
  const NS  = 'http://www.w3.org/2000/svg';

  const root = document.createElementNS(NS, 'svg');
  root.setAttribute('xmlns', NS);
  root.setAttribute('width', EW);
  root.setAttribute('height', EH);
  root.setAttribute('viewBox', `0 0 ${EW} ${EH}`);

  const bg = document.createElementNS(NS, 'rect');
  bg.setAttribute('width', EW); bg.setAttribute('height', EH); bg.setAttribute('fill', C.bg);
  root.appendChild(bg);

  const defs = document.createElementNS(NS, 'defs');
  const pat  = document.createElementNS(NS, 'pattern');
  pat.setAttribute('id', 'xdots'); pat.setAttribute('width', '28'); pat.setAttribute('height', '28');
  pat.setAttribute('patternUnits', 'userSpaceOnUse');
  const dot = document.createElementNS(NS, 'circle');
  dot.setAttribute('cx', '1'); dot.setAttribute('cy', '1'); dot.setAttribute('r', '1'); dot.setAttribute('fill', C.grid);
  pat.appendChild(dot); defs.appendChild(pat);
  const origDefs = document.querySelector('#diagram-svg defs');
  if (origDefs) [...origDefs.children].forEach(c => defs.appendChild(c.cloneNode(true)));
  root.appendChild(defs);

  const gridRect = document.createElementNS(NS, 'rect');
  gridRect.setAttribute('width', EW); gridRect.setAttribute('height', EH); gridRect.setAttribute('fill', 'url(#xdots)');
  root.appendChild(gridRect);

  const content = document.createElementNS(NS, 'g');
  content.setAttribute('transform', `translate(${PAD - b.minX}, ${PAD - b.minY})`);

  const linesClone  = document.getElementById('diagram-lines').cloneNode(true);
  const tablesClone = document.getElementById('diagram-tables').cloneNode(true);

  [linesClone, tablesClone].forEach(c => {
    // quitar clases de interacción y las etiquetas hover (invisibles en pantalla
    // por CSS de la página; sin ese CSS aparecerían todas en el export)
    c.querySelectorAll('.fk-label').forEach(el => el.remove());
    c.querySelectorAll('[class]').forEach(el => {
      el.classList.remove('selected','connected','dimmed','search-match','search-dim');
    });
  });

  content.appendChild(linesClone);
  content.appendChild(tablesClone);
  root.appendChild(content);

  return '<?xml version="1.0" encoding="UTF-8"?>\n' +
         new XMLSerializer().serializeToString(root);
}

export function exportSVG() {
  try {
    const str = buildExportSVGString();
    if (!str) { showToast('No hay diagrama para exportar', 'err'); return; }
    downloadBlob(new Blob([str], { type: 'image/svg+xml;charset=utf-8' }), 'focusdb-schema.svg');
    showToast('SVG exportado');
  } catch (e) {
    showToast('Error al exportar SVG: ' + e.message, 'err');
  }
}

export async function exportPNG() {
  try {
    const str = buildExportSVGString();
    if (!str) { showToast('No hay diagrama para exportar', 'err'); return; }

    const b = getDiagramBounds();
    const PAD = 50, SCALE = 2;
    const EW = (b.maxX - b.minX + PAD * 2);
    const EH = (b.maxY - b.minY + PAD * 2);

    const canvas = document.createElement('canvas');
    canvas.width  = EW * SCALE;
    canvas.height = EH * SCALE;
    const ctx = canvas.getContext('2d');
    ctx.scale(SCALE, SCALE);

    const blob = new Blob([str], { type: 'image/svg+xml;charset=utf-8' });
    const url  = URL.createObjectURL(blob);
    const img  = new Image();
    await new Promise((res, rej) => { img.onload = res; img.onerror = () => rej(new Error('no se pudo rasterizar el SVG')); img.src = url; });
    ctx.drawImage(img, 0, 0);
    URL.revokeObjectURL(url);

    canvas.toBlob(pngBlob => {
      if (!pngBlob) { showToast('Error al generar el PNG', 'err'); return; }
      downloadBlob(pngBlob, 'focusdb-schema.png');
      showToast('PNG exportado');
    }, 'image/png');
  } catch (e) {
    showToast('Error al exportar PNG: ' + e.message, 'err');
  }
}

// Descarta datos y posiciones en memoria (cambio de esquema activo): la
// próxima entrada a la vista vuelve a pedir el diagrama del esquema nuevo.
export function discardDiagram() {
  diagData = null;
  diagPos = {};
}

// Refresh explícito (botón ↺): refetch del schema conservando posiciones.
export function reloadDiagram() {
  const saved = { pos: diagPos, tx: diagTx, ty: diagTy, scale: diagScale };
  diagData = null;
  loadDiagram(true).then(() => {
    // conservar lo que siga existiendo
    for (const name of Object.keys(saved.pos)) {
      if (diagPos[name]) diagPos[name] = saved.pos[name];
    }
    renderDiagram();
  });
}

// ─── Atajos de teclado (vista diagrama) ───────────────────────────────────────
document.addEventListener('keydown', e => {
  if (!document.getElementById('diagram-view').classList.contains('visible')) return;
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
  switch (e.key) {
    case 'f': case 'F': fitDiagram(); break;
    case 'r': case 'R': resetDiagramLayout(); break;
    case 'Escape': selectedTable = null; clearDiagramSelection(); break;
    case '+': case '=': diagZoomBy(0.15); break;
    case '-': case '_': diagZoomBy(-0.15); break;
  }
});
