// ─── Cliente del API del GUI server ───────────────────────────────────────────
import { state } from './state.js';

// Filas máximas que pedimos por defecto: protege al DOM de resultados gigantes.
// El backend marca truncated:true cuando recorta.
export const DEFAULT_MAX_ROWS = 5000;

async function postJSON(url, body, signal) {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  return res.json();
}

function getJSON(url, params) {
  const q = params ? '?' + new URLSearchParams(params) : '';
  return fetch(url + q).then(r => r.json());
}

// Toda ejecución viaja con el esquema activo: los nombres sin calificar se
// resuelven ahí (equivalente al search_path de una sesión).
export function apiQuery(sql, opts = {}) {
  return postJSON('/api/query', {
    sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS, schema: opts.schema ?? state.schema,
  }, opts.signal);
}

export function apiScript(sql, opts = {}) {
  return postJSON('/api/script', {
    sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS, schema: opts.schema ?? state.schema,
  }, opts.signal);
}

export function apiValidate(sql) { return postJSON('/api/validate', { sql }); }

export function apiSchemas() { return getJSON('/api/schemas'); }
export function apiSchema(schema = state.schema)  { return getJSON('/api/schema', { schema }); }
export function apiObjects() { return getJSON('/api/objects'); }
export function apiDiagram(schema = state.schema) { return getJSON('/api/diagram', { schema }); }

export function apiTableData(table, offset = 0, limit = 100, schema = state.schema) {
  return getJSON('/api/table-data', { table, offset: String(offset), limit: String(limit), schema });
}
