// Shared data for the three Carta Desktop layout mocks. The network is
// fictional (192.168.10.0/24, documentation MACs from RFC 7042); it is the
// same survey the Fyne mock shows, so the options can be compared fairly.
window.CARTA = (() => {
  const vantage = {
    prefix: '192.168.10.0/24', ip: '192.168.10.20', iface: 'Wi-Fi', host: 'carta-laptop',
    started: '23 Nov 2026, 15:56', duration: '41 s', instrument: 'carta-desktop',
    gateway: '192.168.10.1', resolver: '192.168.10.1',
  };

  // kind: named (a name and a confident identification), partial (one of the
  // two), unknown (neither), infra (the gateway and the instrument).
  const devices = [
    { id: 'gw', label: 'gateway.lab.example', sub: 'router / gateway', kind: 'infra', ip: '192.168.10.1', mac: '00:00:5e:00:53:01' },
    { id: 'ap', label: 'ap-upstairs.lab.example', sub: 'access point', kind: 'named', ip: '192.168.10.2', mac: '00:00:5e:00:53:02' },
    { id: 'nas', label: 'nas.local', sub: 'storage / NAS', kind: 'named', ip: '192.168.10.5', mac: '00:00:5e:00:53:05' },
    { id: 'prn', label: 'office-printer.local', sub: 'printer', kind: 'named', ip: '192.168.10.12', mac: '00:00:5e:00:53:0c' },
    { id: 'me', label: 'carta-laptop', sub: 'survey vantage', kind: 'infra', ip: '192.168.10.20', mac: '00:00:5e:00:53:14' },
    { id: 'pc', label: 'studio-pc', sub: 'workstation', kind: 'named', ip: '192.168.10.21', mac: '00:00:5e:00:53:15' },
    { id: 'tv', label: 'Living Room TV', sub: 'media / TV', kind: 'partial', ip: '192.168.10.30', mac: '00:00:5e:00:53:1e' },
    { id: 'spk', label: 'kitchen-speaker.local', sub: 'speaker', kind: 'named', ip: '192.168.10.31', mac: '00:00:5e:00:53:1f' },
    { id: 'p41', label: '192.168.10.41', sub: 'personal device?', kind: 'unknown', ip: '192.168.10.41', mac: '7e:5d:c3:00:00:aa' },
    { id: 'p42', label: '192.168.10.42', sub: 'personal device?', kind: 'unknown', ip: '192.168.10.42', mac: '3a:91:0c:00:00:bb' },
    { id: 'tab', label: 'kids-tablet.local', sub: 'tablet?', kind: 'partial', ip: '192.168.10.44', mac: '00:00:5e:00:53:2c' },
    { id: 'plg', label: '192.168.10.50', sub: 'smart plug', kind: 'partial', ip: '192.168.10.50', mac: '00:00:5e:00:53:32' },
    { id: 'cam', label: '192.168.10.60', sub: 'camera?', kind: 'unknown', ip: '192.168.10.60', mac: '00:00:5e:00:53:3c' },
    { id: 'u77', label: '192.168.10.77', sub: 'unidentified', kind: 'unknown', ip: '192.168.10.77', mac: '' },
  ];
  const links = devices.filter(d => d.id !== 'gw').map(d => ({ a: 'gw', b: d.id, inferred: true }));

  // The selected device, as the inspector shows it: each claim with the
  // confidence it deserves and the evidence it came from.
  const selected = {
    id: 'gw', label: 'gateway.lab.example', ip: '192.168.10.1', mac: '00:00:5e:00:53:01',
    claims: [
      { key: 'Name', value: 'gateway.lab.example', conf: 'inferred', method: 'rdns', evidence: 'PTR gateway.lab.example.' },
      { key: 'Vendor', value: 'Northwind Networks', conf: 'inferred', method: 'oui', evidence: '00:00:5E' },
      { key: 'Class', value: 'router / gateway', conf: 'observed', method: 'route-table', evidence: 'default via 192.168.10.1 dev Wi-Fi' },
      { key: 'Model', value: 'NW-AX3000', conf: 'asserted', method: 'ssdp', evidence: 'modelName: NW-AX3000' },
      { key: 'OS', value: 'Linux', conf: 'weak', method: 'http', evidence: 'Server: lighttpd' },
    ],
    services: [
      { name: 'dns', port: '53/udp', conf: 'observed' },
      { name: 'https', port: '443/tcp', conf: 'observed' },
    ],
  };

  // Every device's claims, so a mock can open any of them. The gateway's are
  // the richest; the rest are what a survey of this network would plausibly
  // learn, with the same honesty about how each was learned.
  const claims = {
    gw: selected.claims,
    ap: [
      { key: 'Name', value: 'ap-upstairs.lab.example', conf: 'inferred', method: 'rdns', evidence: 'PTR ap-upstairs.lab.example.' },
      { key: 'Class', value: 'access point', conf: 'inferred', method: 'ssdp', evidence: 'deviceType: WLANAccessPointDevice' },
      { key: 'Vendor', value: 'Northwind Networks', conf: 'inferred', method: 'oui', evidence: '00:00:5E' },
    ],
    nas: [
      { key: 'Name', value: 'nas.local', conf: 'asserted', method: 'mdns', evidence: 'nas.local A 192.168.10.5' },
      { key: 'Class', value: 'storage / NAS', conf: 'asserted', method: 'mdns', evidence: '_smb._tcp, _afpovertcp._tcp' },
      { key: 'Vendor', value: 'Contoso Storage', conf: 'inferred', method: 'oui', evidence: '00:00:5E' },
    ],
    prn: [
      { key: 'Name', value: 'office-printer.local', conf: 'asserted', method: 'mdns', evidence: 'office-printer.local A 192.168.10.12' },
      { key: 'Class', value: 'printer', conf: 'asserted', method: 'mdns', evidence: '_ipp._tcp.local' },
      { key: 'Model', value: 'Contoso P2100', conf: 'asserted', method: 'mdns', evidence: 'ty=Contoso P2100' },
    ],
    me: [
      { key: 'Name', value: 'carta-laptop', conf: 'asserted', method: 'host', evidence: 'this machine' },
      { key: 'Class', value: 'survey vantage', conf: 'observed', method: 'host', evidence: 'the instrument itself' },
      { key: 'OS', value: 'Windows', conf: 'asserted', method: 'host', evidence: 'runtime.GOOS' },
    ],
    pc: [
      { key: 'Name', value: 'studio-pc', conf: 'asserted', method: 'nbns', evidence: 'STUDIO-PC <00> UNIQUE' },
      { key: 'Class', value: 'workstation', conf: 'inferred', method: 'nbns', evidence: 'file server service <20>' },
      { key: 'OS', value: 'Windows', conf: 'weak', method: 'tcp-connect', evidence: 'TTL 128' },
    ],
    tv: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Label', value: 'Living Room TV', conf: 'asserted', method: 'ssdp', evidence: 'friendlyName: Living Room TV' },
      { key: 'Class', value: 'media / TV', conf: 'asserted', method: 'ssdp', evidence: 'deviceType: MediaRenderer' },
    ],
    spk: [
      { key: 'Name', value: 'kitchen-speaker.local', conf: 'asserted', method: 'mdns', evidence: 'kitchen-speaker.local A 192.168.10.31' },
      { key: 'Class', value: 'speaker', conf: 'asserted', method: 'mdns', evidence: '_googlecast._tcp' },
    ],
    p41: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Class', value: 'personal device (randomised MAC)', conf: 'weak', method: 'oui', evidence: 'locally administered bit set' },
    ],
    p42: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Class', value: 'personal device (randomised MAC)', conf: 'weak', method: 'oui', evidence: 'locally administered bit set' },
    ],
    tab: [
      { key: 'Name', value: 'kids-tablet.local', conf: 'asserted', method: 'mdns', evidence: 'kids-tablet.local A 192.168.10.44' },
      { key: 'Class', value: 'tablet', conf: 'weak', method: 'mdns', evidence: '_companion-link._tcp' },
    ],
    plg: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Class', value: 'smart plug', conf: 'inferred', method: 'http', evidence: '<title>Plug setup</title>' },
      { key: 'Vendor', value: 'Fabrikam IoT', conf: 'inferred', method: 'oui', evidence: '00:00:5E' },
    ],
    cam: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Class', value: 'camera', conf: 'weak', method: 'tcp-connect', evidence: '554/tcp (rtsp) open' },
      { key: 'Vendor', value: 'Fabrikam IoT', conf: 'inferred', method: 'oui', evidence: '00:00:5E' },
    ],
    u77: [
      { key: 'Name', value: '', conf: '', method: '', evidence: '' },
      { key: 'Class', value: '', conf: '', method: '', evidence: '' },
    ],
  };
  const services = {
    gw: selected.services, nas: [{ name: 'smb', port: '445/tcp', conf: 'asserted' }, { name: 'https', port: '5001/tcp', conf: 'observed' }],
    prn: [{ name: 'ipp', port: '631/tcp', conf: 'asserted' }], cam: [{ name: 'rtsp', port: '554/tcp', conf: 'observed' }],
    u77: [{ name: 'ssh', port: '22/tcp', conf: 'observed' }], pc: [{ name: 'smb', port: '445/tcp', conf: 'observed' }],
  };

  const findings = [
    { severity: 'risk', title: 'A camera streams without a name or an owner', conf: 'observed', entity: 'cam',
      detail: '192.168.10.60 answers on RTSP (554/tcp) and announces nothing about itself.' },
    { severity: 'warn', title: '2 devices hide their hardware address', conf: 'inferred', entity: 'p41',
      detail: 'Randomised MACs are normal on phones, but they cannot be told apart across surveys.' },
    { severity: 'info', title: 'One resolver, and it is the gateway', conf: 'observed', entity: 'gw',
      detail: 'If 192.168.10.1 stops answering DNS, every device here loses names.' },
  ];

  const drift = {
    since: '16 Nov 2026, 14:44',
    changes: [
      { kind: 'appeared', id: 'p42', entity: '192.168.10.42', text: 'personal device (randomised MAC)' },
      { kind: 'moved', id: 'cam', entity: '192.168.10.60', text: 'was 192.168.10.61, now 192.168.10.60', by: 'mac', conf: 'observed' },
      { kind: 'mutated', id: 'nas', entity: 'nas.local', text: 'service appeared: https (5001/tcp)', by: 'mac', conf: 'observed' },
    ],
  };

  // 254 addresses: which answered, which were asked and stayed silent.
  const answered = new Set([1, 2, 5, 12, 20, 21, 30, 31, 41, 42, 44, 50, 60, 77]);
  const coverage = { total: 254, swept: 254, answered: answered.size, isAnswered: n => answered.has(n) };

  // The web UI's spring layout, run to a fixed count so the picture is the
  // same every time; returns positions normalised to 0..1.
  function layout() {
    const nodes = devices.map((d, i) => {
      const a = (i / devices.length) * Math.PI * 2;
      return { d, x: Math.cos(a) * 220, y: Math.sin(a) * 220, vx: 0, vy: 0 };
    });
    const idx = Object.fromEntries(nodes.map(n => [n.d.id, n]));
    const ls = links.map(l => ({ a: idx[l.a], b: idx[l.b] }));
    for (let s = 0; s < 400; s++) {
      const t = 1 - s / 400;
      for (const n of nodes) {
        for (const m of nodes) {
          if (n === m) continue;
          const dx = n.x - m.x, dy = n.y - m.y, d2 = dx * dx + dy * dy || 0.01, f = 2400 / d2;
          n.vx += dx * f * 0.02; n.vy += dy * f * 0.02;
        }
        n.vx -= n.x * 0.002; n.vy -= n.y * 0.002;
      }
      for (const l of ls) {
        const dx = l.b.x - l.a.x, dy = l.b.y - l.a.y, d = Math.hypot(dx, dy) || 0.01, f = (d - 130) * 0.01;
        l.a.vx += dx / d * f; l.a.vy += dy / d * f; l.b.vx -= dx / d * f; l.b.vy -= dy / d * f;
      }
      for (const n of nodes) { n.x += n.vx * t; n.y += n.vy * t; n.vx *= 0.82; n.vy *= 0.82; }
    }
    const xs = nodes.map(n => n.x), ys = nodes.map(n => n.y);
    const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
    for (const n of nodes) { n.u = (n.x - minX) / (maxX - minX); n.v = (n.y - minY) / (maxY - minY); }
    return idx;
  }

  return { vantage, devices, links, selected, claims, services, findings, drift, coverage, layout: layout() };
})();
