// ─── Utilidades DOM seguras + ambiente ────────────────────────────────────────

export function escHtml(s) {
  return String(s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;');
}

export function escAttr(s) { return escHtml(s).replace(/'/g,'&#39;'); }

// Inyecta markup propio de forma segura (parse + replaceChildren, sin innerHTML directo).
export function render(el, html) {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  el.replaceChildren(...Array.from(doc.body.childNodes));
}

// ─── Toasts: avisos discretos ──────────────────────────────────────────────────
export function showToast(msg, kind = '') {
  const host = document.getElementById('toasts');
  if (!host) return;
  const t = document.createElement('div');
  t.className = 'toast' + (kind ? ' ' + kind : '');
  const ic = document.createElement('span');
  ic.className = 'ti';
  const svg = kind === 'err'
    ? '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>'
    : '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8.5l3.2 3.2L13 5"/></svg>';
  // SVG estático propio → DOMParser (sin innerHTML).
  const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
  ic.appendChild(doc.documentElement);
  const label = document.createElement('span');
  label.textContent = msg;
  t.appendChild(ic); t.appendChild(label);
  host.appendChild(t);
  setTimeout(() => {
    t.classList.add('leaving');
    t.addEventListener('animationend', () => t.remove(), { once: true });
  }, 2200);
}

// ─── Ambiente: esporas / polen a la deriva (aire quieto de jardín) ─────────────
export function spawnMotes() {
  const host = document.getElementById('motes');
  if (!host || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  const N = window.innerWidth < 600 ? 6 : 11;
  for (let i = 0; i < N; i++) {
    const m = document.createElement('span');
    m.className = 'mote';
    const size = (2 + Math.random() * 2.4).toFixed(1);
    const dur  = (28 + Math.random() * 34).toFixed(1);
    m.style.left = (Math.random() * 100).toFixed(2) + '%';
    m.style.bottom = '-12px';
    m.style.width = size + 'px';
    m.style.height = size + 'px';
    m.style.animationDuration = dur + 's';
    m.style.animationDelay = (-Math.random() * dur).toFixed(1) + 's';
    host.appendChild(m);
  }
}

// ─── Descargas ────────────────────────────────────────────────────────────────
export function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a   = document.createElement('a');
  a.href = url; a.download = filename;
  document.body.appendChild(a); a.click();
  setTimeout(() => { URL.revokeObjectURL(url); a.remove(); }, 500);
}
