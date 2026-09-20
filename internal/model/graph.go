package model

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Source is where a claim came from. Every claim has exactly one; a claim that
// several methods agree on becomes several claims that entity resolution later
// reconciles, rather than one claim with a fudged origin.
type Source struct {
	Method   string    `json:"method"`             // "arp-cache", "mdns", "ssdp", "nbns", "rdns", "tcp-connect", "tls-cert", "http", "oui", "route-table"
	Evidence string    `json:"evidence,omitempty"` // the raw thing that was observed, verbatim where it is short enough to keep
	At       time.Time `json:"at"`
}

// Claim is one attribute of one entity, with the evidence for it attached.
//
// Nothing in CARTA ever stores a bare value. If it is worth recording that a
// device is a printer, it is worth recording that we believe that because it
// answered on port 9100, which is a guess, and not because it said so.
type Claim struct {
	Key        string     `json:"key"` // "hostname", "vendor", "os", "class", "service", "model", "alive"
	Value      string     `json:"value"`
	Confidence Confidence `json:"confidence"`
	Source     Source     `json:"source"`
}

// Identity is a key that can be used to decide two observations describe the
// same device: "mac:aa:bb:cc:dd:ee:ff", "ip:192.168.1.7", "host:nas.local".
//
// They are namespaced strings rather than a struct because they are used as map
// keys in the union-find, and because a new identity kind (a serial number from
// an SSDP description, say) should not require a schema change.
type Identity string

func IPIdentity(ip string) Identity   { return Identity("ip:" + ip) }
func MACIdentity(mac string) Identity { return Identity("mac:" + strings.ToLower(mac)) }
func HostIdentity(h string) Identity {
	return Identity("host:" + strings.ToLower(strings.TrimSuffix(h, ".")))
}

// Kind reports the namespace of an identity ("ip", "mac", "host").
func (i Identity) Kind() string {
	if k, _, ok := strings.Cut(string(i), ":"); ok {
		return k
	}
	return ""
}

// Value reports the identity without its namespace.
func (i Identity) Value() string {
	if _, v, ok := strings.Cut(string(i), ":"); ok {
		return v
	}
	return string(i)
}

// Entity is one thing on the network, as far as we can tell.
//
// "As far as we can tell" is load-bearing: two Entities may in fact be one
// device whose second interface we never associated, and one Entity may in fact
// be two devices behind a NAT we could not see through. Merges is the audit
// trail of every join that produced this entity, so a wrong merge is traceable
// rather than mysterious.
type Entity struct {
	ID         string     `json:"id"` // stable within a survey: the lowest identity, so snapshots diff cleanly
	Identities []Identity `json:"identities"`
	Claims     []Claim    `json:"claims"`
	Merges     []string   `json:"merges,omitempty"` // human-readable: "mac:.. + ip:.. (arp-cache)"
	Conflicts  []string   `json:"conflicts,omitempty"`
}

// Best returns the most trustworthy value for a key, and its confidence.
//
// Ties are broken by recency, then lexically, so the same inputs always produce
// the same document — a survey whose report reshuffles between runs makes drift
// reports unreadable.
func (e *Entity) Best(key string) (string, Confidence) {
	var bestVal string
	var bestConf Confidence
	var bestAt time.Time
	var bestSelf bool
	for _, c := range e.Claims {
		if c.Key != key {
			continue
		}
		// A claim from the "host" method is the machine describing itself, and
		// the instrument is the one device on the network whose identity is not
		// in question. Without this, the survey host gets renamed after
		// whatever application happened to be announcing itself loudest.
		self := c.Source.Method == "host"
		better := c.Confidence > bestConf ||
			(c.Confidence == bestConf && self && !bestSelf) ||
			(c.Confidence == bestConf && self == bestSelf && c.Source.At.After(bestAt)) ||
			(c.Confidence == bestConf && self == bestSelf && c.Source.At.Equal(bestAt) && c.Value < bestVal)
		if bestVal == "" || better {
			bestVal, bestConf, bestAt, bestSelf = c.Value, c.Confidence, c.Source.At, self
		}
	}
	return bestVal, bestConf
}

// All returns every distinct value claimed for a key, highest confidence first.
// Used for keys that are genuinely multi-valued (services, addresses) and for
// printing disagreements.
func (e *Entity) All(key string) []Claim {
	var out []Claim
	for _, c := range e.Claims {
		if c.Key == key {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}

// Addresses returns the entity's IPs, sorted, for stable rendering.
func (e *Entity) Addresses() []string { return e.identityValues("ip") }

// MACs returns the entity's hardware addresses.
func (e *Entity) MACs() []string { return e.identityValues("mac") }

// Hostnames returns every name the entity is known by.
func (e *Entity) Hostnames() []string { return e.identityValues("host") }

func (e *Entity) identityValues(kind string) []string {
	var out []string
	for _, id := range e.Identities {
		if id.Kind() == kind {
			out = append(out, id.Value())
		}
	}
	sort.Strings(out)
	return out
}

// Label is what the entity is called in a diagram: the friendliest name we have
// that is not an invention.
func (e *Entity) Label() string {
	if h, _ := e.Best("hostname"); h != "" {
		return h
	}
	// An application's own label ("Kitchen Speaker", "Living Room TV") is a
	// worse name than a hostname and a far better one than an IP address, so it
	// sits exactly here in the order.
	if l, _ := e.Best("label"); l != "" {
		return l
	}
	if addrs := e.Addresses(); len(addrs) > 0 {
		return addrs[0]
	}
	if macs := e.MACs(); len(macs) > 0 {
		return macs[0]
	}
	return e.ID
}

// EdgeKind is the relationship an edge asserts. Each kind carries its own
// natural confidence ceiling, recorded on the edge itself.
type EdgeKind string

const (
	EdgeGateway  EdgeKind = "gateway-of"            // this entity routes for that one
	EdgeSameL2   EdgeKind = "same-broadcast-domain" // inferred from shared ARP visibility
	EdgeVantage  EdgeKind = "surveyed-from"         // the host CARTA ran on, to what it saw
	EdgeResolver EdgeKind = "dns-resolver-for"
	EdgeDHCP     EdgeKind = "dhcp-server-for"
)

// Edge is a claimed relationship between two entities. RTT and loss hang off
// the edge rather than off a node, because a latency figure is a property of a
// path: "12ms" means nothing until you say between what and what.
type Edge struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Kind       EdgeKind   `json:"kind"`
	Confidence Confidence `json:"confidence"`
	Source     Source     `json:"source"`
	RTTms      float64    `json:"rtt_ms,omitempty"`
}

// Coverage records what was looked for, so the report can distinguish "there is
// nothing there" from "we did not look".
//
// This is the single most important structure for honesty in the output. A
// sweep that finds no response at an address has produced absence of evidence,
// and a renderer that draws that as empty space is telling the truth only
// because this struct let it know the difference.
type Coverage struct {
	Prefix      string    `json:"prefix"`      // the subnet swept, e.g. "192.168.1.0/24"
	AddrsSwept  int       `json:"addrs_swept"` // how many addresses were actually touched
	AddrsTotal  int       `json:"addrs_total"` // how many the prefix contains (minus network/broadcast)
	Responded   int       `json:"responded"`
	PortsProbed []int     `json:"ports_probed"`
	Protocols   []string  `json:"protocols"` // multicast protocols queried
	Frontier    []string  `json:"frontier"`  // prefixes seen referenced but deliberately not surveyed
	Notes       []string  `json:"notes"`     // e.g. "VPN default route present; off-link results suppressed"
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
}

// Complete reports whether every address in the prefix was reached by the
// sweep. An incomplete sweep is not a failure, but it must never be rendered as
// a complete one.
func (c Coverage) Complete() bool { return c.AddrsTotal > 0 && c.AddrsSwept >= c.AddrsTotal }

// Graph is one survey's model of a network.
type Graph struct {
	SurveyID  string            `json:"survey_id"`
	StartedAt time.Time         `json:"started_at"`
	Vantage   Vantage           `json:"vantage"`
	Entities  []*Entity         `json:"entities"`
	Edges     []Edge            `json:"edges"`
	Coverage  []Coverage        `json:"coverage"`
	Findings  []Finding         `json:"findings"`
	Meta      map[string]string `json:"meta,omitempty"`

	// resolution state, rebuilt on load, never serialised
	parent map[Identity]Identity
	byID   map[Identity]*Entity

	// Discovery is concurrent by design — a sweep, two multicast listeners and
	// a probe pool all report findings as they arrive — and they all land here.
	// One mutex around the whole graph is the right granularity: contention is
	// negligible next to network I/O, and finer locking would mean a merge
	// could run while another goroutine walks the entity it is folding away.
	mu sync.Mutex
}

// Vantage is where the survey was run from. Everything in a survey is relative
// to it: what you can see depends entirely on where you stand, and two surveys
// from different jacks are not comparable without knowing this.
type Vantage struct {
	Host        string   `json:"host"`
	Interface   string   `json:"interface"`
	IP          string   `json:"ip"`
	MAC         string   `json:"mac"`
	Prefix      string   `json:"prefix"`
	Gateway     string   `json:"gateway"`
	Resolvers   []string `json:"resolvers"`
	SearchList  []string `json:"search_domains,omitempty"`
	Wireless    string   `json:"wireless,omitempty"` // SSID, if associated
	Confounders []string `json:"confounders,omitempty"`
	OS          string   `json:"os"`
}

// Finding is a health or hygiene observation about the network as a whole
// rather than about one device.
type Finding struct {
	Severity   string     `json:"severity"` // "info", "warn", "risk"
	Title      string     `json:"title"`
	Detail     string     `json:"detail"`
	Confidence Confidence `json:"confidence"`
	Entities   []string   `json:"entities,omitempty"`
}

func NewGraph(surveyID string) *Graph {
	return &Graph{
		SurveyID:  surveyID,
		StartedAt: time.Now(),
		parent:    map[Identity]Identity{},
		byID:      map[Identity]*Entity{},
		Meta:      map[string]string{},
	}
}

// find is the union-find lookup with path compression.
func (g *Graph) find(id Identity) Identity {
	p, ok := g.parent[id]
	if !ok {
		g.parent[id] = id
		return id
	}
	if p == id {
		return id
	}
	root := g.find(p)
	g.parent[id] = root
	return root
}

// Observe records a claim about whichever entity owns the given identities,
// creating or merging entities as needed.
//
// This is the only way facts enter the graph. Callers hand over everything they
// know identifies the thing they just saw — an ARP reply knows an IP and a MAC,
// an mDNS answer knows a name and an IP — and the union-find does the rest. A
// discovery method never has to ask "have I seen this device before"; that
// question has exactly one implementation, here.
func (g *Graph) Observe(ids []Identity, claims ...Claim) *Entity {
	if len(ids) == 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.parent == nil {
		g.parent = map[Identity]Identity{}
		g.byID = map[Identity]*Entity{}
	}

	// Union every identity in this observation onto the first.
	root := g.find(ids[0])
	for _, id := range ids[1:] {
		other := g.find(id)
		if other == root {
			continue
		}
		g.parent[other] = root
		if a, b := g.byID[root], g.byID[other]; a != nil && b != nil && a != b {
			g.mergeEntities(a, b, ids)
		}
	}

	ent := g.byID[root]
	if ent == nil {
		ent = &Entity{ID: string(root)}
		g.Entities = append(g.Entities, ent)
	}
	for _, id := range ids {
		g.parent[id] = root
		g.byID[g.find(id)] = ent
		if !contains(ent.Identities, id) {
			ent.Identities = append(ent.Identities, id)
		}
	}
	g.byID[root] = ent

	for _, c := range claims {
		g.addClaim(ent, c)
	}
	sort.Slice(ent.Identities, func(i, j int) bool { return ent.Identities[i] < ent.Identities[j] })
	ent.ID = string(ent.Identities[0])
	return ent
}

// mergeEntities folds b into a, keeping every claim and recording why the two
// were ever considered the same device.
func (g *Graph) mergeEntities(a, b *Entity, because []Identity) {
	for _, id := range b.Identities {
		if !contains(a.Identities, id) {
			a.Identities = append(a.Identities, id)
		}
		g.byID[id] = a
	}
	for _, c := range b.Claims {
		g.addClaim(a, c)
	}
	a.Merges = append(a.Merges, fmt.Sprintf("%s + %s via %s", a.ID, b.ID, joinIdentities(because)))
	a.Conflicts = append(a.Conflicts, b.Conflicts...)

	for i, e := range g.Entities {
		if e == b {
			g.Entities = append(g.Entities[:i], g.Entities[i+1:]...)
			break
		}
	}
	for _, id := range b.Identities {
		g.byID[id] = a
	}
	// Edges referencing b now reference a.
	for i := range g.Edges {
		if g.Edges[i].From == b.ID {
			g.Edges[i].From = a.ID
		}
		if g.Edges[i].To == b.ID {
			g.Edges[i].To = a.ID
		}
	}
}

// addClaim applies the promotion and conflict rules from confidence.go.
func (g *Graph) addClaim(e *Entity, c Claim) {
	if c.Value == "" {
		return
	}
	if c.Source.At.IsZero() {
		c.Source.At = time.Now()
	}
	for i, existing := range e.Claims {
		if existing.Key != c.Key {
			continue
		}
		if existing.Value == c.Value {
			// Same value from a different method is corroboration; from the
			// same method it is one observation seen twice and changes nothing.
			if existing.Source.Method != c.Source.Method {
				e.Claims[i].Confidence = Corroborate(existing.Confidence, c.Confidence)
			}
			return
		}
		if !multiValued[c.Key] {
			// A real disagreement. Keep both, demote the winner, and say so in
			// the report rather than picking silently.
			// The conflict is recorded once per pair of claims, not once per
			// observation: a device seen five times disagreeing with itself in
			// the same way is one disagreement, and printing it five times
			// buries the other four findings.
			note := fmt.Sprintf("%s: %q (%s) vs %q (%s)",
				c.Key, existing.Value, existing.Source.Method, c.Value, c.Source.Method)
			if !containsString(e.Conflicts, note) {
				e.Conflicts = append(e.Conflicts, note)
				// Demote once per disagreement, not once per observation. A
				// chatty source repeating the same contradiction is still one
				// contradiction; demoting on every repeat would let a weak
				// source win a claim by saying it often enough.
				switch {
				case existing.Source.Method == "host" && c.Source.Method != "host":
					// The instrument disagreeing with someone else about the
					// instrument: a laptop knows its own name, and an
					// application announcing a different one on the network
					// does not get to demote that.
					c.Confidence = Conflict(c.Confidence)
				case c.Confidence > existing.Confidence:
					c.Confidence = Conflict(c.Confidence)
				default:
					e.Claims[i].Confidence = Conflict(existing.Confidence)
				}
			}
		}
	}
	e.Claims = append(e.Claims, c)
}

// multiValued lists the keys where two different values are both true rather
// than a contradiction. A device legitimately runs several services and answers
// to several names; it has only one vendor.
var multiValued = map[string]bool{
	"service":  true,
	"port":     true,
	"hostname": true,
	"model":    true,
	"label":    true,
	"note":     true,
}

// Entity returns the entity owning an identity, if any.
func (g *Graph) Entity(id Identity) *Entity {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.byID == nil {
		return nil
	}
	return g.byID[g.find(id)]
}

// AddEdge records a relationship, de-duplicating on (from, to, kind).
func (g *Graph) AddEdge(e Edge) {
	if e.From == "" || e.To == "" || e.From == e.To {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, existing := range g.Edges {
		if existing.From == e.From && existing.To == e.To && existing.Kind == e.Kind {
			if e.Confidence > existing.Confidence {
				g.Edges[i] = e
			}
			return
		}
	}
	g.Edges = append(g.Edges, e)
}

// AddFinding records a network-level observation.
func (g *Graph) AddFinding(f Finding) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Findings = append(g.Findings, f)
}

// Sort puts the graph into a stable order so that two surveys of an unchanged
// network produce byte-identical snapshots, which is what makes the drift
// report trustworthy.
func (g *Graph) Sort() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, e := range g.Entities {
		sort.Slice(e.Identities, func(i, j int) bool { return e.Identities[i] < e.Identities[j] })
		sort.SliceStable(e.Claims, func(i, j int) bool {
			if e.Claims[i].Key != e.Claims[j].Key {
				return e.Claims[i].Key < e.Claims[j].Key
			}
			if e.Claims[i].Confidence != e.Claims[j].Confidence {
				return e.Claims[i].Confidence > e.Claims[j].Confidence
			}
			return e.Claims[i].Value < e.Claims[j].Value
		})
		sort.Strings(e.Conflicts)
	}
	sort.Slice(g.Entities, func(i, j int) bool { return entityOrder(g.Entities[i]) < entityOrder(g.Entities[j]) })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		if g.Edges[i].To != g.Edges[j].To {
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].Kind < g.Edges[j].Kind
	})
	sort.SliceStable(g.Findings, func(i, j int) bool {
		return severityRank(g.Findings[i].Severity) < severityRank(g.Findings[j].Severity)
	})
}

// entityOrder sorts by IP numerically where possible, so 192.168.1.9 precedes
// 192.168.1.10 in every table CARTA prints.
func entityOrder(e *Entity) string {
	if addrs := e.Addresses(); len(addrs) > 0 {
		return padIP(addrs[0])
	}
	return "zzz" + e.ID
}

func padIP(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ip
	}
	var b strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&b, "%03s", p)
	}
	return b.String()
}

func severityRank(s string) int {
	switch s {
	case "risk":
		return 0
	case "warn":
		return 1
	default:
		return 2
	}
}

// Rebuild restores the union-find indexes after a Graph is loaded from JSON.
func (g *Graph) Rebuild() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.parent = map[Identity]Identity{}
	g.byID = map[Identity]*Entity{}
	for _, e := range g.Entities {
		if len(e.Identities) == 0 {
			continue
		}
		root := e.Identities[0]
		for _, id := range e.Identities {
			g.parent[id] = root
			g.byID[id] = e
		}
		g.byID[root] = e
	}
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func contains(ids []Identity, id Identity) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func joinIdentities(ids []Identity) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}
