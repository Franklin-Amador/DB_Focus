// ─── Cliente del API del GUI server ───────────────────────────────────────────

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

export function apiQuery(sql, opts = {}) {
  return postJSON('/api/query', { sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS }, opts.signal);
}

export function apiScript(sql, opts = {}) {
  return postJSON('/api/script', { sql, maxRows: opts.maxRows ?? DEFAULT_MAX_ROWS }, opts.signal);
}

export function apiValidate(sql) { return postJSON('/api/validate', { sql }); }

export function apiSchema()  { return fetch('/api/schema').then(r => r.json()); }
export function apiObjects() { return fetch('/api/objects').then(r => r.json()); }
export function apiDiagram() { return fetch('/api/diagram').then(r => r.json()); }

export function apiTableData(table, offset = 0, limit = 100) {
  const q = new URLSearchParams({ table, offset: String(offset), limit: String(limit) });
  return fetch('/api/table-data?' + q).then(r => r.json());
}
