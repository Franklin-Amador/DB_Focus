// ─── Iconografía de línea (SVG, heredan color) ─────────────────────────────────
export const ICON_PATHS = {
  schema:  '<ellipse cx="8" cy="4" rx="5.5" ry="2.2"/><path d="M2.5 4v8c0 1.2 2.5 2.2 5.5 2.2s5.5-1 5.5-2.2V4"/><path d="M2.5 8c0 1.2 2.5 2.2 5.5 2.2s5.5-1 5.5-2.2"/>',
  table:   '<rect x="2" y="3" width="12" height="10" rx="1.5"/><line x1="2" y1="6.5" x2="14" y2="6.5"/><line x1="7" y1="6.5" x2="7" y2="13"/>',
  view:    '<path d="M1.5 8C4 4.5 12 4.5 14.5 8 12 11.5 4 11.5 1.5 8Z"/><circle cx="8" cy="8" r="1.9"/>',
  proc:    '<circle cx="8" cy="8" r="2.3"/><path d="M8 1.6v2M8 12.4v2M1.6 8h2M12.4 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M12.6 3.4l-1.4 1.4M4.8 11.2l-1.4 1.4"/>',
  trigger: '<path d="M9 1.5 4 9h4l-1 5.5L12 7H8z"/>',
  job:     '<circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.5 1.5"/>',
  caret:   '<path d="M6 4l4 4-4 4"/>',
  column:  '<circle cx="8" cy="8" r="2"/>',
  key:     '<circle cx="5.5" cy="6" r="2.7"/><path d="M7.6 7.9 13 13M11 11l1.4-1.4M12.6 12.6l1.2-1.2"/>',
  link:    '<path d="M6.6 9.4 9.4 6.6"/><path d="M9 4.6l1-1a2.4 2.4 0 0 1 3.4 3.4l-1 1"/><path d="M7 11.4l-1 1a2.4 2.4 0 0 1-3.4-3.4l1-1"/>',
};

export function icon(name, size = 14, cls = '') {
  const p = ICON_PATHS[name] || '';
  return `<svg class="${cls}" width="${size}" height="${size}" viewBox="0 0 16 16" fill="none" `
       + `stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">${p}</svg>`;
}
