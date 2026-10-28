package drift

import (
	"strings"
	"testing"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// announced records a device the way mDNS and reverse DNS do: the name is an
// identity, not only a claim, which is what lets drift match on it.
func announced(g *model.Graph, ip, host string) {
	at := time.Unix(1700000000, 0)
	g.Observe([]model.Identity{model.IPIdentity(ip), model.HostIdentity(host)},
		model.Claim{Key: "alive", Value: "yes", Confidence: model.High,
			Source: model.Source{Method: "tcp-connect", At: at}},
		model.Claim{Key: "hostname", Value: host, Confidence: model.Asserted,
			Source: model.Source{Method: "mdns", At: at}})
}

// A phone cannot see MAC addresses, so its surveys match on hostname and then
// on address. A hostname match is an inference and says so.
func TestWithoutMACsAHostnameStillMatchesAMovedDevice(t *testing.T) {
	before := graph("192.168.0.0/24")
	announced(before, "192.168.0.17", "nas")
	after := graph("192.168.0.0/24")
	announced(after, "192.168.0.43", "nas")

	k := kinds(Compare(before, after))
	if len(k["moved"]) != 1 || k["appeared"] != nil || k["vanished"] != nil {
		t.Fatalf("a new lease with a stable hostname is a move: %+v", k)
	}
	if m := k["moved"][0]; m.MatchedBy != "host" || m.Match != model.Moderate {
		t.Errorf("matched by %s at %v, want host at inferred", m.MatchedBy, m.Match)
	}
}

// Without MACs or hostnames only the address is left, and on a DHCP network a
// rotated lease pairs two different devices that swapped addresses. That
// cannot be detected from here; it must be reported as weak rather than as
// fact.
func TestAddressOnlyMatchesAreWeakBecauseLeasesRotate(t *testing.T) {
	at := time.Unix(1700000000, 0)
	ssh := model.Claim{Key: "service", Value: "ssh (22/tcp)", Confidence: model.High,
		Source: model.Source{Method: "tcp-connect", At: at}}
	ipp := model.Claim{Key: "service", Value: "ipp (631/tcp)", Confidence: model.High,
		Source: model.Source{Method: "tcp-connect", At: at}}

	before := graph("192.168.0.0/24")
	device(before, "192.168.0.20", "", "", ssh) // a server
	device(before, "192.168.0.21", "", "", ipp) // a printer
	after := graph("192.168.0.0/24")
	device(after, "192.168.0.20", "", "", ipp) // the printer got .20
	device(after, "192.168.0.21", "", "", ssh) // the server got .21

	r := Compare(before, after)
	k := kinds(r)
	if len(k["mutated"]) != 2 {
		t.Fatalf("swapped leases read as two mutations, got %+v", r.Changes)
	}
	for _, c := range k["mutated"] {
		if c.MatchedBy != "ip" || c.Match != model.Low {
			t.Errorf("%s matched by %s at %v, want ip at weak", c.Entity, c.MatchedBy, c.Match)
		}
	}
	if !strings.Contains(r.Summary(), "2 matched by address only") {
		t.Errorf("the summary must say the matches are weak: %q", r.Summary())
	}
}

// 192.168.1.0/24 is half the home networks in the world. Two surveys on the
// same prefix behind different gateways are two networks, not one that
// changed.
func TestSamePrefixDifferentGatewayIsNotComparable(t *testing.T) {
	before := graph("192.168.1.0/24")
	before.Vantage.Gateway = "192.168.1.1"
	after := graph("192.168.1.0/24")
	after.Vantage.Gateway = "192.168.1.254"

	r := Compare(before, after)
	if r.Comparable {
		t.Error("different gateways on one prefix must not be comparable")
	}
	if !strings.Contains(strings.Join(r.Caveats, " "), "gateway differs") {
		t.Errorf("caveats: %v", r.Caveats)
	}
}

func TestFingerprintAndInstrumentDifferencesAreStated(t *testing.T) {
	before := graph("192.168.1.0/24")
	before.Vantage.Gateway, before.Vantage.DHCPServer = "192.168.1.1", "192.168.1.1"
	before.Vantage.Resolvers = []string{"192.168.1.1"}
	after := graph("192.168.1.0/24")
	after.Vantage.Gateway, after.Vantage.DHCPServer = "192.168.1.1", "192.168.1.2"
	after.Vantage.Resolvers = []string{"192.168.1.1", "1.1.1.1"}
	after.Vantage.Instrument = "carta-android"

	r := Compare(before, after)
	if !r.Comparable {
		t.Error("same prefix and gateway stays comparable; the rest are caveats")
	}
	all := strings.Join(r.Caveats, " ")
	for _, want := range []string{"DHCP server differs", "DNS servers differ", "carta-cli, then carta-android"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing caveat %q in %v", want, r.Caveats)
		}
	}
}
