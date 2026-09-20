package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func claim(key, val string, conf Confidence, method string) Claim {
	return Claim{Key: key, Value: val, Confidence: conf,
		Source: Source{Method: method, At: time.Unix(1700000000, 0)}}
}

func TestCorroborationPromotesByOneAndNeverReachesAsserted(t *testing.T) {
	if got := Corroborate(Low, Low); got != Moderate {
		t.Errorf("two weak sources agreeing: got %v, want moderate", got)
	}
	if got := Corroborate(Moderate, Low); got != High {
		t.Errorf("inferred + weak: got %v, want observed", got)
	}
	// Agreement between third parties can never make something the device's own
	// statement about itself.
	if got := Corroborate(High, High); got != High {
		t.Errorf("two observations: got %v, want observed (not asserted)", got)
	}
	if got := Corroborate(Asserted, Moderate); got != Asserted {
		t.Errorf("asserted stays asserted: got %v", got)
	}
	if got := Corroborate(Unknown, Moderate); got != Moderate {
		t.Errorf("unknown must not promote: got %v", got)
	}
}

func TestConflictDemotesTheWinner(t *testing.T) {
	if got := Conflict(Asserted); got != High {
		t.Errorf("got %v, want observed", got)
	}
	if got := Conflict(Low); got != Low {
		t.Errorf("weak cannot demote below weak: got %v", got)
	}
}

func TestObserveMergesEntitiesThroughSharedIdentity(t *testing.T) {
	g := NewGraph("t")
	// Seen first by address alone, then by name, then an ARP reply ties the
	// address to a MAC — three observations, one device.
	g.Observe([]Identity{IPIdentity("192.168.0.5")}, claim("alive", "yes", High, "tcp-connect"))
	g.Observe([]Identity{HostIdentity("nas.local")}, claim("class", "file server", Asserted, "mdns"))
	g.Observe([]Identity{IPIdentity("192.168.0.5"), MACIdentity("AA:BB:CC:DD:EE:FF"), HostIdentity("nas.local")},
		claim("hostname", "nas", Asserted, "mdns"))

	if len(g.Entities) != 1 {
		t.Fatalf("want 1 entity after merge, got %d", len(g.Entities))
	}
	e := g.Entities[0]
	if v, _ := e.Best("class"); v != "file server" {
		t.Errorf("claims from the merged-away entity were lost: class = %q", v)
	}
	if len(e.MACs()) != 1 || e.MACs()[0] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC should be normalised to lower case: %v", e.MACs())
	}
	if len(e.Merges) == 0 {
		t.Error("a merge must leave an audit trail")
	}
}

func TestDisagreementIsKeptDemotedAndRecordedOnce(t *testing.T) {
	g := NewGraph("t")
	ids := []Identity{IPIdentity("10.0.0.1")}
	g.Observe(ids, claim("vendor", "TP-Link", Asserted, "upnp-description"))
	for i := 0; i < 3; i++ {
		g.Observe(ids, claim("vendor", "Tenda", Moderate, "oui"))
	}

	e := g.Entities[0]
	if len(e.Conflicts) != 1 {
		t.Errorf("the same disagreement must be recorded once, got %d: %v", len(e.Conflicts), e.Conflicts)
	}
	// Both values survive; nothing is silently discarded.
	if len(e.All("vendor")) < 2 {
		t.Errorf("both sides of a disagreement must be kept, got %v", e.All("vendor"))
	}
	v, conf := e.Best("vendor")
	if v != "TP-Link" {
		t.Errorf("the stronger source should win the value, got %q", v)
	}
	if conf != High {
		t.Errorf("a contested claim must be demoted: got %v, want observed", conf)
	}
}

func TestSelfKnowledgeBeatsAnApplicationAnnouncement(t *testing.T) {
	// The regression this protects: a laptop running Spotify announces
	// "Spotify Desktop Launcher" over mDNS, which was overriding the machine's
	// own name in the inventory.
	g := NewGraph("t")
	ids := []Identity{IPIdentity("192.168.0.7")}
	g.Observe(ids, claim("hostname", "MSI", Asserted, "host"))
	later := Claim{Key: "hostname", Value: "Spotify Desktop Launcher", Confidence: Asserted,
		Source: Source{Method: "mdns", At: time.Unix(1700000900, 0)}}
	g.Observe(ids, later)

	if v, _ := g.Entities[0].Best("hostname"); v != "MSI" {
		t.Errorf("the host's own name must win, got %q", v)
	}
}

func TestLabelFallsBackInOrder(t *testing.T) {
	g := NewGraph("t")
	e := g.Observe([]Identity{IPIdentity("10.0.0.9"), MACIdentity("aa:bb:cc:00:00:01")})
	if got := e.Label(); got != "10.0.0.9" {
		t.Errorf("with no names, the address is the label: %q", got)
	}
	g.Observe([]Identity{IPIdentity("10.0.0.9")}, claim("label", "Kitchen Speaker", Asserted, "mdns"))
	if got := e.Label(); got != "Kitchen Speaker" {
		t.Errorf("an application label beats an address: %q", got)
	}
	g.Observe([]Identity{IPIdentity("10.0.0.9")}, claim("hostname", "sonos-1", Asserted, "rdns"))
	if got := e.Label(); got != "sonos-1" {
		t.Errorf("a hostname beats an application label: %q", got)
	}
}

func TestSnapshotsOfAnUnchangedNetworkAreByteIdentical(t *testing.T) {
	// Drift reporting rests on this: if the serialisation wobbles, every survey
	// shows spurious change and nobody reads the third one.
	build := func() *Graph {
		g := NewGraph("t")
		g.StartedAt = time.Unix(1700000000, 0).UTC()
		for _, ip := range []string{"192.168.0.10", "192.168.0.2", "192.168.0.100"} {
			g.Observe([]Identity{IPIdentity(ip)},
				claim("alive", "yes", High, "tcp-connect"),
				claim("service", "ssh (22/tcp)", Moderate, "tcp-connect"))
		}
		g.Sort()
		return g
	}
	a, _ := json.Marshal(build())
	b, _ := json.Marshal(build())
	if string(a) != string(b) {
		t.Error("two identical surveys serialised differently")
	}
	// And addresses sort numerically, not lexically.
	g := build()
	if got := g.Entities[0].Addresses()[0]; got != "192.168.0.2" {
		t.Errorf("addresses must sort numerically, first was %q", got)
	}
	if got := g.Entities[2].Addresses()[0]; got != "192.168.0.100" {
		t.Errorf("192.168.0.100 should sort last, got %q", got)
	}
}

func TestCoverageKnowsWhatItDidNotSee(t *testing.T) {
	partial := Coverage{Prefix: "10.0.0.0/24", AddrsTotal: 254, AddrsSwept: 100}
	if partial.Complete() {
		t.Error("a partial sweep must not report itself complete")
	}
	full := Coverage{Prefix: "10.0.0.0/24", AddrsTotal: 254, AddrsSwept: 254}
	if !full.Complete() {
		t.Error("a full sweep should report complete")
	}
	empty := Coverage{}
	if empty.Complete() {
		t.Error("a sweep that never ran is not complete")
	}
}

func TestConfidenceSerialisesAsWords(t *testing.T) {
	b, err := json.Marshal(struct{ C Confidence }{Moderate})
	if err != nil || !strings.Contains(string(b), `"inferred"`) {
		t.Fatalf("got %s, %v", b, err)
	}
	var back struct{ C Confidence }
	if err := json.Unmarshal([]byte(`{"C":"asserted"}`), &back); err != nil || back.C != Asserted {
		t.Fatalf("round trip failed: %v %v", back.C, err)
	}
}
