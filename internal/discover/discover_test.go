package discover

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// buildResponse assembles an mDNS response packet the way a device would.
func buildResponse(t *testing.T, extra ...dnsmessage.Resource) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	name := dnsmessage.MustNewName("printer.local.")
	if err := b.AResource(
		dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 120},
		dnsmessage.AResource{A: [4]byte{192, 168, 0, 33}},
	); err != nil {
		t.Fatal(err)
	}
	if err := b.PTRResource(
		dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("_ipp._tcp.local."), Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET, TTL: 120},
		dnsmessage.PTRResource{PTR: dnsmessage.MustNewName("Office Printer._ipp._tcp.local.")},
	); err != nil {
		t.Fatal(err)
	}
	for _, r := range extra {
		if err := b.AAAAResource(r.Header, *r.Body.(*dnsmessage.AAAAResource)); err != nil {
			t.Fatal(err)
		}
	}
	out, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAbsorbReadsNamesAndClasses(t *testing.T) {
	g := model.NewGraph("t")
	absorb(g, "192.168.0.33", buildResponse(t), map[string]bool{})

	e := g.Entity(model.IPIdentity("192.168.0.33"))
	if e == nil {
		t.Fatal("no entity was created from the response")
	}
	if got, _ := e.Best("hostname"); got != "printer" {
		t.Errorf("hostname from the A record: got %q, want printer", got)
	}
	if got, _ := e.Best("class"); got != "printer" {
		t.Errorf("class from the service type: got %q", got)
	}
	// The application's instance name must not become the device's hostname.
	if got, _ := e.Best("label"); got != "Office Printer" {
		t.Errorf("instance name should be filed as a label: got %q", got)
	}
}

// TestAbsorbTerminatesOnUnhandledRecordTypes is a regression test for a hang.
//
// The first version of the parser stepped the DNS message state machine by
// hand and called SkipAnswer for every record type it did not handle. In the
// additionals section that skip is the wrong one, so the parser stopped
// advancing while the loop kept running: the survey spun at 100% CPU forever
// against a real network, and no unit test noticed because the packets in the
// tests only held types the parser knew.
func TestAbsorbTerminatesOnUnhandledRecordTypes(t *testing.T) {
	aaaa := dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{
			Name: dnsmessage.MustNewName("printer.local."), Type: dnsmessage.TypeAAAA,
			Class: dnsmessage.ClassINET, TTL: 120,
		},
		Body: &dnsmessage.AAAAResource{AAAA: [16]byte{0x20, 0x01, 0xd, 0xb8}},
	}
	pkt := buildResponse(t, aaaa)

	done := make(chan struct{})
	go func() {
		absorb(model.NewGraph("t"), "192.168.0.33", pkt, map[string]bool{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("absorb did not terminate on a packet containing an unhandled record type")
	}
}

func TestAbsorbSurvivesGarbage(t *testing.T) {
	// Every byte here arrives from an unauthenticated device on a broadcast
	// network. Malformed input is the normal case, not the exception.
	for _, pkt := range [][]byte{nil, {}, {0x00}, []byte("not a dns packet at all"),
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}} {
		done := make(chan struct{})
		go func(p []byte) {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panicked on %q: %v", p, r)
				}
			}()
			absorb(model.NewGraph("t"), "10.0.0.1", p, map[string]bool{})
		}(pkt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("absorb hung on %q", pkt)
		}
	}
}

func TestHostAddressesExcludesNetworkAndBroadcast(t *testing.T) {
	_, n, _ := net.ParseCIDR("192.168.0.0/24")
	addrs := hostAddresses(n)
	if len(addrs) != 254 {
		t.Fatalf("a /24 holds 254 usable addresses, got %d", len(addrs))
	}
	if addrs[0].String() != "192.168.0.1" || addrs[253].String() != "192.168.0.254" {
		t.Errorf("range runs %s..%s", addrs[0], addrs[253])
	}

	_, small, _ := net.ParseCIDR("10.1.2.0/30")
	if got := len(hostAddresses(small)); got != 2 {
		t.Errorf("a /30 holds 2 usable addresses, got %d", got)
	}
}

func TestIsNonDeviceFiltersProtocolFurniture(t *testing.T) {
	cases := []struct {
		ip, mac string
		want    bool
	}{
		{"224.0.0.251", "01:00:5e:00:00:fb", true}, // mDNS multicast group
		{"239.255.255.250", "01:00:5e:7f:ff:fa", true},
		{"192.168.0.255", "ff:ff:ff:ff:ff:ff", true}, // broadcast
		{"192.168.0.7", "dc:56:7b:64:5b:bd", false},  // a real device
	}
	for _, c := range cases {
		if got := isNonDevice(c.ip, c.mac); got != c.want {
			t.Errorf("isNonDevice(%s, %s) = %v, want %v", c.ip, c.mac, got, c.want)
		}
	}
}

func TestRandomisedMACsAreReportedRatherThanGuessedAt(t *testing.T) {
	g := model.NewGraph("t")
	g.Observe([]model.Identity{model.IPIdentity("192.168.0.4"), model.MACIdentity("7e:5d:c3:c7:a9:be")})
	g.Observe([]model.Identity{model.IPIdentity("192.168.0.1"), model.MACIdentity("b8:fb:b3:95:58:0d")})
	Vendors(g, map[string]string{"B8FBB3": "TP-Link", "7E5DC3": "Should Never Be Used"})

	randomised := g.Entity(model.IPIdentity("192.168.0.4"))
	if v, _ := randomised.Best("vendor"); v != "" {
		t.Errorf("a randomised address must not yield a vendor, got %q", v)
	}
	if notes := randomised.All("note"); len(notes) == 0 || !strings.Contains(notes[0].Value, "randomised") {
		t.Error("the randomisation itself should be reported")
	}

	burned := g.Entity(model.IPIdentity("192.168.0.1"))
	v, conf := burned.Best("vendor")
	if v != "TP-Link" {
		t.Errorf("burned-in OUI should resolve, got %q", v)
	}
	if conf != model.Moderate {
		t.Errorf("an OUI is an inference and no better: got %v", conf)
	}
}

func TestLoadOUIReadsIEEEFormatAndKeepsBuiltins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oui.csv")
	body := "Registry,Assignment,Organization Name,Organization Address\n" +
		"MA-L,AABBCC,\"Example Networks, Inc\",Somewhere\n" +
		"MA-L,DDEEFF,Another Vendor,Elsewhere\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := LoadOUI(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := table["AABBCC"]; got != "Example Networks, Inc" {
		t.Errorf("quoted vendor name: got %q", got)
	}
	if table["B8FBB3"] == "" {
		t.Error("a supplied pack must extend the built-in table, not replace it")
	}

	// A missing pack degrades to the built-in table rather than failing a survey.
	if table, err := LoadOUI(filepath.Join(dir, "absent.csv")); err == nil || len(table) == 0 {
		t.Error("a missing pack should report the error and still return the built-in table")
	}
}

func TestSanitiseStripsEverythingThatIsNotPrintableASCII(t *testing.T) {
	// A real SSH banner: a version string followed by binary key exchange.
	raw := "SSH-2.0-dropbear_2020.80\r\n\x00\x00\x01\x14\x05\xd4\x9e\x8f\xff"
	got := sanitise(raw)
	if got != "SSH-2.0-dropbear_2020.80" {
		t.Errorf("got %q", got)
	}
	// Pipes would otherwise break out of a Markdown table cell.
	if got := sanitise("a|b"); !strings.Contains(got, `\|`) {
		t.Errorf("pipe must be escaped for Markdown: %q", got)
	}
}

func TestPortHintsAreOnlyEverWeakEvidence(t *testing.T) {
	// Guards the design rule: an open port suggests, it never establishes.
	// If someone later raises these to a higher confidence, this fails loudly.
	g := model.NewGraph("t")
	ids := []model.Identity{model.IPIdentity("10.0.0.5")}
	hint := portHints[9100]
	if hint.class != "printer" {
		t.Fatalf("9100 should hint printer, got %q", hint.class)
	}
	g.Observe(ids, model.Claim{Key: "class", Value: hint.class, Confidence: model.Low,
		Source: model.Source{Method: "tcp-connect", At: time.Now()}})
	g.Observe(ids, model.Claim{Key: "class", Value: "camera", Confidence: model.Asserted,
		Source: model.Source{Method: "mdns", At: time.Now()}})

	if v, _ := g.Entity(ids[0]).Best("class"); v != "camera" {
		t.Errorf("a device's own announcement must override a port guess, got %q", v)
	}
}
