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
});

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
  const w = canvas.clientWidth, h = canvas.clientHeight;
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

    ctx.font = '11px ui-monospace, Menlo, Consolas, monospace';
    ctx.fillStyle = n.kind === 'unknown' ? colour.dim : colour.fg;
    ctx.textAlign = 'center';
    ctx.fillText(n.label, n.x, n.y + n.r + 13);

    ctx.font = '9px ui-monospace, Menlo, Consolas, monospace';
    ctx.fillStyle = colour.dim;
    ctx.fillText(n.sub, n.x, n.y + n.r + 24);
  }
  ctx.restore();
}

/* ---------- interaction ---------- */

let dragging = null, panning = null;

canvas.addEventListener('mousedown', ev => {
  const p = toModel(ev);
  const hit = nodes.find(n => Math.hypot(n.x - p.x, n.y - p.y) < n.r + 6);
  if (hit) {
    dragging = hit;
    select(hit);
  } else {
    panning = { x: ev.clientX - view.x, y: ev.clientY - view.y };
    userMoved = true;
  }
});

window.addEventListener('mousemove', ev => {
  if (dragging) {
    const p = toModel(ev);
    dragging.x = p.x; dragging.y = p.y;
    draw();
  } else if (panning) {
    view.x = ev.clientX - panning.x;
    view.y = ev.clientY - panning.y;
    draw();
  }
});

window.addEventListener('mouseup', () => { dragging = null; panning = null; });

canvas.addEventListener('wheel', ev => {
  ev.preventDefault();
  userMoved = true;
  const factor = ev.deltaY < 0 ? 1.1 : 0.9;
  const rect = canvas.getBoundingClientRect();
  const mx = ev.clientX - rect.left, my = ev.clientY - rect.top;
  view.x = mx - (mx - view.x) * factor;
  view.y = my - (my - view.y) * factor;
  view.k *= factor;
  draw();
}, { passive: false });

// A canvas that gains its size after load — a hidden pane being shown, a split
// being dragged — has to be re-framed, but only while the view is still the one
// carta chose. Once someone has panned or zoomed, the framing is theirs.
new ResizeObserver(() => {
  if (!userMoved) fit();
  draw();
}).observe(canvas);

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

function select(node) {
  selected = node;
  renderDevice(node.e);
  showTab('device');
  draw();
}

/* ---------- panels ---------- */

function renderVantage() {
  const v = survey.vantage || {};
  document.getElementById('vantage').textContent =
    `${v.prefix || '?'} · from ${v.ip || '?'} on ${v.interface || '?'} · ${new Date(survey.started_at).toLocaleString()}`;
}

function renderCoverage() {
  const el = document.getElementById('coverage');
  const c = (survey.coverage || [])[0];
  if (!c) { el.textContent = ''; return; }
  const responded = c.responded || 0;
  const total = c.addrs_total || 1;
  const pct = Math.round(responded / total * 100);
  el.innerHTML = `
    <div class="covbar">
      <i class="responded" style="width:${pct}%"></i>
      <i class="silent" style="width:${100 - pct}%"></i>
    </div>
    ${responded} of ${total} addresses in ${c.prefix} answered.
    The hatched ${total - responded} did not — unknown, not empty.`;
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
  el.innerHTML = '<input class="searchbox" id="q" placeholder="filter by name, address, vendor or class" />'
    + '<div id="rows"></div>';
  const rows = document.getElementById('rows');
  const paint = (filter) => {
    const f = (filter || '').toLowerCase();
    rows.innerHTML = nodes
      .filter(n => !f || rowText(n.e).toLowerCase().includes(f))
      .map(n => `<div class="row" data-id="${esc(n.id)}">
          <span class="label">${esc(n.label)}</span>
          <span class="meta">${esc(ids(n.e, 'ip').join(' '))}<br>${esc(best(n.e, 'class') || best(n.e, 'vendor') || 'unidentified')}</span>
        </div>`).join('');
    rows.querySelectorAll('.row').forEach(r => {
      r.onclick = () => {
        const n = nodes.find(x => x.id === r.dataset.id);
        if (n) select(n);
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
  b.onclick = () => showTab(b.dataset.tab);
});

function showTab(name) {
  document.querySelectorAll('.tabs button').forEach(b => b.classList.toggle('on', b.dataset.tab === name));
  document.querySelectorAll('.tabbody').forEach(d => d.classList.toggle('hidden', d.id !== name));
}

/* Everything rendered here came off an untrusted network: a device's own
   hostname, banner or page title is attacker-controlled text. It is escaped
   once, here, and never inserted as HTML anywhere else. */
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
