// ─── Estado compartido entre módulos ─────────────────────────────────────────
// Un único objeto mutable: los módulos mutan propiedades, nunca reasignan
// bindings importados (pitfall de live-bindings de ES modules).
const SCHEMA_KEY = 'focusdb.schema.v1';

function loadSchema() {
  try { return localStorage.getItem(SCHEMA_KEY) || 'public'; } catch (_) { return 'public'; }
}

export const state = {
  editor: null,           // instancia CodeMirror (la crea editor.js en el bootstrap)
  schema: loadSchema(),   // esquema activo: califica lo no calificado en consultas y metadatos
};

// Cambia el esquema activo y lo persiste (sobrevive recargas).
export function persistSchema(name) {
  state.schema = name || 'public';
  try { localStorage.setItem(SCHEMA_KEY, state.schema); } catch (_) {}
}
