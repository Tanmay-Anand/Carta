// Carta's survey view, design D (docs/DESIGN_SYSTEM_CARTA.md). The same page
// is `carta ui` in a browser and the survey view inside the Android app.
//
// Hooks the Android app relies on, which must not change name or meaning:
//   ?embedded          the app draws the top and bottom bars; the page does not
//   ?tab=drift         open on the Drift tab (after a comparison); ?tab=findings
//                      and ?tab=devices open those tabs
//   showTab(name)      the app opens a tab (the snackbar's View)
//   carta.collapse()   Back: lower the sheet; true if anything was open
'use strict';

const $ = id => document.getElementById(id);
const params = new URLSearchParams(location.search);
if (params.has('embedded')) document.body.classList.add('embedded');

let survey = null, drift = null;
const state = { selected: null, note: '', tab: 'device', filter: '', sheet: 'peek', hovered: null };

// Everything here came off an untrusted network: hostnames, banners, mDNS
// names. Every value goes through esc() before it touches innerHTML.
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

/* ---------- reading claims ---------- */

const RANK = { asserted: 4, observed: 3, inferred: 2, weak: 1, unknown: 0 };
const rank = c => RANK[c] ?? 0;

function bestClaim(e, key) {
  let best = null;
  for (const c of e.claims || []) {
    if (c.key === key && c.value && (!best || rank(c.confidence) > rank(best.confidence))) best = c;
  }
  return best;
}
const best = (e, key) => (bestClaim(e, key) || {}).value || '';
const ids = (e, kind) => (e.identities || []).filter(i => i.startsWith(kind + ':')).map(i => i.slice(kind.length + 1));
const isAlive = e => best(e, 'alive') === 'yes';
const labelOf = e => best(e, 'hostname') || best(e, 'label') || ids(e, 'ip')[0] || ids(e, 'mac')[0] || e.id;
const devices = () => (survey?.entities || []).filter(isAlive);

// sublabelOf is what a device is, with guesses marked as guesses.
function sublabelOf(e) {
  const cls = bestClaim(e, 'class'), vendor = bestClaim(e, 'vendor');
  if (cls && rank(cls.confidence) > rank('weak')) return cls.value;
  if (cls) return cls.value + '?';
  if (vendor) return vendor.value + (rank(vendor.confidence) > rank('inferred') ? '' : '?');
  return 'unidentified';
}

// kindOf is how a device is drawn. A weak class is a guess, not an
// identification: drawing it with the weight of a device that announced its
// own model is the flattening this tool exists to avoid.
function kindOf(e) {
  const cls = bestClaim(e, 'class');
  if (cls && (cls.value === 'router / gateway' || cls.value === 'survey vantage')) return 'infra';
  const host = best(e, 'hostname');
  const identified = cls && rank(cls.confidence) > rank('weak');
  if (!host && !identified) return 'unknown';
  if (!host || !identified) return 'partial';
  return 'named';
}

/* ---------- loading ---------- */

fetch('api/survey').then(r => r.json()).then(s => {
  survey = s;
  // The gateway starts selected, so the panel has something to show: the
  // device classed as the router, or else the one at the gateway's address.
  const gw = devices().find(e => kindOf(e) === 'infra' && labelOf(e) !== s.vantage?.host)
    || devices().find(e => ids(e, 'ip').includes(s.vantage?.gateway));
  state.selected = gw || null;
  build();
  arrange();
  const tab = params.get('tab');
  if (tab === 'findings' || tab === 'devices' || (tab === 'drift' && drift)) showTab(tab);
}).catch(() => { $('count').textContent = 'No survey'; });

// The Drift tab exists only when there is a comparison to show (the Android
// app serves one after Compare); `carta ui` has none.
fetch('api/drift').then(r => (r.ok ? r.json() : null)).then(d => {
  if (!d) return;
  drift = d;
  $('tab-drift').classList.remove('hidden');
  $('nDrift').textContent = (d.changes || []).length;
  if (survey && params.get('tab') === 'drift') showTab('drift');
}).catch(() => {});

/* ---------- summary ---------- */

const when = iso => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
const idDate = id => { // a survey id is its UTC start, 20261116T091400Z
  const m = /^(\d{4})(\d\d)(\d\d)T(\d\d)(\d\d)(\d\d)Z$/.exec(id || '');
  return m ? when(`${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}Z`) : id;
};
const cov = () => (survey.coverage || [])[0];
const quiet = () => /quiet survey/.test((cov()?.notes || []).join(' '));

function stageName(id) {
  return { orient: 'orienting', listen: 'listening', sweep: 'sweeping', probe: 'probing services',
    nbns: 'asking NetBIOS', rdns: 'reverse DNS', resolve: 'resolving' }[id] || id;
}

function renderSummary() {
  const v = survey.vantage || {}, c = cov();
  const n = devices().length;
  $('count').textContent = `${n} ${n === 1 ? 'device' : 'devices'}`;
  $('prefix').textContent = 'on ' + (v.prefix || 'this network');
  let took = '';
  if (c?.started_at && c?.ended_at) took = Math.round((new Date(c.ended_at) - new Date(c.started_at)) / 1000) + ' s';
  const facts = [['from', `${v.ip || '?'} on ${v.interface || '?'}`], ['started', when(survey.started_at)], ['took', took || 'unknown'], ['gateway', v.gateway || 'unknown']];
  $('facts').innerHTML = facts.map(([k, val]) => `<dt>${esc(k)}</dt><dd>${esc(val)}</dd>`).join('');
  $('factline').innerHTML = `from <b>${esc(v.ip || '?')}</b> on ${esc(v.interface || '?')}, ${esc(new Date(survey.started_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }))}` +
    (took ? `, took <b>${esc(took)}</b>` : '');

  // One dot per host address in the prefix, in address order.
  const dots = [];
  let ans = 0, sil = 0, unr = 0;
  const m = /^(\d+)\.(\d+)\.(\d+)\.(\d+)\/\d+$/.exec(c?.prefix || '');
  if (c && m) {
    const base = ((+m[1] << 24) >>> 0) + (+m[2] << 16) + (+m[3] << 8) + +m[4];
    const heard = new Set(devices().flatMap(e => ids(e, 'ip')));
    for (let i = 0; i < c.addrs_total; i++) {
      const a = base + i + 1;
      const ip = [a >>> 24, (a >>> 16) & 255, (a >>> 8) & 255, a & 255].join('.');
      if (heard.has(ip)) { dots.push('a'); ans++; }
      else if (i < c.addrs_swept) { dots.push(''); sil++; }
      else { dots.push('u'); unr++; }
    }
  }
  $('matrix').innerHTML = dots.map(d => `<i class="${d}"></i>`).join('');
  $('matrix').classList.toggle('wide', dots.length > 256);
  $('matrix').setAttribute('aria-label', `${ans} addresses answered, ${sil} were asked and stayed silent, ${unr} were never asked.`);
  $('counts').innerHTML = `<span><b>${ans}</b>answered</span><span><b>${sil}</b>silent</span><span><b>${unr}</b>unreached</span>`;
  $('covnote').textContent = quiet()
    ? 'A quiet survey asks nothing, so every address it did not hear is unreached.'
    : 'One dot per address. Silent means it did not answer: unknown, not empty.';

  const cut = survey.meta?.cut_short;
  $('status').innerHTML = cut
    ? `<span>Stopped during ${esc(stageName(cut))}</span><span class="tag">Stopped early</span>`
    : `<span>${quiet() ? 'Quiet survey: nothing was sent' : 'Survey complete'}</span>`;
  if (cut) $('factline').insertAdjacentHTML('beforeend', ' <span class="tag">Stopped early</span>');
}

/* ---------- map ---------- */

const svg = $('map'), NS = 'http://www.w3.org/2000/svg';
let nodes = [], links = [], view = { k: 1, x: 0, y: 0 }, fitted = false, userMoved = false;

function build() {
  renderSummary();
  const alive = devices();
  nodes = alive.map((e, i) => {
    // Seeded on a circle, not at random: the same survey must give the same
    // picture, or two screenshots look like two networks.
    const a = (i / alive.length) * Math.PI * 2;
    return { e, id: e.id, label: labelOf(e), kind: kindOf(e), x: Math.cos(a) * 220, y: Math.sin(a) * 220, vx: 0, vy: 0 };
  });
  const index = new Map(nodes.map(n => [n.id, n]));
  links = (survey.edges || []).filter(ed => ed.kind === 'gateway-of')
    .map(ed => ({ a: index.get(ed.from), b: index.get(ed.to) })).filter(l => l.a && l.b && l.a !== l.b);
  layout();
  svg.setAttribute('aria-label', `Map of ${alive.length} ${alive.length === 1 ? 'device' : 'devices'} on ${survey.vantage?.prefix || 'this network'}. The Devices tab lists them.`);
  $('nDevices').textContent = alive.length;
  const fs = survey.findings || [];
  $('nFindings').textContent = fs.length;
  $('nFindings').classList.toggle('risk', fs.some(f => f.severity === 'risk'));
  render();
}

// Spring and repulsion, run to a fixed count up front rather than animated:
// a map that keeps drifting cannot be pointed at.
function layout() {
  for (let s = 0; s < 400; s++) {
    const t = 1 - s / 400;
    for (const n of nodes) {
      for (const o of nodes) {
        if (n === o) continue;
        const dx = n.x - o.x, dy = n.y - o.y, d2 = dx * dx + dy * dy || 0.01, f = 2400 / d2;
        n.vx += dx * f * 0.02; n.vy += dy * f * 0.02;
      }
      n.vx -= n.x * 0.002; n.vy -= n.y * 0.002;
    }
    for (const l of links) {
      const dx = l.b.x - l.a.x, dy = l.b.y - l.a.y, d = Math.hypot(dx, dy) || 0.01, f = (d - 130) * 0.01;
      l.a.vx += dx / d * f; l.a.vy += dy / d * f; l.b.vx -= dx / d * f; l.b.vy -= dy / d * f;
    }
    for (const n of nodes) { n.x += n.vx * t; n.y += n.vy * t; n.vx *= 0.82; n.vy *= 0.82; }
  }
}

// The part of the map not covered by the sheet, in the map's own pixels. It
// uses the height the sheet is going to, not the height it has now: in the
// middle of its slide the sheet is neither, and framing the map against it
// would leave devices under the sheet once it settles.
function visibleHeight() {
  const h = svg.clientHeight;
  return document.body.classList.contains('sheet') ? h - sheetHeights()[state.sheet] : h;
}

// fit frames the map in what is visible, leaving room on the right for labels.
function fit() {
  const w = svg.clientWidth, h = visibleHeight();
  if (!nodes.length || w < 2 || h < 2) return false;
  const narrow = document.body.classList.contains('sheet');
  const [l, r, t, b] = narrow ? [26, 40, 22, 22] : [60, 190, 70, 80];
  const xs = nodes.map(n => n.x), ys = nodes.map(n => n.y);
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
  const k = Math.min((w - l - r) / Math.max(maxX - minX, 1), (h - t - b) / Math.max(maxY - minY, 1), 1.8);
  if (!(k > 0 && isFinite(k))) return false;
  view.k = k;
  view.x = l + ((w - l - r) - (maxX - minX) * k) / 2 - minX * k;
  view.y = t + ((h - t - b) - (maxY - minY) * k) / 2 - minY * k;
  fitted = true;
  return true;
}

const at = n => [n.x * view.k + view.x, n.y * view.k + view.y];
const css = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
// SVG text ignores the reader's text size; the page's root font size carries
// it (a browser setting, or Android's font scale in the app), so map labels
// are scaled by it to follow.
const labelSize = () => 12 * Math.max(1, parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1);
const measure = (() => {
  const ctx = document.createElement('canvas').getContext('2d');
  return s => { ctx.font = `${labelSize()}px ${css('--sans')}`; return ctx.measureText(s).width; };
})();

function el(tag, attrs, parent) {
  const e = document.createElementNS(NS, tag);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  (parent || svg).append(e);
  return e;
}

function draw() {
  if (!fitted && !fit()) return;
  svg.textContent = '';
  const W = svg.clientWidth, H = visibleHeight();
  const sel = state.selected && nodes.find(n => n.e === state.selected);
  for (const l of links) {
    const [x1, y1] = at(l.a), [x2, y2] = at(l.b);
    el('line', { x1, y1, x2, y2, stroke: css('--thread'), 'stroke-width': 1 });
  }
  const taken = [];
  for (const n of nodes) {
    const [x, y] = at(n), r = n.kind === 'infra' ? 7 : 4.5, isSel = n === sel;
    n.r = r;
    taken.push([x - r - 3, y - r - 3, x + r + 3, y + r + 3]);
    const g = el('g', { class: 'dev', 'data-id': n.id });
    el('circle', { cx: x, cy: y, r: 16, fill: 'transparent' }, g); // a wider target than the dot
    const col = isSel ? css('--red') : css('--fg');
    if (isSel) el('circle', { cx: x, cy: y, r: r + 9, fill: 'none', stroke: css('--red'), 'stroke-width': 1 }, g);
    if (n.kind === 'unknown') el('circle', { cx: x, cy: y, r, fill: 'none', stroke: isSel ? col : css('--amber'), 'stroke-width': 1.3, 'stroke-dasharray': '2 2' }, g);
    else if (n.kind === 'partial') el('circle', { cx: x, cy: y, r, fill: 'none', stroke: col, 'stroke-width': 1.3 }, g);
    else el('circle', { cx: x, cy: y, r, fill: col }, g);
  }
  // Labels: right of the dot, else left, below or above, slid inside the map.
  // One that collides everywhere shows only when hovered or selected, so text
  // never prints over text.
  const order = [sel, state.hovered].filter(Boolean);
  for (const n of nodes) if (!order.includes(n)) order.push(n);
  for (const n of order) {
    const [x, y] = at(n), w = measure(n.label), h = labelSize() + 2, must = n === sel || n === state.hovered;
    const gap = n.r + (n === sel ? 14 : 8);
    const cx = Math.min(Math.max(x - w / 2, 6), W - 6 - w);
    const cands = [[x + gap, y], [x - gap - w, y], [cx, y + gap + 8], [cx, y - gap - 2]];
    for (let i = 0; i < cands.length; i++) {
      const [lx, ly] = cands[i], box = [lx - 2, ly - h / 2 - 1, lx + w + 2, ly + h / 2 + 1];
      const inside = box[0] >= 4 && box[2] <= W - 4 && box[1] >= 4 && box[3] <= H - 4;
      const clear = !taken.some(o => box[0] < o[2] && o[0] < box[2] && box[1] < o[3] && o[1] < box[3]);
      if ((inside && clear) || (must && i === cands.length - 1)) {
        const t = el('text', { x: lx, y: ly + labelSize() / 3, fill: must ? css('--fg') : css('--dim'), 'font-size': labelSize() });
        t.textContent = n.label; // textContent: a device's name is untrusted
        taken.push(box);
        break;
      }
    }
  }
}

function hit(px, py) {
  for (let i = nodes.length - 1; i >= 0; i--) {
    const [x, y] = at(nodes[i]);
    if (Math.hypot(x - px, y - py) < (nodes[i].r || 5) + 12) return nodes[i];
  }
  return null;
}

// One finger or the mouse pans; two fingers pinch; a tap without movement
// selects; the wheel zooms about the pointer.
const pts = new Map();
let pan = null, pinch = null;
const local = ev => { const b = svg.getBoundingClientRect(); return [ev.clientX - b.left, ev.clientY - b.top]; };
svg.addEventListener('pointerdown', ev => {
  try { svg.setPointerCapture(ev.pointerId); } catch (_) {}
  pts.set(ev.pointerId, local(ev));
  if (pts.size === 1) pan = { start: local(ev), vx: view.x, vy: view.y, moved: false };
  if (pts.size === 2) { const [a, b] = [...pts.values()]; pinch = { d: Math.hypot(a[0] - b[0], a[1] - b[1]), k: view.k, vx: view.x, vy: view.y, c: [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2] }; pan = null; }
  svg.classList.add('grabbing');
});
svg.addEventListener('pointermove', ev => {
  const p = local(ev);
  if (!pts.has(ev.pointerId)) {
    const h = hit(...p);
    if (h !== state.hovered) { state.hovered = h; draw(); }
    return;
  }
  pts.set(ev.pointerId, p);
  if (pinch && pts.size === 2) {
    const [a, b] = [...pts.values()], k = Math.min(5, Math.max(0.3, pinch.k * Math.hypot(a[0] - b[0], a[1] - b[1]) / pinch.d));
    view.x = pinch.c[0] - (pinch.c[0] - pinch.vx) * k / pinch.k; view.y = pinch.c[1] - (pinch.c[1] - pinch.vy) * k / pinch.k; view.k = k;
    userMoved = true; draw();
  } else if (pan) {
    const dx = p[0] - pan.start[0], dy = p[1] - pan.start[1];
    if (Math.hypot(dx, dy) > 6) pan.moved = true;
    if (pan.moved) { view.x = pan.vx + dx; view.y = pan.vy + dy; userMoved = true; draw(); }
  }
});
const release = ev => {
  if (pan && !pan.moved) { const h = hit(...local(ev)); if (h) select(h.e); }
  pts.delete(ev.pointerId);
  if (pts.size < 2) pinch = null;
  if (!pts.size) { pan = null; svg.classList.remove('grabbing'); }
};
svg.addEventListener('pointerup', release);
svg.addEventListener('pointercancel', release);
svg.addEventListener('pointerleave', () => { if (state.hovered) { state.hovered = null; draw(); } });
svg.addEventListener('wheel', ev => {
  ev.preventDefault();
  const [px, py] = local(ev), k = Math.min(5, Math.max(0.3, view.k * Math.pow(1.0015, -ev.deltaY)));
  view.x = px - (px - view.x) * k / view.k; view.y = py - (py - view.y) * k / view.k; view.k = k;
  userMoved = true; draw();
}, { passive: false });
$('fit').onclick = () => { userMoved = false; fit(); draw(); };

// keepVisible moves the map just enough that the selected device sits above
// the sheet, keeping as much of the rest in view as it can.
function keepVisible() {
  const n = state.selected && nodes.find(x => x.e === state.selected);
  if (!n) return;
  const [, y] = at(n), vis = visibleHeight();
  if (y > vis - 40) view.y -= y - (vis - 70);
  else if (y < 30) view.y += 60 - y;
}

/* ---------- panel ---------- */

function select(e, focusPanel) {
  state.selected = e; state.note = ''; state.tab = 'device';
  if (document.body.classList.contains('sheet') && state.sheet === 'peek') setSheet('half');
  if (document.body.classList.contains('sheet')) keepVisible();
  render();
  if (focusPanel) $('body').focus();
}

const confWord = c => `<span class="conf ${esc(c)}">${esc(c)}</span>`;

function renderDevice() {
  const e = state.selected;
  let h = state.note ? `<p class="selnote">${esc(state.note)}</p>` : '';
  if (!e) return h + '<p class="note">Tap a device on the map, or open the Devices tab.</p>';
  const addr = [...ids(e, 'ip'), ...ids(e, 'mac')].join('   ');
  h += `<div class="dev"><div class="caps kicker">Selected</div><h2>${esc(labelOf(e))}</h2><div class="addr">${esc(addr)}</div><div class="claims">`;
  for (const [label, key, always] of [['Name', 'hostname', 1], ['Label', 'label', 0], ['Class', 'class', 1], ['Vendor', 'vendor', 0], ['Model', 'model', 0], ['OS', 'os', 0]]) {
    const c = bestClaim(e, key);
    if (!c && !always) continue;
    h += c
      ? `<div class="claim"><div class="k">${label}</div><div class="v"><span>${esc(c.value)}</span>${confWord(c.confidence)}</div>` +
        `<div class="e">${esc(c.source?.method)}: ${esc(c.source?.evidence)}</div></div>`
      : `<div class="claim"><div class="k">${label}</div><div class="v none">not learned</div></div>`;
  }
  h += '</div>';
  const seen = new Set(), svcs = [];
  for (const c of [...(e.claims || [])].sort((a, b) => rank(b.confidence) - rank(a.confidence))) {
    if (c.key === 'service' && !seen.has(c.value)) { seen.add(c.value); svcs.push(c); }
  }
  if (svcs.length) h += `<div class="sub"><div class="caps">Services</div>${svcs.map(c => `<div class="svc"><span>${esc(c.value)}</span>${confWord(c.confidence)}</div>`).join('')}</div>`;
  const notes = (e.claims || []).filter(c => c.key === 'note');
  if (notes.length) h += `<div class="sub"><div class="caps">Notes</div>${notes.map(c => `<p class="ev">${esc(c.value)}</p>`).join('')}</div>`;
  if ((e.conflicts || []).length) h += `<div class="sub"><div class="caps">Disagreements</div>${e.conflicts.map(c => `<p class="ev">${esc(c)}</p>`).join('')}</div>`;
  if ((e.merges || []).length) h += `<div class="sub"><div class="caps">Merged from</div>${e.merges.map(m => `<p class="ev">${esc(m)}</p>`).join('')}</div>`;
  return h + '</div>';
}

function entityByIdentity(idn) { return devices().find(e => (e.identities || []).includes(idn)); }
function entityByLabel(label) { return devices().find(e => labelOf(e) === label); }

function renderFindings() {
  const fs = survey.findings || [];
  if (!fs.length) return '<p class="note">Nothing worth reporting.</p>';
  return fs.map((f, i) => `<button class="row finding" data-finding="${i}"><span class="bar ${esc(f.severity)}"></span>` +
    `<span><span class="title"><span class="rt">${esc(f.title)}</span>${confWord(f.confidence)}</span><p>${esc(f.detail)}</p></span></button>`).join('');
}

function renderDevices() {
  const q = state.filter.toLowerCase();
  const rows = devices().filter(e => !q || [labelOf(e), ids(e, 'ip').join(' '), best(e, 'vendor'), sublabelOf(e)].join(' ').toLowerCase().includes(q));
  return `<input class="filter" id="filter" aria-label="Filter devices" placeholder="Filter by name, address or kind" value="${esc(state.filter)}">` +
    rows.map(e => {
      const ip = ids(e, 'ip').join(' ');
      return `<button class="row device ${e === state.selected ? 'sel' : ''}" data-id="${esc(e.id)}">` +
        `<span class="rt ${kindOf(e) === 'unknown' ? 'unk' : ''}">${esc(labelOf(e))}</span>` +
        `<span class="what">${ip !== labelOf(e) ? esc(ip) + '<br>' : ''}${esc(sublabelOf(e))}</span></button>`;
    }).join('') + (rows.length ? '' : '<p class="note">No device matches.</p>');
}

function renderDrift() {
  if (!drift) return '';
  let h = `<p class="since">Since the survey of ${esc(idDate(drift.before_id))}</p>`;
  h += (drift.caveats || []).map(c => `<p class="caveat">${esc(c)}</p>`).join('');
  if (!(drift.changes || []).length) h += '<p class="note">Nothing changed between the two surveys.</p>';
  h += (drift.changes || []).map((c, i) => {
    const text = !c.was ? c.now : !c.now ? 'was ' + c.was : `was ${c.was}, now ${c.now}`;
    const how = c.matched_by ? (c.matched_by === 'ip' ? 'matched by address only, ' : `matched by ${c.matched_by}, `) + c.match : '';
    return `<button class="row drift" data-change="${i}"><span class="kind">${esc(c.kind)}</span><span class="rt">${esc(c.entity)}</span>` +
      (text ? `<span class="ev">${esc(text)}</span>` : '') + (c.details || []).map(d => `<span class="ev">${esc(d)}</span>`).join('') +
      (how ? `<span class="ev">${esc(how)}</span>` : '') + '</button>';
  }).join('');
  return h;
}

function render() {
  if (!survey) return;
  for (const b of document.querySelectorAll('#tabs button')) {
    const on = b.dataset.tab === state.tab;
    b.setAttribute('aria-selected', on);
    b.tabIndex = on ? 0 : -1;
  }
  $('body').setAttribute('aria-labelledby', 'tab-' + state.tab);
  $('body').innerHTML = { device: renderDevice, findings: renderFindings, devices: renderDevices, drift: renderDrift }[state.tab]();
  for (const r of $('body').querySelectorAll('[data-finding]')) {
    const f = survey.findings[+r.dataset.finding], e = f.entities?.length && entityByIdentity(f.entities[0]);
    if (e) r.onclick = () => select(e, true); else r.disabled = true;
  }
  for (const r of $('body').querySelectorAll('[data-id]')) r.onclick = () => select(devices().find(e => e.id === r.dataset.id), true);
  for (const r of $('body').querySelectorAll('[data-change]')) {
    const e = entityByLabel(drift.changes[+r.dataset.change].entity);
    if (e) r.onclick = () => select(e, true); else r.disabled = true;
  }
  // The panel's content sets the peek height (sheetHeights), so a new device
  // or tab re-measures it.
  if (document.body.classList.contains('sheet')) $('panel').style.height = sheetHeights()[state.sheet] + 'px';
  const f = $('filter');
  if (f) f.oninput = () => {
    state.filter = f.value; render();
    const g = $('filter'); g.focus(); g.setSelectionRange(g.value.length, g.value.length);
  };
  draw();
}

function showTab(name) {
  if (name === 'drift' && !drift) return;
  state.tab = name;
  if (document.body.classList.contains('sheet') && state.sheet === 'peek') setSheet('half');
  render();
}
window.showTab = showTab;

for (const b of document.querySelectorAll('#tabs button')) b.onclick = () => showTab(b.dataset.tab);
// The tabs are one stop in the keyboard order; arrows move between them.
$('tabs').addEventListener('keydown', ev => {
  const tabs = [...document.querySelectorAll('#tabs button:not(.hidden)')], i = tabs.indexOf(document.activeElement);
  const next = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[ev.key];
  if (i < 0 || next === undefined) return;
  ev.preventDefault();
  const t = tabs[(next + tabs.length) % tabs.length];
  showTab(t.dataset.tab);
  t.focus();
});
$('skip').addEventListener('click', ev => {
  ev.preventDefault();
  if (document.body.classList.contains('sheet')) setSheet('full');
  showTab('devices');
  $('filter')?.focus();
});

/* ---------- layout: wide, two columns, or sheet ---------- */

function mode() {
  if (innerWidth < 760) return 'sheet';
  if (innerWidth < 1000 || innerHeight < 560) return 'twocol';
  return 'wide';
}

// The peek shows the tabs and the selected device's name and address, and
// ends just below them rather than cutting a line in half. The positions
// inside the sheet do not depend on its height, so they can be measured at
// any height.
function sheetHeights() {
  const h = $('main').clientHeight;
  let peek = 138;
  const addr = $('body').querySelector('.addr');
  if (addr && state.tab === 'device') {
    peek = Math.round(addr.getBoundingClientRect().bottom - $('panel').getBoundingClientRect().top + 16);
    peek = Math.min(Math.max(peek, 120), 240);
  }
  return { peek, half: Math.round(h * 0.55), full: h - 56 };
}

function setSheet(s, animate = true) {
  state.sheet = s;
  const p = $('panel');
  if (!animate) p.style.transition = 'none';
  p.style.height = sheetHeights()[s] + 'px';
  document.body.classList.toggle('peek', s === 'peek');
  document.body.classList.toggle('open', s !== 'peek');
  $('handle').setAttribute('aria-expanded', s !== 'peek');
  $('handle').setAttribute('aria-label', `Details panel, ${s === 'peek' ? 'collapsed' : s === 'half' ? 'half open' : 'open'}`);
  if (!animate) requestAnimationFrame(() => { p.style.transition = ''; });
  // The summary just changed height, so the map did too.
  requestAnimationFrame(() => { if (!userMoved) fit(); keepVisible(); draw(); });
}

function arrange() {
  const m = mode();
  for (const c of ['wide', 'twocol', 'sheet']) document.body.classList.toggle(c, c === m);
  if (m === 'sheet') {
    document.body.classList.remove('tight');
    if ($('summary').offsetHeight > $('main').clientHeight * 0.4) document.body.classList.add('tight');
    setSheet(state.sheet, false);
  }
  else { $('panel').style.height = ''; document.body.classList.remove('peek', 'open'); }
  if (!userMoved) fit();
  draw();
}
addEventListener('resize', arrange);
// The labels are measured in Inter; once it has loaded, measure again.
document.fonts?.ready.then(() => { if (survey) draw(); });

// The grip drags the sheet; a tap on it steps peek, half, full.
let gripDrag = null;
$('grip').addEventListener('pointerdown', ev => {
  gripDrag = { y: ev.clientY, h: $('panel').clientHeight, moved: false };
  $('panel').style.transition = 'none';
  // Capture keeps a drag alive when the finger leaves the grip; it throws for
  // a pointer the browser no longer tracks, which must not cost the gesture.
  try { $('grip').setPointerCapture(ev.pointerId); } catch (_) {}
});
$('grip').addEventListener('pointermove', ev => {
  if (!gripDrag) return;
  const hs = sheetHeights(), d = gripDrag.y - ev.clientY;
  if (Math.abs(d) > 4) gripDrag.moved = true;
  $('panel').style.height = Math.max(hs.peek, Math.min(hs.full, gripDrag.h + d)) + 'px';
});
$('grip').addEventListener('pointerup', () => {
  if (!gripDrag) return;
  $('panel').style.transition = '';
  if (gripDrag.moved) {
    const cur = $('panel').clientHeight, hs = sheetHeights();
    setSheet(Object.keys(hs).sort((a, b) => Math.abs(hs[a] - cur) - Math.abs(hs[b] - cur))[0]);
  } else {
    setSheet({ peek: 'half', half: 'full', full: 'peek' }[state.sheet]);
  }
  gripDrag = null;
});
$('handle').addEventListener('keydown', ev => {
  const order = ['peek', 'half', 'full'], i = order.indexOf(state.sheet);
  if (ev.key === 'ArrowUp' && i < 2) { ev.preventDefault(); setSheet(order[i + 1]); }
  if (ev.key === 'ArrowDown' && i > 0) { ev.preventDefault(); setSheet(order[i - 1]); }
});

// Back in the Android app: lower the sheet. Returns whether it did anything,
// so the app leaves only when nothing was open. The page never calls the app.
window.carta = {
  collapse() {
    if (document.body.classList.contains('sheet') && state.sheet !== 'peek') { setSheet('peek'); return true; }
    return false;
  },
};
