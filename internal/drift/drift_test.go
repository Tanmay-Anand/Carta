package drift

import (
	"strings"
	"testing"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Burned-in OUIs are used throughout: b8:27:eb is Raspberry Pi's, and its
// first octet has the locally-administered bit clear, which is what makes it
// usable as an identity across visits. Addresses like aa:bb:cc do NOT — the
// 0x02 bit is set — and the matcher is required to ignore those.
func graph(prefix string) *model.Graph {
	g := model.NewGraph("s")
	g.StartedAt = time.Unix(1700000000, 0)
	g.Vantage = model.Vantage{Prefix: prefix, Interface: "wlan0"}
	g.Coverage = []model.Coverage{{Prefix: prefix, AddrsSwept: 254, AddrsTotal: 254}}
	return g
}

func device(g *model.Graph, ip, mac, host string, claims ...model.Claim) {
	ids := []model.Identity{model.IPIdentity(ip)}
	if mac != "" {
		ids = append(ids, model.MACIdentity(mac))
	}
	all := []model.Claim{{Key: "alive", Value: "yes", Confidence: model.High,
		Source: model.Source{Method: "tcp-connect", At: time.Unix(1700000000, 0)}}}
	if host != "" {
		all = append(all, model.Claim{Key: "hostname", Value: host, Confidence: model.Asserted,
			Source: model.Source{Method: "mdns", At: time.Unix(1700000000, 0)}})
	}
	g.Observe(ids, append(all, claims...)...)
}

func kinds(r Report) map[string][]Change {
	out := map[string][]Change{}
	for _, c := range r.Changes {
		out[c.Kind] = append(out[c.Kind], c)
	}
	return out
}

func TestAppearedAndVanished(t *testing.T) {
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.10", "b8:27:eb:00:00:01", "nas")
	device(before, "192.168.0.11", "b8:27:eb:00:00:02", "printer")

	after := graph("192.168.0.0/24")
	device(after, "192.168.0.10", "b8:27:eb:00:00:01", "nas")
	device(after, "192.168.0.12", "b8:27:eb:00:00:03", "new-laptop")

	k := kinds(Compare(before, after))
	if len(k["appeared"]) != 1 || k["appeared"][0].Entity != "new-laptop" {
		t.Errorf("appeared: %v", k["appeared"])
	}
	if len(k["vanished"]) != 1 || k["vanished"][0].Entity != "printer" {
		t.Errorf("vanished: %v", k["vanished"])
	}
	if len(k["moved"]) != 0 {
		t.Errorf("nothing moved: %v", k["moved"])
	}
}

func TestANewLeaseIsAMoveNotABirthAndADeath(t *testing.T) {
	// The distinction that makes drift reports readable on a DHCP network.
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.17", "b8:27:eb:00:00:01", "nas")

	after := graph("192.168.0.0/24")
	device(after, "192.168.0.43", "b8:27:eb:00:00:01", "nas")

	k := kinds(Compare(before, after))
	if len(k["moved"]) != 1 {
		t.Fatalf("want one move, got %+v", k)
	}
	m := k["moved"][0]
	if m.Was != "192.168.0.17" || m.Now != "192.168.0.43" || m.MatchedBy != "mac" {
		t.Errorf("got %+v", m)
	}
	if len(k["appeared"])+len(k["vanished"]) != 0 {
		t.Error("a moved device must not also be reported as appeared or vanished")
	}
}

func TestRandomisedMACsAreNotUsedToMatchAcrossVisits(t *testing.T) {
	// 7e:.. is locally administered. Two different devices that happen to share
	// one must not be matched, or every phone becomes the same phone.
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.20", "7e:5d:c3:c7:a9:be", "")

	after := graph("192.168.0.0/24")
	device(after, "192.168.0.21", "7e:5d:c3:c7:a9:be", "")

	k := kinds(Compare(before, after))
	if len(k["moved"]) != 0 {
		t.Errorf("a randomised MAC is not an identity across visits: %v", k["moved"])
	}
	if len(k["appeared"]) != 1 || len(k["vanished"]) != 1 {
		t.Errorf("expected one appeared and one vanished, got %+v", k)
	}
}

func TestMutationsReportServiceAndIdentityChanges(t *testing.T) {
	at := time.Unix(1700000000, 0)
	svc := func(v string) model.Claim {
		return model.Claim{Key: "service", Value: v, Confidence: model.Moderate,
			Source: model.Source{Method: "tcp-connect", At: at}}
	}
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.10", "b8:27:eb:00:00:01", "nas", svc("ssh (22/tcp)"))

	after := graph("192.168.0.0/24")
	device(after, "192.168.0.10", "b8:27:eb:00:00:01", "nas-01", svc("ssh (22/tcp)"), svc("telnet (23/tcp)"))

	k := kinds(Compare(before, after))
	if len(k["mutated"]) != 1 {
		t.Fatalf("want one mutation, got %+v", k)
	}
	joined := strings.Join(k["mutated"][0].Details, " | ")
	if !strings.Contains(joined, "telnet") {
		t.Errorf("a new service must be reported: %s", joined)
	}
	if !strings.Contains(joined, "hostname") {
		t.Errorf("a rename must be reported: %s", joined)
	}
}

func TestComparingTwoDifferentNetworksSaysSoInsteadOfPrintingNoise(t *testing.T) {
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.10", "b8:27:eb:00:00:01", "nas")
	after := graph("10.0.0.0/24")
	device(after, "10.0.0.10", "b8:27:eb:00:00:99", "something-else")

	r := Compare(before, after)
	if r.Comparable {
		t.Error("surveys of different prefixes are not comparable")
	}
	if len(r.Caveats) == 0 || !strings.Contains(strings.Join(r.Caveats, " "), "different prefixes") {
		t.Errorf("the caveat must be stated: %v", r.Caveats)
	}
}

func TestSummaryOfAnUnchangedNetwork(t *testing.T) {
	before := graph("192.168.0.0/24")
	device(before, "192.168.0.10", "b8:27:eb:00:00:01", "nas")
	after := graph("192.168.0.0/24")
	device(after, "192.168.0.10", "b8:27:eb:00:00:01", "nas")

	r := Compare(before, after)
	if len(r.Changes) != 0 {
		t.Fatalf("an unchanged network must produce no changes, got %+v", r.Changes)
	}
	if got := r.Summary(); !strings.Contains(got, "No drift") {
		t.Errorf("got %q", got)
	}
}
