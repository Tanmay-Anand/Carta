package model

import "testing"

// A vantage that cannot read the ARP cache (Android) produces the same claims
// minus the layer-2 ones. Nothing in the model may treat the missing method as
// a signal: no claim may lose confidence, no device may go missing, and the
// only differences are the claims ARP itself would have made.
func TestMissingARPIsNoEvidenceNotNegativeEvidence(t *testing.T) {
	l3 := func(g *Graph) {
		g.Observe([]Identity{IPIdentity("192.168.0.10")},
			claim("alive", "yes", High, "tcp-connect"),
			claim("service", "ssh (22/tcp)", High, "tcp-connect"))
		g.Observe([]Identity{IPIdentity("192.168.0.10"), HostIdentity("nas")},
			claim("hostname", "nas", Asserted, "mdns"),
			claim("class", "storage", Asserted, "mdns"))
		g.Observe([]Identity{IPIdentity("192.168.0.11")},
			claim("alive", "yes", High, "tcp-connect"))
	}

	withARP := NewGraph("arp")
	l3(withARP)
	macs := map[string]string{"192.168.0.10": "b8:27:eb:00:00:10", "192.168.0.11": "b8:27:eb:00:00:11"}
	for ip, mac := range macs {
		withARP.Observe([]Identity{IPIdentity(ip), MACIdentity(mac)},
			claim("alive", "yes", High, "arp-cache"),
			claim("l2-present", "yes", High, "arp-cache"))
	}
	without := NewGraph("noarp")
	l3(without)

	if len(withARP.Entities) != len(without.Entities) {
		t.Fatalf("device count changed without ARP: %d vs %d", len(withARP.Entities), len(without.Entities))
	}
	for ip := range macs {
		a, b := withARP.Entity(IPIdentity(ip)), without.Entity(IPIdentity(ip))
		if a == nil || b == nil {
			t.Fatalf("%s missing: with=%v without=%v", ip, a, b)
		}
		for _, key := range []string{"alive", "hostname", "class", "service"} {
			av, ac := a.Best(key)
			bv, bc := b.Best(key)
			if av != bv || ac != bc {
				t.Errorf("%s %s: with ARP %q/%v, without %q/%v", ip, key, av, ac, bv, bc)
			}
		}
		if v, _ := b.Best("l2-present"); v != "" {
			t.Errorf("%s: l2-present claimed without ARP", ip)
		}
		if len(b.MACs()) != 0 {
			t.Errorf("%s: MAC learned without ARP: %v", ip, b.MACs())
		}
	}
}
