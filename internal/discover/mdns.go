package discover

import (
	"context"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Multicast service discovery is the richest unprivileged identity source on a
// modern network, and the one most scanners underuse. A port scan tells you
// something listens on 8009; mDNS tells you it is a Chromecast called "Living
// Room TV" running a particular firmware, because the device announced it
// itself.
//
// That is also why these claims enter the model as Asserted rather than High:
// the device said so. It is the strongest evidence available here, and it is
// still a claim by an unauthenticated party on a broadcast network — a printer
// that calls itself a printer is usually a printer, but nothing verifies it.

// mdnsServiceTypes is a hand-picked list rather than an exhaustive one. Each
// entry identifies a device CLASS, which is what the inventory needs; querying
// a hundred obscure types would add packets and no information.
var mdnsServiceTypes = []string{
	"_services._dns-sd._udp.local.", // the meta-query: "what service types exist here"
	"_device-info._tcp.local.",
	"_workstation._tcp.local.",
	"_http._tcp.local.",
	"_smb._tcp.local.",
	"_afpovertcp._tcp.local.",
	"_ipp._tcp.local.",
	"_ipps._tcp.local.",
	"_printer._tcp.local.",
	"_pdl-datastream._tcp.local.",
	"_scanner._tcp.local.",
	"_airplay._tcp.local.",
	"_raop._tcp.local.",
	"_googlecast._tcp.local.",
	"_spotify-connect._tcp.local.",
	"_sonos._tcp.local.",
	"_hap._tcp.local.",
	"_homekit._tcp.local.",
	"_ssh._tcp.local.",
	"_sftp-ssh._tcp.local.",
	"_nvstream._tcp.local.",
	"_rfb._tcp.local.",
	"_daap._tcp.local.",
	"_nfs._tcp.local.",
}

// serviceClass maps an mDNS service type to the kind of thing that offers it.
// Only unambiguous mappings are listed: _http._tcp says nothing about what a
// device IS, so it is absent, while _pdl-datastream._tcp is a print queue and
// nothing else.
var serviceClass = map[string]string{
	"_ipp":             "printer",
	"_ipps":            "printer",
	"_printer":         "printer",
	"_pdl-datastream":  "printer",
	"_scanner":         "scanner",
	"_airplay":         "media receiver",
	"_raop":            "media receiver (AirPlay audio)",
	"_googlecast":      "media receiver (Chromecast)",
	"_spotify-connect": "media receiver",
	"_sonos":           "speaker",
	"_hap":             "smart-home accessory",
	"_homekit":         "smart-home accessory",
	"_smb":             "file server",
	"_afpovertcp":      "file server (Apple)",
	"_nfs":             "file server",
	"_nvstream":        "game streaming host",
	"_workstation":     "workstation",
	"_rfb":             "remote desktop host",
}

// MDNS queries the local link for service announcements and folds every answer
// into the graph.
//
// The socket is an ordinary unbound UDP socket, not a bind of port 5353: on
// Windows and macOS the OS already runs an mDNS responder holding that port,
// and fighting it would make CARTA refuse to run on exactly the machines it is
// meant to run on. Queries therefore set the QU bit asking for a unicast reply,
// which well-behaved responders honour.
func MDNS(ctx context.Context, g *model.Graph, window time.Duration) []string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	for _, t := range mdnsServiceTypes {
		if msg, err := buildQuery(t, dnsmessage.TypePTR, true); err == nil {
			_, _ = conn.WriteToUDP(msg, group)
		}
	}

	deadline := time.Now().Add(window)
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 9000)
	seen := map[string]bool{}
	var protocols []string

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return protocols
		default:
		}
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		if n == 0 || src == nil {
			continue
		}
		absorb(g, src.IP.String(), buf[:n], seen)
		protocols = appendOnce(protocols, "mdns")
	}
	return protocols
}

// absorb folds one mDNS response into the graph.
//
// Records are attributed to the address that sent the packet, not to whatever
// address an A record claims. A responder may answer on behalf of another
// device (that is what a proxy responder is for), and believing the packet's
// contents over its origin is how one chatty device gets credited with half the
// network's identities.
//
// Each section is taken whole rather than record by record. Stepping the
// parser's state machine by hand means every unhandled record type needs the
// matching skip for the section it is in — get that wrong and the parser stops
// advancing while the loop keeps going, which is a hang on a live network and
// silence in a unit test. AllAnswers and AllAdditionals cannot be desynchronised
// that way.
func absorb(g *model.Graph, srcIP string, pkt []byte, seen map[string]bool) {
	var p dnsmessage.Parser
	if _, err := p.Start(pkt); err != nil {
		return
	}
	if err := p.SkipAllQuestions(); err != nil {
		return
	}

	answers, err := p.AllAnswers()
	if err != nil && len(answers) == 0 {
		return
	}
	if err := p.SkipAllAuthorities(); err == nil {
		// Additionals carry the A, SRV and TXT records that give an answer its
		// meaning, so they are worth reading rather than skipping.
		if extra, err := p.AllAdditionals(); err == nil || len(extra) > 0 {
			answers = append(answers, extra...)
		}
	}

	now := time.Now()
	ids := []model.Identity{model.IPIdentity(srcIP)}
	for _, r := range answers {
		consumeRecord(g, ids, r, now, seen)
	}
}

func consumeRecord(g *model.Graph, ids []model.Identity, r dnsmessage.Resource, now time.Time, seen map[string]bool) {
	name := strings.TrimSuffix(r.Header.Name.String(), ".")

	switch body := r.Body.(type) {
	case *dnsmessage.AResource:
		ip := net.IP(body.A[:]).String()
		key := "A|" + name + "|" + ip
		if seen[key] {
			return
		}
		seen[key] = true
		g.Observe([]model.Identity{model.IPIdentity(ip), model.HostIdentity(name)},
			model.Claim{Key: "hostname", Value: hostLabel(name), Confidence: model.Asserted,
				Source: model.Source{Method: "mdns", Evidence: name + " A " + ip, At: now}})

	case *dnsmessage.PTRResource:
		target := strings.TrimSuffix(body.PTR.String(), ".")
		if class := classOf(target); class != "" {
			g.Observe(ids, model.Claim{Key: "class", Value: class, Confidence: model.Asserted,
				Source: model.Source{Method: "mdns", Evidence: target, At: now}})
		}
		if svc := serviceLabel(target); svc != "" {
			g.Observe(ids, model.Claim{Key: "service", Value: svc, Confidence: model.Asserted,
				Source: model.Source{Method: "mdns", Evidence: target, At: now}})
		}
		// An instance name is a label an APPLICATION chose, not the device's
		// name: a laptop running Spotify announces "Spotify Desktop Launcher",
		// and filing that as the hostname renames the machine after whatever
		// software happened to be open. It is recorded as a label, and only
		// A and SRV records — which name hosts — set a hostname.
		if inst := instanceName(target); inst != "" {
			g.Observe(ids, model.Claim{Key: "label", Value: inst, Confidence: model.Asserted,
				Source: model.Source{Method: "mdns", Evidence: target, At: now}})
		}

	case *dnsmessage.SRVResource:
		target := strings.TrimSuffix(body.Target.String(), ".")
		if target != "" {
			g.Observe(append(ids, model.HostIdentity(target)),
				model.Claim{Key: "hostname", Value: hostLabel(target), Confidence: model.Asserted,
					Source: model.Source{Method: "mdns", Evidence: name + " SRV " + target, At: now}})
		}

	case *dnsmessage.TXTResource:
		for _, kv := range body.TXT {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || v == "" {
				continue
			}
			switch strings.ToLower(k) {
			case "md", "model", "ty", "usb_mdl", "am":
				// "ty" is the human-readable model on printers, "md" on
				// Chromecasts, "am" on AirPlay receivers.
				g.Observe(ids, model.Claim{Key: "model", Value: v, Confidence: model.Asserted,
					Source: model.Source{Method: "mdns", Evidence: kv, At: now}})
			case "usb_mfg", "mfg", "manufacturer":
				g.Observe(ids, model.Claim{Key: "vendor", Value: v, Confidence: model.Asserted,
					Source: model.Source{Method: "mdns", Evidence: kv, At: now}})
			case "os":
				g.Observe(ids, model.Claim{Key: "os", Value: v, Confidence: model.Asserted,
					Source: model.Source{Method: "mdns", Evidence: kv, At: now}})
			}
		}
	}
}

// buildQuery assembles a one-question DNS packet. unicast sets the QU bit,
// asking the responder to reply directly to our ephemeral port rather than to
// the multicast group.
func buildQuery(name string, qtype dnsmessage.Type, unicast bool) ([]byte, error) {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, err
	}
	class := dnsmessage.ClassINET
	if unicast {
		class = dnsmessage.Class(uint16(dnsmessage.ClassINET) | 0x8000)
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{RecursionDesired: false})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: n, Type: qtype, Class: class}); err != nil {
		return nil, err
	}
	return b.Finish()
}

// classOf maps "Something._ipp._tcp.local" to "printer".
func classOf(target string) string {
	for prefix, class := range serviceClass {
		if strings.Contains(target, prefix+"._tcp") || strings.Contains(target, prefix+"._udp") {
			return class
		}
	}
	return ""
}

// serviceLabel extracts the bare service type for the inventory's service list.
func serviceLabel(target string) string {
	i := strings.Index(target, "._tcp")
	if i < 0 {
		i = strings.Index(target, "._udp")
	}
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(target[:i], "._")
	if start >= 0 {
		return strings.TrimPrefix(target[start+1:i], "_") + " (mDNS)"
	}
	return strings.TrimPrefix(target[:i], "_") + " (mDNS)"
}

// instanceName returns the human label a device gave itself, or "" when the
// name is purely structural. Service types are not device names, and treating
// "_http._tcp.local" as a hostname is how inventories end up full of protocol
// jargon where the device names should be.
func instanceName(target string) string {
	if strings.HasPrefix(target, "_") {
		return ""
	}
	i := strings.Index(target, "._")
	if i <= 0 {
		return ""
	}
	name := strings.ReplaceAll(target[:i], `\032`, " ")
	if name == "" || strings.HasPrefix(name, "_") {
		return ""
	}
	return name
}

// hostLabel strips the ".local" suffix for display while keeping the name.
func hostLabel(n string) string {
	n = strings.TrimSuffix(n, ".")
	return strings.TrimSuffix(n, ".local")
}

func appendOnce(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
