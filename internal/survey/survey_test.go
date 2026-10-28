package survey

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/Tanmay-Anand/carta/internal/orient"
)

// A survey cancelled before it starts must emit nothing and must not claim
// coverage it did not get. The supplied profile is on TEST-NET-1, so nothing
// would answer even if a packet escaped.
func TestACancelledSurveyIsPartialAndSaysSo(t *testing.T) {
	ip, prefix, _ := net.ParseCIDR("192.0.2.5/29")
	prof := &orient.Profile{
		OS: "test", Instrument: "carta-test", Interface: "test0",
		IP: ip, Prefix: prefix, Gateway: "192.0.2.1",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g, _, err := Run(ctx, Options{Profile: prof, NoARP: true, ExplicitResolverOnly: true})
	if err != nil {
		t.Fatalf("a cancelled survey is a partial result, not an error: %v", err)
	}
	if len(g.Coverage) != 1 {
		t.Fatalf("coverage: %+v", g.Coverage)
	}
	cov := g.Coverage[0]
	if cov.AddrsSwept != 0 || cov.AddrsTotal != 6 {
		t.Errorf("swept %d of %d; a cancelled sweep touched nothing", cov.AddrsSwept, cov.AddrsTotal)
	}
	for _, p := range []string{"arp-cache", "rdns"} {
		if slices.Contains(cov.Protocols, p) {
			t.Errorf("%s listed as queried, but it never ran", p)
		}
	}
	notes := strings.Join(cov.Notes, "\n")
	for _, want := range []string{"layer-2 evidence unavailable", "survey cut short (cancelled) during listen"} {
		if !strings.Contains(notes, want) {
			t.Errorf("missing note %q in:\n%s", want, notes)
		}
	}
	if g.Vantage.Instrument != "carta-test" {
		t.Errorf("instrument %q did not reach the vantage", g.Vantage.Instrument)
	}
	var titles []string
	for _, f := range g.Findings {
		titles = append(titles, f.Title)
	}
	for _, want := range []string{"The survey was cut short", "Coverage is incomplete"} {
		if !slices.Contains(titles, want) {
			t.Errorf("missing finding %q in %v", want, titles)
		}
	}
}
