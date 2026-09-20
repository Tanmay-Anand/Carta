// Package survey runs the phases in order and assembles one model from them.
//
// The order is not arbitrary. Orientation must precede discovery, because a
// discovery result is meaningless without knowing where it was taken from.
// Multicast listening runs alongside the sweep rather than after it, because
// the sweep's traffic itself provokes announcements. Entity resolution happens
// continuously inside the model rather than as a post-pass, and findings are
// computed last, once every claim is in.
package survey

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/discover"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
)

// Options are the knobs a survey exposes on the command line.
type Options struct {
	Passive     bool
	Concurrency int
	Timeout     time.Duration
	OUIPath     string
	Progress    func(stage string, done, total int)
}

// stage announces a phase boundary through the same channel as progress, so a
// caller that is drawing a progress line and a caller that is writing a log
// file both learn the same things from one callback.
func (o Options) stage(name string) {
	if o.Progress != nil {
		o.Progress(name, 0, 0)
	}
}

// Run performs a complete survey and returns the model it built.
func Run(ctx context.Context, opt Options) (*model.Graph, *orient.Profile, error) {
	opt.stage("orient")
	prof, err := orient.Run(ctx)
	if err != nil {
		return nil, nil, err
	}

	g := model.NewGraph(time.Now().UTC().Format("20060102T150405Z"))
	g.Vantage = prof.Vantage()
	g.Meta["tool"] = "carta"
	g.Meta["tier"] = tierLabel(opt.Passive)

	recordVantage(g, prof)

	dopt := discover.DefaultOptions()
	if opt.Concurrency > 0 {
		dopt.Concurrency = opt.Concurrency
	}
	if opt.Timeout > 0 {
		dopt.Timeout = opt.Timeout
	}
	dopt.Passive = opt.Passive
	if opt.Progress != nil {
		dopt.Progress = func(done, total int, stage string) { opt.Progress(stage, done, total) }
	}

	// Multicast listening runs concurrently with the sweep. Devices announce
	// themselves when they see new traffic on the segment, so the sweep is not
	// just a consumer of time here — it is what makes the listening productive.
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		protocols []string
	)
	opt.stage("listen")
	wg.Add(2)
	go func() {
		defer wg.Done()
		p := discover.MDNS(ctx, g, 6*time.Second)
		mu.Lock()
		protocols = append(protocols, p...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		p := discover.SSDP(ctx, g, 6*time.Second)
		mu.Lock()
		protocols = append(protocols, p...)
		mu.Unlock()
	}()

	opt.stage("sweep")
	cov := discover.Sweep(ctx, g, prof, dopt)
	wg.Wait()

	cov.Protocols = append(cov.Protocols, protocols...)
	cov.Protocols = append(cov.Protocols, "arp-cache")

	// Everything from here on only makes sense against addresses that answered
	// something, so the target list is derived from the model rather than from
	// the address range: probing 254 addresses for 30 ports each would be a
	// scan, not a survey.
	targets := liveAddresses(g)

	// Responded is counted from the model rather than from the sweep's own
	// tally: a host discovered only at layer 2, or only because it announced
	// itself over mDNS while the sweep was running, responded to this survey
	// every bit as much as one that completed a TCP handshake.
	cov.Responded = len(targets)

	if !opt.Passive {
		opt.stage("probe")
		discover.Probe(ctx, g, targets, dopt)
		opt.stage("nbns")
		if p := discover.NetBIOS(ctx, g, targets, dopt.Timeout); len(p) > 0 {
			cov.Protocols = append(cov.Protocols, p...)
		}
		opt.stage("rdns")
		found, asked := discover.ReverseDNS(ctx, g, targets, prof.Resolvers, dopt.Timeout)
		cov.Protocols = append(cov.Protocols, "rdns")
		if asked > 0 {
			g.Meta["ptr_coverage"] = fmt.Sprintf("%d/%d", found, asked)
		}
	}

	opt.stage("resolve")
	table, err := discover.LoadOUI(opt.OUIPath)
	if err != nil && opt.OUIPath != "" {
		cov.Notes = append(cov.Notes, "OUI pack at "+opt.OUIPath+" could not be read; using the built-in table")
	}
	discover.Vendors(g, table)

	// Frontier: prefixes this host can see referenced but that were deliberately
	// not surveyed. Recording them is what lets the map show an edge of the
	// world rather than implying there is nothing beyond it.
	for _, ifc := range prof.Interfaces {
		if !ifc.Up || strings.EqualFold(ifc.Name, prof.Interface) {
			continue
		}
		for _, a := range ifc.Addrs {
			if strings.Contains(a, ":") || strings.HasPrefix(a, "127.") {
				continue
			}
			cov.Frontier = append(cov.Frontier, a+" (on "+ifc.Name+", not surveyed)")
		}
	}

	g.Coverage = append(g.Coverage, cov)
	buildEdges(g, prof)
	findings(g, prof, cov)
	g.Sort()
	return g, prof, nil
}

func tierLabel(passive bool) string {
	if passive {
		return "P (passive only)"
	}
	return "P + 0 (unprivileged)"
}

// recordVantage puts the instrument itself into the model. The host CARTA runs
// on is a device on the network like any other, and leaving it out produces a
// map with a hole exactly where the observer stands.
func recordVantage(g *model.Graph, prof *orient.Profile) {
	now := time.Now()
	if prof.IP == nil {
		return
	}
	ids := []model.Identity{model.IPIdentity(prof.IP.String())}
	if prof.MAC != "" {
		ids = append(ids, model.MACIdentity(prof.MAC))
	}
	if prof.Host != "" {
		ids = append(ids, model.HostIdentity(prof.Host))
	}
	g.Observe(ids,
		model.Claim{Key: "hostname", Value: prof.Host, Confidence: model.Asserted,
			Source: model.Source{Method: "host", Evidence: "the survey ran on this machine", At: now}},
		model.Claim{Key: "class", Value: "survey vantage", Confidence: model.Asserted,
			Source: model.Source{Method: "host", Evidence: "this is the instrument", At: now}},
		model.Claim{Key: "os", Value: prof.OS, Confidence: model.Asserted,
			Source: model.Source{Method: "host", Evidence: "reported by the Go runtime", At: now}},
		model.Claim{Key: "alive", Value: "yes", Confidence: model.Asserted,
			Source: model.Source{Method: "host", Evidence: "self", At: now}},
	)

	if prof.Gateway != "" {
		g.Observe([]model.Identity{model.IPIdentity(prof.Gateway)},
			model.Claim{Key: "class", Value: "router / gateway", Confidence: model.High,
				Source: model.Source{Method: "route-table", Evidence: "carries this host's default route", At: now}},
			model.Claim{Key: "alive", Value: "yes", Confidence: model.High,
				Source: model.Source{Method: "route-table", Evidence: "named as the default gateway", At: now}})
	}
	for _, r := range prof.Resolvers {
		if strings.Contains(r, ":") {
			continue // IPv6 resolvers are recorded on the vantage, not as v4 entities
		}
		g.Observe([]model.Identity{model.IPIdentity(r)},
			model.Claim{Key: "service", Value: "dns (53/udp)", Confidence: model.High,
				Source: model.Source{Method: "host", Evidence: "configured as a resolver for this host", At: now}})
	}
	if prof.DHCPServer != "" && !strings.Contains(prof.DHCPServer, ":") {
		g.Observe([]model.Identity{model.IPIdentity(prof.DHCPServer)},
			model.Claim{Key: "service", Value: "dhcp (67/udp)", Confidence: model.High,
				Source: model.Source{Method: "host", Evidence: "issued this host's lease", At: now}})
	}
}

// buildEdges turns what we know into relationships. Each edge carries the
// confidence of the weakest thing it depends on.
func buildEdges(g *model.Graph, prof *orient.Profile) {
	now := time.Now()
	self := g.Entity(model.IPIdentity(ipString(prof)))
	gw := g.Entity(model.IPIdentity(prof.Gateway))

	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v == "" {
			continue
		}
		if self != nil && e != self {
			g.AddEdge(model.Edge{From: self.ID, To: e.ID, Kind: model.EdgeVantage, Confidence: model.High,
				Source: model.Source{Method: "survey", Evidence: "observed from this vantage", At: now}})
		}
		if gw != nil && e != gw {
			// Every host on this prefix uses this gateway — an inference from
			// the routing table of ONE host, which is why it is Moderate: a
			// second router on the same segment serving half the devices is
			// exactly the misconfiguration this cannot see.
			g.AddEdge(model.Edge{From: gw.ID, To: e.ID, Kind: model.EdgeGateway, Confidence: model.Moderate,
				Source: model.Source{Method: "route-table", Evidence: "this host's default route; assumed shared by the prefix", At: now}})
		}
	}

	// Shared broadcast domain: everything that resolved at layer 2 from this
	// vantage is, by definition, on the same segment as the vantage.
	if self != nil {
		for _, e := range g.Entities {
			if e == self {
				continue
			}
			if v, _ := e.Best("l2-present"); v == "yes" {
				g.AddEdge(model.Edge{From: self.ID, To: e.ID, Kind: model.EdgeSameL2, Confidence: model.High,
					Source: model.Source{Method: "arp-cache", Evidence: "resolved at layer 2 from this host", At: now}})
			}
		}
	}
	for _, r := range prof.Resolvers {
		if res := g.Entity(model.IPIdentity(r)); res != nil && self != nil {
			g.AddEdge(model.Edge{From: res.ID, To: self.ID, Kind: model.EdgeResolver, Confidence: model.High,
				Source: model.Source{Method: "host", Evidence: "configured resolver", At: now}})
		}
	}
	if d := g.Entity(model.IPIdentity(prof.DHCPServer)); d != nil && self != nil {
		g.AddEdge(model.Edge{From: d.ID, To: self.ID, Kind: model.EdgeDHCP, Confidence: model.High,
			Source: model.Source{Method: "host", Evidence: "issued this host's lease", At: now}})
	}
}

func ipString(p *orient.Profile) string {
	if p.IP == nil {
		return ""
	}
	return p.IP.String()
}

func liveAddresses(g *model.Graph) []string {
	var out []string
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		out = append(out, e.Addresses()...)
	}
	return out
}
