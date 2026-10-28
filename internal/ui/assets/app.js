/* carta UI.
 *
 * A force-directed graph in about two hundred lines of canvas, rather than a
 * graph library. Three reasons: the binary stays small enough to live on a
 * stick, nothing is fetched at runtime so the tool works air-gapped, and the
 * drawing rules ARE the product — shape and stroke carry how well each device
 * is known, which is the one thing a generic graph library would flatten away.
 */

let survey = null;
let nodes = [];
let links = [];
let selected = null;
let view = { x: 0, y: 0, k: 1 };

// ?embedded: shown inside a host app that draws its own branded chrome.
if (new URLSearchParams(location.search).has('embedded')) {
  document.body.classList.add('embedded');
}

const canvas = document.getElementById('map');
const ctx = canvas.getContext('2d');

fetch('api/survey').then(r => r.json()).then(data => {
  survey = data;
  build();
  layout();
  draw();
  renderVantage();
  renderCoverage();
  renderFindings();
  renderList();
  // The vantage and coverage text just changed the sheet's peek height.
  // Re-measure here rather than trust a resize callback, which a page that is
  // not being drawn (a background tab) never gets.
  if (sheetMode) {
    measureSheet();
    setSheet(sheet.state, false);
    if (!userMoved) fit();
    draw();
  }
});

// Drift is optional: only a host that was asked to compare two surveys serves
// it, and the tab stays hidden everywhere else.
fetch('api/drift').then(r => r.ok ? r.json() : null).then(d => {
  if (!d) return;
  renderDrift(d);
  // ?tab=drift: the host just ran a comparison and the answer is the point.
  if (new URLSearchParams(location.search).get('tab') === 'drift') showTab('drift');
}).catch(() => {});

/* ---------- model → drawable ---------- */

function build() {
  const alive = (survey.entities || []).filter(e => best(e, 'alive') === 'yes');
  nodes = alive.map((e, i) => ({
    e,
    id: e.id,
    label: labelOf(e),
    sub: sublabelOf(e),
    kind: kindOf(e),
    // Seeded on a circle rather than at random: an identical survey should
    // produce an identical picture, or two screenshots taken a minute apart
    // look like different networks.
    x: Math.cos((i / alive.length) * Math.PI * 2) * 220,
    y: Math.sin((i / alive.length) * Math.PI * 2) * 220,
    vx: 0, vy: 0,
    r: 0,
  }));

  const index = new Map(nodes.map(n => [n.id, n]));
  links = (survey.edges || [])
    .filter(ed => ed.kind === 'gateway-of')
    .map(ed => ({ a: index.get(ed.from), b: index.get(ed.to), conf: ed.confidence }))
    .filter(l => l.a && l.b && l.a !== l.b);

  for (const n of nodes) n.r = n.kind === 'infra' ? 13 : 9;

  const prefix = (survey.vantage || {}).prefix || 'this network';
  canvas.setAttribute('aria-label',
    `Map of ${nodes.length} ${nodes.length === 1 ? 'device' : 'devices'} on ${prefix}. The All tab lists them.`);
}

function best(e, key) {
  let bv = '', bc = -1;
  for (const c of e.claims || []) {
    if (c.key !== key) continue;
    const rank = confRank(c.confidence);
    if (rank > bc) { bv = c.value; bc = rank; }
  }
  return bv;
}

function bestWithConf(e, key) {
  let bv = '', bc = -1, conf = '';
  for (const c of e.claims || []) {
    if (c.key !== key) continue;
    const rank = confRank(c.confidence);
    if (rank > bc) { bv = c.value; bc = rank; conf = c.confidence; }
  }
  return { value: bv, conf };
}

function confRank(c) {
  return { asserted: 4, observed: 3, inferred: 2, weak: 1, unknown: 0 }[c] ?? 0;
}

function ids(e, kind) {
  return (e.identities || []).filter(i => i.startsWith(kind + ':')).map(i => i.slice(kind.length + 1));
}

function labelOf(e) {
  const host = best(e, 'hostname') || best(e, 'label');
  if (host) return host;
  const ip = ids(e, 'ip')[0];
  return ip || e.id;
}

// sublabelOf is what a device is, with weak guesses marked as guesses.
function sublabelOf(e) {
  const cls = bestWithConf(e, 'class');
  const vendor = bestWithConf(e, 'vendor');
  if (cls.value && confRank(cls.conf) > confRank('weak')) return cls.value;
  if (cls.value) return cls.value + '?';
  if (vendor.value) return vendor.value + (confRank(vendor.conf) > confRank('inferred') ? '' : '?');
  return 'unidentified';
}

function kindOf(e) {
  const cls = bestWithConf(e, 'class');
  const host = best(e, 'hostname');
  if (cls.value === 'router / gateway' || cls.value === 'survey vantage') return 'infra';
  // A weak class is a guess, not an identification. "Personal device
  // (randomised MAC)" is a reasonable hunch about an address that told us
  // nothing, and drawing it with the same weight as a device that announced
  // its own model number is exactly the flattening this tool exists to avoid.
  const identified = cls.value && confRank(cls.conf) > confRank('weak');
  if (!host && !identified) return 'unknown';
  if (!host || !identified) return 'partial';
  return 'named';
}

/* ---------- layout ----------
 * Plain spring-and-repulsion, run to a fixed iteration count up front rather
 * than animated forever: a map that keeps drifting is a map you cannot point
 * at while someone photographs the screen.
 */
function layout() {
  const iterations = 400;
  for (let step = 0; step < iterations; step++) {
    const t = 1 - step / iterations;
    for (const n of nodes) {
      for (const m of nodes) {
        if (n === m) continue;
        let dx = n.x - m.x, dy = n.y - m.y;
        let d2 = dx * dx + dy * dy || 0.01;
        const f = 2400 / d2;
        n.vx += dx * f * 0.02;
        n.vy += dy * f * 0.02;
      }
      // Gentle pull to the origin keeps disconnected devices — the common case
      // on a flat network where everything hangs off one gateway — on screen.
      n.vx -= n.x * 0.002;
      n.vy -= n.y * 0.002;
    }
    for (const l of links) {
      const dx = l.b.x - l.a.x, dy = l.b.y - l.a.y;
      const d = Math.hypot(dx, dy) || 0.01;
      const target = 130;
      const f = (d - target) * 0.01;
      l.a.vx += dx / d * f; l.a.vy += dy / d * f;
      l.b.vx -= dx / d * f; l.b.vy -= dy / d * f;
    }
    for (const n of nodes) {
      n.x += n.vx * t; n.y += n.vy * t;
      n.vx *= 0.82; n.vy *= 0.82;
    }
  }
  fit();
}

// fit frames the whole graph in the canvas.
//
// It refuses to run against a zero-sized canvas. The page can load while its
// pane is hidden or still being laid out, and measuring then yields a scale of
// zero — a blank map with every node present and correct, which is the most
// confusing possible failure. fitted stays false until a real measurement
// happens, and draw() retries.
let fitted = false;
let userMoved = false;

function fit() {
  // In the sheet layout the panel's peek covers the bottom of the canvas, so
  // the map is framed in the part that is actually visible.
  const w = canvas.clientWidth, h = canvas.clientHeight - mapInset();
  if (!nodes.length || w < 2 || h < 2) return false;

  const xs = nodes.map(n => n.x), ys = nodes.map(n => n.y);
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minY = Math.min(...ys), maxY = Math.max(...ys);
  const k = Math.min(w / (maxX - minX + 180), h / (maxY - minY + 180), 1.6);
  if (!isFinite(k) || k <= 0) return false;

  view.k = k;
  view.x = w / 2 - ((minX + maxX) / 2) * k;
  view.y = h / 2 - ((minY + maxY) / 2) * k;
  fitted = true;
  return true;
}

/* ---------- drawing ---------- */

function draw() {
  if (!fitted) fit();
  const dpr = window.devicePixelRatio || 1;
  canvas.width = canvas.clientWidth * dpr;
  canvas.height = canvas.clientHeight * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, canvas.clientWidth, canvas.clientHeight);

  const css = getComputedStyle(document.documentElement);
  const colour = {
    named: css.getPropertyValue('--named').trim(),
    partial: css.getPropertyValue('--partial').trim(),
    unknown: css.getPropertyValue('--unknown').trim(),
    infra: css.getPropertyValue('--infra').trim(),
    rule: css.getPropertyValue('--rule').trim(),
    dim: css.getPropertyValue('--dim').trim(),
    fg: css.getPropertyValue('--fg').trim(),
  };
  // The canvas cannot read a CSS variable in ctx.font, so the stack is copied
  // out of the stylesheet: the map's labels use the same bundled face as the
  // rest of the page.
  const mono = css.getPropertyValue('--mono').trim();
  // Canvas text ignores the reader's text size; the page's root font size
  // carries it (a browser setting, or Android's font scale in the WebView), so
  // the labels follow it too.
  const zoom = Math.max(1, parseFloat(css.fontSize) / 16 || 1);

  ctx.save();
  ctx.translate(view.x, view.y);
  ctx.scale(view.k, view.k);

  for (const l of links) {
    ctx.beginPath();
    ctx.moveTo(l.a.x, l.a.y);
    ctx.lineTo(l.b.x, l.b.y);
    ctx.strokeStyle = colour.rule;
    ctx.lineWidth = 1;
    // An inferred relationship is drawn dashed. The gateway edges on a flat
    // network are inferred from one host's routing table, and the picture says
    // so rather than implying it was verified per device.
    ctx.setLineDash(confRank(l.conf) >= 3 ? [] : [3, 3]);
    ctx.stroke();
    ctx.setLineDash([]);
  }

  for (const n of nodes) {
    ctx.beginPath();
    ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2);
    if (n.kind === 'unknown') {
      ctx.setLineDash([3, 3]);
      ctx.strokeStyle = colour.unknown;
      ctx.lineWidth = 1.5;
      ctx.stroke();
      ctx.setLineDash([]);
    } else if (n.kind === 'partial') {
      ctx.strokeStyle = colour.partial;
      ctx.lineWidth = 1.5;
      ctx.stroke();
    } else {
      ctx.fillStyle = n.kind === 'infra' ? colour.infra : colour.named;
      ctx.fill();
    }
    if (n === selected) {
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.r + 5, 0, Math.PI * 2);
      ctx.strokeStyle = colour.fg;
      ctx.lineWidth = 1;
      ctx.stroke();
    }

    ctx.font = `${11 * zoom}px ${mono}`;
    ctx.fillStyle = n.kind === 'unknown' ? colour.dim : colour.fg;
    ctx.textAlign = 'center';
    ctx.fillText(n.label, n.x, n.y + n.r + 13 * zoom);

    ctx.font = `${9 * zoom}px ${mono}`;
    ctx.fillStyle = colour.dim;
    ctx.fillText(n.sub, n.x, n.y + n.r + 24 * zoom);
  }
  ctx.restore();
}

/* ---------- interaction ---------- */

// Pointer events rather than mouse events, so the same code handles a mouse,
// a finger and a pen. Two fingers on the canvas pinch-zoom.
let dragging = null, panning = null;
const pointers = new Map();
let pinch = null;

canvas.addEventListener('pointerdown', ev => {
  // Capture keeps a drag alive when the finger leaves the canvas; it throws for
  // a pointer the browser no longer tracks, which must not cost the gesture.
  try { canvas.setPointerCapture(ev.pointerId); } catch (_) {}
  pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (pointers.size === 2) {
    dragging = null; panning = null;
    pinch = { d: pinchDistance(), k: view.k };
    userMoved = true;
    return;
  }
  const p = toModel(ev);
  // A fingertip is far less precise than a cursor, so a touch hits wider.
  const slop = ev.pointerType === 'touch' ? 18 : 6;
  const hit = nodes.find(n => Math.hypot(n.x - p.x, n.y - p.y) < n.r + slop);
  if (hit) {
    dragging = hit;
    select(hit);
  } else {
    panning = { x: ev.clientX - view.x, y: ev.clientY - view.y };
    userMoved = true;
  }
});

canvas.addEventListener('pointermove', ev => {
  if (!pointers.has(ev.pointerId)) return;
  pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (pinch && pointers.size === 2) {
    const [a, b] = [...pointers.values()];
    const rect = canvas.getBoundingClientRect();
    zoomAt((a.x + b.x) / 2 - rect.left, (a.y + b.y) / 2 - rect.top, pinch.k * pinchDistance() / pinch.d / view.k);
  } else if (dragging) {
    const p = toModel(ev);
    dragging.x = p.x; dragging.y = p.y;
    draw();
  } else if (panning) {
    view.x = ev.clientX - panning.x;
    view.y = ev.clientY - panning.y;
    draw();
  }
});

function endPointer(ev) {
  pointers.delete(ev.pointerId);
  if (pointers.size < 2) pinch = null;
  if (pointers.size === 0) { dragging = null; panning = null; }
}
canvas.addEventListener('pointerup', endPointer);
canvas.addEventListener('pointercancel', endPointer);

function pinchDistance() {
  const [a, b] = [...pointers.values()];
  return Math.max(Math.hypot(a.x - b.x, a.y - b.y), 1);
}

function zoomAt(mx, my, factor) {
  view.x = mx - (mx - view.x) * factor;
  view.y = my - (my - view.y) * factor;
  view.k *= factor;
  draw();
}

canvas.addEventListener('wheel', ev => {
  ev.preventDefault();
  userMoved = true;
  const rect = canvas.getBoundingClientRect();
  zoomAt(ev.clientX - rect.left, ev.clientY - rect.top, ev.deltaY < 0 ? 1.1 : 0.9);
}, { passive: false });

// A canvas that gains its size after load — a hidden pane being shown, a split
// being dragged — has to be re-framed, but only while the view is still the one
// carta chose. Once someone has panned or zoomed, the framing is theirs.
new ResizeObserver(() => {
  if (!userMoved) fit();
  draw();
}).observe(canvas);

// The bundled face loads after the first paint (font-display: swap), and a
// canvas does not repaint itself when it arrives.
// It also reflows the sheet's head, so the peek height is measured again.
if (document.fonts) document.fonts.addEventListener('loadingdone', () => {
  if (sheetMode) { measureSheet(); setSheet(sheet.state, false); }
  draw();
});

window.addEventListener('resize', () => {
  if (!userMoved) fit();
  draw();
});

function toModel(ev) {
  const rect = canvas.getBoundingClientRect();
  return {
    x: (ev.clientX - rect.left - view.x) / view.k,
    y: (ev.clientY - rect.top - view.y) / view.k,
  };
}

// focusPanel: the choice came from the keyboard or the list, so focus follows
// it to the device's details instead of staying on a row that is now hidden.
function select(node, focusPanel) {
  selected = node;
  renderDevice(node.e);
  showTab('device');
  if (sheetMode && sheet.state === 'peek') setSheet('half', true);
  keepVisible(node);
  draw();
  if (focusPanel) document.getElementById('device').focus();
}

/* ---------- layout: compact and sheet ----------

   compact (?embedded, or a narrow window): the vantage line goes to the
   panel's head and the legend behind a button on the map, so the header no
   longer eats the map's height.

   sheet (a narrow window): the map is full-bleed and the panel slides over it,
   snapping to peek (handle, head, tabs), half or full. Only the grip — the
   handle and the head — drags the sheet: the canvas keeps pan and pinch, and
   the panel's body scrolls.

   The elements are moved, not duplicated, so there is one vantage line and one
   legend whichever layout is showing. */

const narrow = matchMedia('(max-width: 760px)');
let compact = false, sheetMode = false;
const panel = document.getElementById('panel');
const grip = document.getElementById('grip');
const handle = document.getElementById('handle');
const sheet = { state: 'peek', heights: { peek: 0, half: 0, full: 0 } };
const SHEET_STATES = ['peek', 'half', 'full'];

function arrange() {
  compact = document.body.classList.contains('embedded') || narrow.matches;
  sheetMode = narrow.matches;
  document.body.classList.toggle('compact', compact);
  document.body.classList.toggle('sheet', sheetMode);

  const bar = document.querySelector('.bar');
  const head = document.getElementById('panelhead');
  const vantage = document.getElementById('vantage');
  const legend = document.querySelector('.legend');
  const coverage = document.getElementById('coverage');
  if (compact) {
    head.prepend(vantage);
    document.getElementById('legendcard').append(legend);
  } else {
    bar.querySelector('.brand').after(vantage);
    vantage.after(legend);
    showLegend(false);
  }
  if (sheetMode) head.append(coverage);
  else document.querySelector('.graph').append(coverage);

  if (sheetMode) {
    measureSheet();
    setSheet(sheet.state, false);
  } else {
    panel.style.transform = '';
    panel.style.height = '';
  }
  if (!userMoved) fitted = false;
  draw();
}

function measureSheet() {
  const h = document.querySelector('main').clientHeight;
  const peek = grip.offsetHeight + document.querySelector('.tabs').offsetHeight;
  sheet.heights = {
    peek,
    half: Math.max(peek, Math.round(h * 0.5)),
    full: Math.max(peek, h - 16),
  };
  panel.style.height = sheet.heights.full + 'px';
}

function setSheet(state, animate) {
  sheet.state = state;
  panel.classList.toggle('animating', !!animate);
  panel.style.transform = 'translateY(' + (sheet.heights.full - sheet.heights[state]) + 'px)';
  handle.setAttribute('aria-expanded', state !== 'peek');
  handle.setAttribute('aria-label', 'Details panel, ' +
    (state === 'peek' ? 'collapsed' : state === 'half' ? 'half open' : 'open'));
}

function mapInset() {
  return sheetMode ? sheet.heights.peek : 0;
}

// A selected node must not end up under the sheet that just rose to show it.
function keepVisible(node) {
  if (!sheetMode) return;
  const visible = canvas.clientHeight - sheet.heights[sheet.state];
  const y = view.y + node.y * view.k;
  if (y > visible - 40 || y < 20) {
    view.y += visible / 2 - y;
    userMoved = true;
  }
}

let sheetDrag = null, sheetJustDragged = false;

grip.addEventListener('pointerdown', ev => {
  if (!sheetMode) return;
  const offset = sheet.heights.full - sheet.heights[sheet.state];
  sheetDrag = { y0: ev.clientY, offset0: offset, offset, moved: false, lastY: ev.clientY, lastT: performance.now(), v: 0 };
  try { grip.setPointerCapture(ev.pointerId); } catch (_) {}
});

grip.addEventListener('pointermove', ev => {
  const d = sheetDrag;
  if (!d) return;
  const dy = ev.clientY - d.y0;
  // A few pixels of wobble is a tap on the handle, not a drag.
  if (!d.moved && Math.abs(dy) < 6) return;
  d.moved = true;
  panel.classList.remove('animating');
  const max = sheet.heights.full - sheet.heights.peek;
  d.offset = Math.min(max, Math.max(0, d.offset0 + dy));
  panel.style.transform = 'translateY(' + d.offset + 'px)';
  const now = performance.now();
  if (now > d.lastT) d.v = (ev.clientY - d.lastY) / (now - d.lastT);
  d.lastY = ev.clientY;
  d.lastT = now;
});

// On release the sheet settles at the nearest height, or — after a flick —
// at the next height in the flick's direction from where the finger let go.
function endSheetDrag() {
  const d = sheetDrag;
  sheetDrag = null;
  if (!d || !d.moved) return;
  sheetJustDragged = true;
  setTimeout(() => { sheetJustDragged = false; }, 0);
  const shown = sheet.heights.full - d.offset;
  let target = nearestState(shown);
  if (d.v < -0.5) target = SHEET_STATES.find(st => sheet.heights[st] > shown) || 'full';
  else if (d.v > 0.5) target = [...SHEET_STATES].reverse().find(st => sheet.heights[st] < shown) || 'peek';
  setSheet(target, true);
}
grip.addEventListener('pointerup', endSheetDrag);
grip.addEventListener('pointercancel', endSheetDrag);

function nearestState(shown) {
  return SHEET_STATES.reduce((a, b) =>
    Math.abs(sheet.heights[b] - shown) < Math.abs(sheet.heights[a] - shown) ? b : a);
}

// The handle is a button: a tap or Enter cycles the heights, arrows step them.
handle.addEventListener('click', () => {
  if (sheetJustDragged) return;
  setSheet(SHEET_STATES[(SHEET_STATES.indexOf(sheet.state) + 1) % 3], true);
});
handle.addEventListener('keydown', ev => {
  const i = SHEET_STATES.indexOf(sheet.state);
  if (ev.key === 'ArrowUp') { ev.preventDefault(); setSheet(SHEET_STATES[Math.min(2, i + 1)], true); }
  if (ev.key === 'ArrowDown') { ev.preventDefault(); setSheet(SHEET_STATES[Math.max(0, i - 1)], true); }
});

const legendBtn = document.getElementById('legendbtn');
function showLegend(on) {
  document.getElementById('legendcard').classList.toggle('hidden', !on);
  legendBtn.setAttribute('aria-expanded', on);
}
legendBtn.addEventListener('click', () => showLegend(legendBtn.getAttribute('aria-expanded') !== 'true'));

// For a host app's Back gesture: fold whatever is open, and say whether
// anything was. The host calls this; nothing here calls the host.
window.carta = {
  collapse() {
    if (legendBtn.getAttribute('aria-expanded') === 'true') { showLegend(false); return true; }
    if (sheetMode && sheet.state !== 'peek') { setSheet('peek', true); return true; }
    return false;
  },
};

narrow.addEventListener('change', arrange);
// The peek is as tall as the grip, which grows when the survey's vantage and
// coverage text arrive; the other heights follow the page.
const sheetSizer = new ResizeObserver(() => {
  if (!sheetMode) return;
  measureSheet();
  setSheet(sheet.state, false);
});
sheetSizer.observe(document.querySelector('main'));
sheetSizer.observe(grip);
arrange();

/* ---------- panels ---------- */

function renderVantage() {
  const v = survey.vantage || {};
  // Three facts, kept apart by spacing rather than a separator glyph, so they
  // can also wrap one per line on a narrow screen.
  const el = document.getElementById('vantage');
  el.textContent = '';
  for (const part of [
    v.prefix || '?',
    `from ${v.ip || '?'} on ${v.interface || '?'}`,
    new Date(survey.started_at).toLocaleString(),
  ]) {
    const span = document.createElement('span');
    span.className = 'vpart';
    span.textContent = part;
    el.append(span);
  }
}

function renderCoverage() {
  const el = document.getElementById('coverage');
  const c = (survey.coverage || [])[0];
  if (!c) { el.textContent = ''; return; }
  const total = c.addrs_total || 1;
  const swept = Math.min(c.addrs_swept ?? total, total);
  const responded = Math.min(c.responded || 0, swept);
  // Widths are set through the DOM, not a style attribute, so the page can run
  // under a Content-Security-Policy that forbids inline styles.
  el.innerHTML = `
    <div class="covbar"><i class="responded"></i><i class="silent"></i><i class="unswept"></i></div>
    <span class="covtext"></span>`;
  const bar = el.querySelectorAll('.covbar i');
  bar[0].style.width = (responded / total * 100) + '%';
  bar[1].style.width = ((swept - responded) / total * 100) + '%';
  bar[2].style.width = ((total - swept) / total * 100) + '%';
  // A survey cut short has addresses it never reached, and those are a third
  // state: not answered and not asked.
  el.querySelector('.covtext').textContent =
    `${responded} of ${total} addresses in ${c.prefix} answered. ` +
    `The hatched ${swept - responded} did not — unknown, not empty.` +
    (swept < total ? ` The open ${total - swept} were never reached: the survey was cut short.` : '');
}

function renderFindings() {
  const el = document.getElementById('findings');
  const fs = survey.findings || [];
  if (!fs.length) { el.innerHTML = '<p class="hint">Nothing worth reporting.</p>'; return; }
  el.innerHTML = fs.map(f => `
    <div class="finding ${esc(f.severity)}">
      <h3>${esc(f.title)}</h3>
      <span class="conf ${esc(f.confidence)}">${esc(f.confidence)}</span>
      <p>${esc(f.detail)}</p>
    </div>`).join('');
}

function renderList() {
  const el = document.getElementById('list');
  el.innerHTML = '<input class="searchbox" id="q" aria-label="Filter devices" placeholder="filter by name, address, vendor or class" />'
    + '<div id="rows"></div>';
  const rows = document.getElementById('rows');
  const paint = (filter) => {
    const f = (filter || '').toLowerCase();
    rows.innerHTML = nodes
      .filter(n => !f || rowText(n.e).toLowerCase().includes(f))
      .map(n => `<div class="row" role="button" tabindex="0" data-id="${esc(n.id)}">
          <span class="label">${esc(n.label)}</span>
          <span class="meta">${esc(ids(n.e, 'ip').join(' '))}<br>${esc(best(n.e, 'class') || best(n.e, 'vendor') || 'unidentified')}</span>
        </div>`).join('');
    rows.querySelectorAll('.row').forEach(r => {
      const open = () => {
        const n = nodes.find(x => x.id === r.dataset.id);
        if (n) select(n, true);
      };
      r.onclick = open;
      r.onkeydown = ev => {
        if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); open(); }
      };
    });
  };
  paint('');
  document.getElementById('q').addEventListener('input', e => paint(e.target.value));
}

function rowText(e) {
  return [labelOf(e), ids(e, 'ip').join(' '), ids(e, 'mac').join(' '), best(e, 'vendor'), best(e, 'class')].join(' ');
}

function renderDevice(e) {
  const el = document.getElementById('device');
  const host = bestWithConf(e, 'hostname');
  const vendor = bestWithConf(e, 'vendor');
  const cls = bestWithConf(e, 'class');
  const model = bestWithConf(e, 'model');
  const os = bestWithConf(e, 'os');

  const idRow = (k, v) => v.length ? `<tr><td class="k">${k}</td><td class="mono">${esc(v.join(', '))}</td></tr>` : '';
  const claimRow = (k, c) => c.value
    ? `<tr><td class="k">${k}</td><td>${esc(c.value)} <span class="conf ${esc(c.conf)}">${esc(c.conf)}</span></td></tr>`
    : `<tr><td class="k">${k}</td><td class="hint">not learned</td></tr>`;

  const services = (e.claims || []).filter(c => c.key === 'service');
  const ports = (e.claims || []).filter(c => c.key === 'port');
  const notes = (e.claims || []).filter(c => c.key === 'note');

  el.innerHTML = `
    <h2 class="devname">${esc(labelOf(e))}</h2>
    <div class="devsub">${esc(ids(e, 'ip').join(', '))}</div>

    <table>
      ${claimRow('Name', host)}
      ${claimRow('Vendor', vendor)}
      ${claimRow('Class', cls)}
      ${claimRow('Model', model)}
      ${claimRow('OS', os)}
      ${idRow('MAC', ids(e, 'mac'))}
    </table>

    ${services.length ? `<h3>Services</h3><table>${services.map(c =>
      `<tr><td>${esc(c.value)}</td><td><span class="conf ${esc(c.confidence)}">${esc(c.confidence)}</span></td></tr>`).join('')}</table>` : ''}

    ${ports.length ? `<p class="evidence">Open TCP: ${ports.map(c => esc(c.value)).join(', ')}</p>` : ''}

    ${notes.length ? `<h3>Notes</h3>${notes.map(c => `<p class="evidence">${esc(c.value)}</p>`).join('')}` : ''}

    <h3>Evidence</h3>
    <table>
      <tr><th>Claim</th><th>Source</th></tr>
      ${(e.claims || []).map(c => `<tr>
        <td>${esc(c.key)} = ${esc(c.value)}</td>
        <td><span class="conf ${esc(c.confidence)}">${esc(c.confidence)}</span>
            <div class="evidence">${esc(c.source.method)} — ${esc(c.source.evidence || '')}</div></td>
      </tr>`).join('')}
    </table>

    ${(e.conflicts || []).length ? `<h3>Disagreements</h3>${e.conflicts.map(c =>
      `<p class="evidence">${esc(c)}</p>`).join('')}` : ''}

    ${(e.merges || []).length ? `<h3>Merged from</h3>${e.merges.map(m =>
      `<p class="evidence">${esc(m)}</p>`).join('')}` : ''}
  `;
}

document.querySelectorAll('.tabs button').forEach(b => {
  b.onclick = () => {
    showTab(b.dataset.tab);
    // Choosing a tab on a sheet that only peeks means wanting to read it.
    if (sheetMode && sheet.state === 'peek') setSheet('half', true);
  };
});

// The tabs are one stop in the keyboard order (the selected one); arrows move
// between them, as a tablist does everywhere else.
document.querySelector('.tabs').addEventListener('keydown', ev => {
  const tabs = [...document.querySelectorAll('.tabs button:not(.hidden)')];
  const i = tabs.indexOf(document.activeElement);
  if (i < 0) return;
  const next = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[ev.key];
  if (next === undefined) return;
  ev.preventDefault();
  const t = tabs[(next + tabs.length) % tabs.length];
  showTab(t.dataset.tab);
  t.focus();
});

document.getElementById('skip').addEventListener('click', ev => {
  ev.preventDefault();
  if (sheetMode) setSheet('full', true);
  showTab('list');
  document.getElementById('q')?.focus();
});

/* Drift arrives already computed: matching devices across time is the model's
   job, not the page's. Each change shows how it was matched, because a match on
   an address alone may be two different devices that swapped leases. */
function renderDrift(d) {
  const el = document.getElementById('drift');
  const caveats = (d.caveats || []).map(c => `<p class="caveat">${esc(c)}</p>`).join('');
  const groups = [
    ['appeared', 'Appeared'], ['vanished', 'Vanished'], ['moved', 'Moved'], ['mutated', 'Changed'],
  ];
  const changes = d.changes || [];
  const body = groups.map(([kind, title]) => {
    const rows = changes.filter(c => c.kind === kind);
    if (!rows.length) return '';
    return `<h3>${title} (${rows.length})</h3>` + rows.map(c => `
      <div class="change">
        <span class="label">${esc(c.entity)}</span>
        ${c.matched_by ? `<span class="conf ${esc(c.match)}">by ${esc(c.matched_by)}, ${esc(c.match)}</span>` : ''}
        <div class="evidence">${esc(c.was ? `${c.was} → ${c.now || ''}` : (c.now || ''))}</div>
        ${(c.details || []).map(x => `<div class="evidence">${esc(x)}</div>`).join('')}
      </div>`).join('');
  }).join('');
  el.innerHTML = `
    <p class="mono">${esc(d.before_id)} → ${esc(d.after_id)}</p>
    ${d.comparable ? '' : '<p class="caveat strong">These surveys are not of the same network. Read the changes below as a difference between two networks.</p>'}
    ${caveats}
    <p>${esc(d.summary || '')}</p>
    ${body}`;
  document.querySelector('.tabs button[data-tab="drift"]').classList.remove('hidden');
}

function showTab(name) {
  document.querySelectorAll('.tabs button').forEach(b => {
    const on = b.dataset.tab === name;
    b.classList.toggle('on', on);
    b.setAttribute('aria-selected', on);
    b.tabIndex = on ? 0 : -1;
  });
  document.querySelectorAll('.tabbody').forEach(d => d.classList.toggle('hidden', d.id !== name));
}

/* Everything rendered here came off an untrusted network: a device's own
   hostname, banner or page title is attacker-controlled text. It is escaped
   once, here, and never inserted as HTML anywhere else. */
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
