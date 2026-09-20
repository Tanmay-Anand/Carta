<p align="center">
  <img src="assets/carta-logo.png" alt="CARTA" width="160">
</p>

<h1 align="center">carta</h1>

<p align="center">
  Plug into a network nobody has ever documented, and it documents itself —
  with the evidence for every claim attached, and the parts it could not reach
  drawn as blank space rather than guessed at.
</p>

---

Most networks are undocumented. The diagram on the wall is three years old, the
inventory spreadsheet drifted within weeks of being written, and the person who
built the addressing plan left in 2019. Every troubleshooting session, change
plan and audit begins with the same archaeology: reconstructing what the network
*is* from memory and guesswork.

`carta` is one command that does the archaeology for you:

```bash
carta survey
```

Thirty seconds later you have a device inventory, a topology diagram, an
addressing plan, a findings report, and a signed-in-place snapshot that the next
run diffs against. No installation, no agent, no credentials, no admin rights,
no internet.

## The idea that makes this more than another scanner

A network map is not a picture. It is **a set of claims about the world**, and
those claims come from wildly different qualities of evidence:

| Claim | Evidence | How much you should trust it |
|---|---|---|
| `10.1.4.22` is alive | It completed a TCP handshake | **observed** — direct observation |
| `10.1.4.22` is a printer | It announced `_ipp._tcp` over mDNS | **asserted** — the device said so |
| `10.1.4.22` is a Dell | The OUI of its MAC address | **inferred** — prefixes get resold, VMs inherit them |
| `10.1.4.22` runs Linux | A TTL of 64 and an SSH banner | **weak** — trivially altered, frequently wrong |
| There is nothing at `10.1.4.47` | A sweep found no response | **This is not evidence of absence** |

Conventional discovery tools flatten all five into one confident-looking diagram
with identical boxes. A diagram that presents inference with the same authority
as observation is worse than no diagram, because engineers plan changes against
it and the parts that are wrong are indistinguishable from the parts that are
right.

So in `carta`, **confidence is carried on every single claim**, from the
observation that produced it through to the cell in the rendered table. It is
never averaged into a false probability, and a device that answered but revealed
nothing is reported as exactly that.

## What it produces

```
carta-surveys/20260920T041611Z/
├── README.md         as-built: the vantage, the coverage, the caveats
├── inventory.md      every device, what it is, how confident that is
├── inventory.csv     the same, for a spreadsheet
├── topology.mmd      the network as a Mermaid diagram
├── addressing.md     addressing, naming, occupancy, who serves what
├── findings.md       what is worth doing something about
├── provenance.md     every claim, its source, and every merge behind a device
├── drift.md          what changed since the last survey
├── survey.json       the snapshot the next run diffs against
└── survey.db         the same survey, queryable in SQLite
```

A real inventory row, from a home network:

| Address | MAC | Name | Vendor | Class | Services |
|---|---|---|---|---|---|
| `192.168.0.1` | `b8:fb:b3:95:58:0d` | Archer-C5 <sub>asserted</sub> | TP-Link <sub>asserted</sub> | router / gateway <sub>observed</sub> | dhcp (67/udp), dns (53/udp), telnet (23/tcp) |
| `192.168.0.4` | `7e:5d:c3:c7:a9:be` | — | — | personal device (randomised MAC) <sub>weak</sub> | |

The second row is the point. It answered, it would not say what it is, its MAC
is randomised so no vendor can be inferred, and all of that is stated rather
than papered over.

## The browser view

```bash
carta ui
```

Opens the survey on `127.0.0.1` — bound to loopback specifically, because a
survey document lists every device on a network and where its management
interface is, and serving that on `0.0.0.0` would publish the map of the network
to the network.

Shape carries certainty: a filled node is identified, an outlined node is partly
known, a dashed node answered and revealed nothing. The bar under the map shows
what proportion of the address range answered at all, with the silent remainder
hatched rather than filled.

## Drift

Run it again next month:

```
Drift since the last survey: 2 appeared, 1 vanished, 1 moved.
```

The hard problem is identity across time, not comparison. A device whose DHCP
lease moved it from `.17` to `.43` has not appeared and vanished — it has
**moved** — and a report that says otherwise trains people to ignore it. Matching
runs on the most durable identity available (burned-in MAC, then hostname, then
address) and each match records which one it used. Randomised MAC addresses are
explicitly *not* used to match, because a phone regenerates one per network.

## Install

```bash
go install github.com/Tanmay-Anand/carta/cmd/carta@latest
```

Or build the single static binary:

```bash
CGO_ENABLED=0 go build -o carta ./cmd/carta
```

There is no runtime dependency. `CGO_ENABLED=0` cross-compiles to every target
from one host, which is the requirement for "runs on whatever machine is at the
site" — including a machine you are not allowed to install anything on.

## Usage

```
carta survey [flags]        survey the network this machine is on
carta diff A.json [B.json]  report what changed between two surveys
carta ui [SNAPSHOT.json]    open a survey in a browser
```

| Flag | Default | |
|---|---|---|
| `--out DIR` | `carta-surveys` | where the document pack is written |
| `--passive` | off | emit nothing: caches and multicast listening only |
| `--timeout DUR` | `700ms` | per-connection budget |
| `--concurrency N` | `128` | parallel workers |
| `--deadline DUR` | `4m` | hard limit on the whole survey |
| `--oui FILE` | built-in | an IEEE `oui.csv` for full vendor coverage |
| `--open` | off | open the browser view when the survey finishes |

## How it works

**Tier P — orientation, zero packets emitted.** Interfaces, routes, gateway,
DHCP lease, resolvers, Wi-Fi association, and the ARP cache as it stands before
anything is touched. It also records how the host is a *biased instrument*: a
VPN carrying the default route, a host firewall, a virtual switch, a system
proxy. Each one annotates the findings it affects, because "we found 14 hosts"
means nothing until you know where the instrument was standing.

**Tier 0 — unprivileged discovery.** Everything here works from an ordinary user
account on Windows, macOS and Linux:

- **ARP-cache warming** — touching an address at L3 forces the kernel to resolve
  it at L2, which is how an unprivileged process performs layer-2 discovery.
  L2 presence is far stronger evidence than any L3 response.
- **Multicast service discovery** — mDNS, SSDP/UPnP and NetBIOS in parallel.
  Collectively the richest unprivileged identity source on a modern network, and
  the one most scanners underuse: printers, speakers, NAS units and TVs all
  announce their own model number.
- **Risk-ranked TCP probing** — not "the top 1000 ports", but the ~35 that say
  what a device *is*, with banner capture, TLS certificate subjects and HTTP
  management-interface fingerprinting.
- **Reverse DNS** — and PTR coverage as a documentation-quality metric: a /24
  with four PTR records is a network nobody is administering by name.

**The model.** Every observation lands in one graph through a single entry
point, and entity resolution happens continuously via union-find over identity
keys (`mac:…`, `ip:…`, `host:…`). A device seen by ARP, by mDNS and by reverse
DNS becomes one device, every merge is recorded, and when two sources disagree
**both claims are kept**, the winner is demoted one level, and the disagreement
is printed.

Tier 1 (LLDP/CDP passive capture) and Tier 2 (SNMP topology crawl, switch-port
location) are designed but not implemented — see [Not implemented](#not-implemented).

## Scope and consent

Survey only networks you are authorised to survey. `carta` emits ordinary
unprivileged traffic — TCP connects, multicast queries, DNS lookups — but on a
managed network that is still activity someone may have to explain. `--passive`
emits nothing at all and is the correct default for OT, ICS and clinical
networks.

It refuses to sweep a prefix larger than a /22 rather than silently truncating
it, because a truncated sweep rendered as a complete one is the exact failure
this tool exists to prevent.

## Not implemented

Honest about its own coverage, since that is the whole point:

- **Tier 1** — LLDP/CDP, STP, VLAN observation, HSRP/VRRP. These need packet
  capture and elevation, which would break the "runs on a borrowed laptop"
  property; the design isolates them in a separate helper binary.
- **Tier 2** — SNMP `BRIDGE-MIB` crawl and switch-port location. Needs
  credentials.
- **Multi-vantage merge**, Ed25519-signed snapshots, IPv6 discovery beyond what
  the host already knows.
- The **built-in OUI table** covers the prefixes common on home and small-office
  networks. Pass `--oui` with the IEEE registry for full coverage.

## Tests

```bash
go test ./...
```

The suite covers the confidence calculus and its promotion rules, entity
resolution and merge behaviour, snapshot determinism, drift matching across
address changes, Markdown injection from hostile device names, and a regression
test for a parser that once hung forever on an unhandled DNS record type.

## Licence

MIT.
