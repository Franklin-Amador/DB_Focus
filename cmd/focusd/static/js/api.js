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

// Toda ejecución viaja con la base y el esquema activos: los statements corren
// dentro de la base, y los nombres sin calificar se resuelven en el esquema
// (equivalente a la base de conexión + search_path de una sesión).
export function apiQuery(sql, opts = {}) {
  return postJSON('/api/query', {
    sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS,
    database: opts.database ?? state.database, schema: opts.schema ?? state.schema,
  }, opts.signal);
}

export function apiScript(sql, opts = {}) {
  return postJSON('/api/script', {
    sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS,
    database: opts.database ?? state.database, schema: opts.schema ?? state.schema,
  }, opts.signal);
}

export function apiValidate(sql) { return postJSON('/api/validate', { sql }); }

export function apiDatabases() { return getJSON('/api/databases'); }
export function apiSchemas(database = state.database) { return getJSON('/api/schemas', { database }); }
export function apiSchema(schema = state.schema, database = state.database) {
  return getJSON('/api/schema', { database, schema });
}
export function apiObjects(database = state.database) { return getJSON('/api/objects', { database }); }
export function apiDiagram(schema = state.schema, database = state.database) {
  return getJSON('/api/diagram', { database, schema });
}

export function apiTableData(table, offset = 0, limit = 100, schema = state.schema, database = state.database) {
  return getJSON('/api/table-data', { database, schema, table, offset: String(offset), limit: String(limit) });
}
