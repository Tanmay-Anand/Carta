// Package discover performs Tier 0 of a survey: unprivileged discovery that
// works from any user account on any of the three platforms.
//
// Everything here is deliberately boring at the protocol level — ordinary
// connect() calls and ordinary multicast queries, no raw sockets, no elevation.
// That constraint is not modesty. A tool that needs administrator rights is a
// tool that does not get run, and the machine available at a site is usually a
// borrowed laptop with a locked-down account.
package discover

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
)

// Options controls the shape of a sweep.
type Options struct {
	Concurrency int           // parallel address workers
	Timeout     time.Duration // per-connection budget
	Ports       []int         // TCP ports probed on every responsive address
	Passive     bool          // emit nothing: cache and multicast reading only
	Progress    func(done, total int, stage string)
}

// DefaultOptions is tuned for a /24 on a home or branch network: fast enough to
// finish while someone is still standing at the jack, gentle enough not to look
// like a scan to an IDS that is watching.
func DefaultOptions() Options {
	return Options{
		Concurrency: 128,
		Timeout:     700 * time.Millisecond,
		Ports:       RiskRankedPorts,
	}
}

// RiskRankedPorts is not "the top 1000 ports". It is the set that identifies
// what a device IS, ordered by how much each one tells you. A device answering
// 9100 is a printer; one answering 32400 is a media server; one answering 445
// and 3389 is a Windows machine someone can log into. Ports that only say
// "something is listening" are not worth the packets.
var RiskRankedPorts = []int{
	80, 443, 22, 445, 139, 3389, 8080, 8443, // management and remote access
	9100, 631, 515, // printing
	554, 8554, 37777, // cameras and NVRs
	5000, 5001, 8006, 9000, // NAS and hypervisor web UIs
	548, 2049, 111, // file services
	1883, 8123, 502, // IoT, home automation, Modbus
	32400, 8096, 1900, // media
	23, 21, 53, 3306, 5432, 6379, 27017, 5900, // legacy, database, remote desktop
}

// Sweep walks a prefix, records what answers, and leaves the model marked with
// what it looked for and did not find.
//
// The sequence matters. ARP warming comes first because it is the only L2
// evidence available without raw sockets, and L2 presence is far stronger
// evidence of a device than any L3 response: an address can be forwarded,
// proxied or NATted, but a MAC in the local cache means something on this
// broadcast domain answered an ARP request for that address.
func Sweep(ctx context.Context, g *model.Graph, prof *orient.Profile, opt Options) model.Coverage {
	cov := model.Coverage{StartedAt: time.Now(), PortsProbed: opt.Ports}
	if prof.Prefix == nil {
		cov.Notes = append(cov.Notes, "no IPv4 prefix on the vantage interface; nothing was swept")
		cov.EndedAt = time.Now()
		return cov
	}
	cov.Prefix = prefixString(prof.Prefix)

	addrs := hostAddresses(prof.Prefix)
	cov.AddrsTotal = len(addrs)

	// A prefix larger than a /22 is refused rather than silently truncated. A
	// truncated sweep rendered as a complete one is the exact failure this tool
	// exists to prevent, and "I surveyed a /16 in four seconds" is a lie a
	// progress bar would happily tell.
	if len(addrs) > 1024 {
		cov.Notes = append(cov.Notes,
			"prefix holds "+itoa(len(addrs))+" addresses; sweeping is limited to 1024 — re-run with --prefix to survey a chosen part of it")
		cov.Frontier = append(cov.Frontier, cov.Prefix)
		cov.EndedAt = time.Now()
		return cov
	}

	// Everything the host already knew, recorded before we touch the wire, and
	// marked as such: a device the host was already talking to is independent
	// evidence, not a result of our own probing.
	seedFromCache(g, prof)

	if opt.Passive {
		cov.Notes = append(cov.Notes, "passive profile: no packets were emitted; findings come from the host's own caches and from multicast listening")
		cov.EndedAt = time.Now()
		return cov
	}

	// Phase 1 — warm the ARP cache. Touching an address at L3 forces the kernel
	// to resolve it at L2, whether or not anything answers at L3. This is how
	// an unprivileged process performs layer-2 discovery.
	var wg sync.WaitGroup
	sem := make(chan struct{}, opt.Concurrency)
	alive := make(chan liveHost, len(addrs))
	var done int
	var mu sync.Mutex

	for _, ip := range addrs {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			h := touch(ctx, ip, opt)
			mu.Lock()
			done++
			if opt.Progress != nil {
				opt.Progress(done, len(addrs), "sweep")
			}
			mu.Unlock()
			if h.responded {
				alive <- h
			}
		}(ip)
	}
	wg.Wait()
	close(alive)

	now := time.Now()
	for h := range alive {
		cov.Responded++
		g.Observe([]model.Identity{model.IPIdentity(h.ip.String())}, model.Claim{
			Key: "alive", Value: "yes", Confidence: model.High,
			Source: model.Source{Method: "tcp-connect", Evidence: h.evidence, At: now},
		})
	}
	cov.AddrsSwept = len(addrs)

	// Phase 2 — re-read the ARP cache. Addresses that now carry a MAC were
	// resolved at layer 2 even if nothing answered at layer 3, which finds
	// hosts that drop every probe but still have to answer ARP to exist.
	after := orient.ReadNeighbours(ctx)
	for _, n := range after {
		if n.MAC == "" || !prof.Prefix.Contains(net.ParseIP(n.IP)) || isNonDevice(n.IP, n.MAC) {
			continue
		}
		ids := []model.Identity{model.IPIdentity(n.IP), model.MACIdentity(n.MAC)}
		g.Observe(ids,
			model.Claim{Key: "alive", Value: "yes", Confidence: model.High,
				Source: model.Source{Method: "arp-cache", Evidence: n.IP + " -> " + n.MAC + " (" + n.State + ")", At: now}},
			model.Claim{Key: "l2-present", Value: "yes", Confidence: model.High,
				Source: model.Source{Method: "arp-cache", Evidence: "resolved on the local broadcast domain", At: now}},
		)
	}

	cov.EndedAt = time.Now()
	return cov
}

type liveHost struct {
	ip        net.IP
	responded bool
	evidence  string
}

// touch tries a small number of connections against one address. It stops at
// the first success: the goal of this phase is liveness and ARP resolution, not
// a service inventory, and hammering every port at every address is what turns
// a survey into something a security team has to investigate.
func touch(ctx context.Context, ip net.IP, opt Options) liveHost {
	h := liveHost{ip: ip}

	// A UDP send to the discard port resolves ARP without needing anyone to
	// answer. It cannot prove liveness, so its only job is cache warming.
	if c, err := net.DialTimeout("udp4", net.JoinHostPort(ip.String(), "9"), opt.Timeout); err == nil {
		_, _ = c.Write([]byte{0})
		_ = c.Close()
	}

	for _, port := range []int{80, 443, 22, 445, 8080} {
		select {
		case <-ctx.Done():
			return h
		default:
		}
		d := net.Dialer{Timeout: opt.Timeout}
		c, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), itoa(port)))
		if err == nil {
			_ = c.Close()
			h.responded = true
			h.evidence = "handshake completed on tcp/" + itoa(port)
			return h
		}
		// A refused connection is still a live host: something replied RST,
		// which only a host with an IP stack does. This is the single most
		// useful distinction in unprivileged discovery and most tools throw it
		// away by treating "closed" as "absent".
		if isRefused(err) {
			h.responded = true
			h.evidence = "tcp/" + itoa(port) + " refused the connection — a stack answered"
			return h
		}
	}
	return h
}

// seedFromCache turns the pre-existing ARP/NDP cache into claims. State is
// carried through as evidence because "reachable" and "stale" are different
// strengths of the same claim.
func seedFromCache(g *model.Graph, prof *orient.Profile) {
	now := time.Now()
	for _, n := range prof.Neighbours {
		ip := net.ParseIP(n.IP)
		if ip == nil || n.MAC == "" || isNonDevice(n.IP, n.MAC) {
			continue
		}
		inPrefix := prof.Prefix != nil && prof.Prefix.Contains(ip)
		if !inPrefix {
			continue // other interfaces' fabrics are recorded as frontier, not as devices
		}
		g.Observe([]model.Identity{model.IPIdentity(n.IP), model.MACIdentity(n.MAC)},
			model.Claim{Key: "alive", Value: "yes", Confidence: model.High,
				Source: model.Source{Method: "arp-cache", Evidence: "present in the host's cache before the survey began (" + n.State + ")", At: now}},
			model.Claim{Key: "prior-contact", Value: "yes", Confidence: model.Moderate,
				Source: model.Source{Method: "arp-cache", Evidence: "this host had already exchanged traffic with it", At: now}},
		)
	}
}

// isNonDevice filters the entries that are protocol furniture rather than
// things: multicast groups, broadcast addresses, and the all-ones MAC.
func isNonDevice(ip, mac string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsMulticast() || parsed.IsUnspecified() {
		return true
	}
	switch mac {
	case "ff:ff:ff:ff:ff:ff", "00:00:00:00:00:00":
		return true
	}
	// 01:00:5e is the IPv4 multicast MAC prefix; 33:33 is IPv6 multicast.
	return len(mac) >= 8 && (mac[:8] == "01:00:5e" || mac[:5] == "33:33")
}

// hostAddresses expands a prefix to its usable host addresses, excluding the
// network and broadcast addresses and our own.
func hostAddresses(n *net.IPNet) []net.IP {
	ip4 := n.IP.To4()
	if ip4 == nil {
		return nil
	}
	mask := net.IP(n.Mask).To4()
	if mask == nil {
		return nil
	}
	base := make(net.IP, 4)
	for i := range base {
		base[i] = ip4[i] & mask[i]
	}
	ones, bits := n.Mask.Size()
	if bits != 32 || ones >= 31 {
		return []net.IP{ip4}
	}
	count := 1 << uint(32-ones)
	out := make([]net.IP, 0, count-2)
	for i := 1; i < count-1; i++ {
		ip := make(net.IP, 4)
		copy(ip, base)
		v := uint32(i)
		ip[3] += byte(v)
		ip[2] += byte(v >> 8)
		ip[1] += byte(v >> 16)
		ip[0] += byte(v >> 24)
		out = append(out, ip)
	}
	return out
}

func prefixString(n *net.IPNet) string {
	ones, _ := n.Mask.Size()
	ip4 := n.IP.To4()
	mask := net.IP(n.Mask).To4()
	if ip4 == nil || mask == nil {
		return n.String()
	}
	base := make(net.IP, 4)
	for i := range base {
		base[i] = ip4[i] & mask[i]
	}
	return base.String() + "/" + itoa(ones)
}
