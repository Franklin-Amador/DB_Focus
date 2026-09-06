// ─── Estado compartido entre módulos ─────────────────────────────────────────
// Un único objeto mutable: los módulos mutan propiedades, nunca reasignan
// bindings importados (pitfall de live-bindings de ES modules).
const DATABASE_KEY = 'focusdb.database.v1';
const SCHEMA_KEY   = 'focusdb.schema.v1';

function loadKey(key, fallback) {
  try { return localStorage.getItem(key) || fallback; } catch (_) { return fallback; }
}

export const state = {
  editor: null,                               // instancia CodeMirror (la crea editor.js en el bootstrap)
  database: loadKey(DATABASE_KEY, 'postgres'), // base de datos activa: contenedor de los esquemas
  schema: loadKey(SCHEMA_KEY, 'public'),       // esquema activo dentro de la base: califica lo no calificado
};

// Cambia el esquema activo y lo persiste (sobrevive recargas).
export function persistSchema(name) {
  state.schema = name || 'public';
  try { localStorage.setItem(SCHEMA_KEY, state.schema); } catch (_) {}
}

// Cambia la base de datos activa; el esquema vuelve a public (cada base tiene los suyos).
export function persistDatabase(name) {
  state.database = name || 'postgres';
  try { localStorage.setItem(DATABASE_KEY, state.database); } catch (_) {}
  persistSchema('public');
}
