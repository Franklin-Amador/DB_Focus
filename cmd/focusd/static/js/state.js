// ─── Estado compartido entre módulos ─────────────────────────────────────────
// Un único objeto mutable: los módulos mutan propiedades, nunca reasignan
// bindings importados (pitfall de live-bindings de ES modules).
export const state = {
  editor: null,   // instancia CodeMirror (la crea editor.js en el bootstrap)
};
