// ─── App: vistas, bootstrap y puente global para handlers inline ──────────────
import { spawnMotes } from './dom.js';
import {
  initEditor, initResizer, initResizerTouch,
  runQuery, runAll, clearEditor, restoreHistory, cancelRun,
} from './editor.js';
import { addTab, selectTab, closeTab, renameTab } from './tabs.js';
import {
  loadSidebar, filterTree, toggleSection, toggleCols,
  quickSelect, insertColRef, insertCall, toggleSidebar, closeSidebar,
} from './sidebar.js';
import {
  sortResults, filterResults, loadMoreRows, copyRowInsert,
  exportResultsCSV, exportResultsJSON, closeCellModal, copyCellModal,
} from './results.js';
import {
  loadDiagram, reloadDiagram, discardDiagram, fitDiagram, resetDiagramLayout, diagSearch,
  diagZoomBy, toggleCompact, exportSVG, exportPNG,
} from './diagram.js';
import {
  openExplorer, explorerPage, explorerRefresh, explorerEditCell,
  explorerDeleteRow, explorerDeleteConfirm, explorerInsertOpen, explorerInsertConfirm,
} from './explorer.js';
import {
  setActiveSchema, schemaCreateOpen, schemaCreateConfirm, schemaDropOpen, schemaDropConfirm,
} from './schemas.js';

// ─── Tabs (Resultados / Historial) ───────────────────────────────────────────
export function activateTab(name) {
  ['results','history'].forEach(t => {
    document.getElementById('tab-'+t).classList.toggle('active', t===name);
    document.getElementById('tc-'+t).classList.toggle('active', t===name);
  });
}

// ─── Sidebar refresh after DDL ────────────────────────────────────────────────
let refreshPending = false;
export function refreshSidebar() {
  if (refreshPending) return;
  refreshPending = true;
  setTimeout(async () => {
    await loadSidebar();
    refreshPending = false;
  }, 400);
}

// ─── Cambio de esquema activo ─────────────────────────────────────────────────
// El árbol, el diagrama y el explorador dependen del esquema: se recargan.
export function onSchemaChanged() {
  loadSidebar();
  discardDiagram();
  const diagramVisible  = document.getElementById('diagram-view').classList.contains('visible');
  const explorerVisible = document.getElementById('explorer-view').classList.contains('visible');
  if (diagramVisible) loadDiagram();
  if (explorerVisible) showView('query');
}

// ─── View switching (query | diagram | explorer) ──────────────────────────────
export function showView(name) {
  const isDiagram  = name === 'diagram';
  const isExplorer = name === 'explorer';
  document.querySelector('.workspace').style.display = (isDiagram || isExplorer) ? 'none' : 'flex';
  document.getElementById('diagram-view').classList.toggle('visible', isDiagram);
  document.getElementById('explorer-view').classList.toggle('visible', isExplorer);
  document.getElementById('nav-query').classList.toggle('active', !isDiagram && !isExplorer);
  document.getElementById('nav-diagram').classList.toggle('active', isDiagram);
  if (isDiagram) loadDiagram();
}

// ─── Puente global: el markup usa handlers inline (onclick/oninput) ───────────
// Checklist derivada de grep 'on[a-z]*=' sobre index.html + HTML generado.
Object.assign(window, {
  // header / vistas
  showView, toggleSidebar, closeSidebar, activateTab,
  // toolbar editor
  runQuery, runAll, clearEditor, cancelRun,
  // pestañas de consulta (markup generado)
  addTab, selectTab, closeTab, renameTab,
  // sidebar (markup generado)
  filterTree, toggleSection, toggleCols, quickSelect, insertColRef, insertCall,
  // resultados / historial (markup generado)
  sortResults, restoreHistory, filterResults, loadMoreRows, copyRowInsert,
  exportResultsCSV, exportResultsJSON, closeCellModal, copyCellModal,
  // diagrama
  loadDiagram, reloadDiagram, fitDiagram, resetDiagramLayout, diagSearch,
  diagZoomBy, toggleCompact, exportSVG, exportPNG,
  // explorador de datos
  openExplorer, explorerPage, explorerRefresh, explorerEditCell,
  explorerDeleteRow, explorerDeleteConfirm, explorerInsertOpen, explorerInsertConfirm,
  // esquemas (header + sidebar + modales)
  setActiveSchema, schemaCreateOpen, schemaCreateConfirm, schemaDropOpen, schemaDropConfirm,
});

// ─── Bootstrap ────────────────────────────────────────────────────────────────
window.addEventListener('DOMContentLoaded', () => {
  initEditor();
  loadSidebar();
  initResizer();
  initResizerTouch();
  spawnMotes();
});
