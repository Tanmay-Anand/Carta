package survey

import (
	"context"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Tanmay-Anand/carta/internal/model"
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

// A quiet survey sends nothing at all. mDNS and SSDP discovery both start by
// asking the segment, so they must not run; the coverage note must say what
// the survey actually did rather than claim listening it never did.
func TestAQuietSurveySendsNothing(t *testing.T) {
	asked := false
	defer func(orig func(context.Context, *model.Graph, *sync.WaitGroup, *sync.Mutex, *[]string)) { listen = orig }(listen)
	listen = func(context.Context, *model.Graph, *sync.WaitGroup, *sync.Mutex, *[]string) { asked = true }

	ip, prefix, _ := net.ParseCIDR("192.0.2.5/29")
	prof := &orient.Profile{OS: "test", Interface: "test0", IP: ip, Prefix: prefix, Gateway: "192.0.2.1"}
	var stages []string
	g, _, err := Run(context.Background(), Options{
		Profile: prof, Passive: true, NoARP: true, ExplicitResolverOnly: true,
		Progress: func(stage string, done, total int) {
			if total == 0 {
				stages = append(stages, stage)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Error("a quiet survey sent mDNS and SSDP queries")
	}
	for _, s := range []string{"listen", "probe", "nbns", "rdns"} {
		if slices.Contains(stages, s) {
			t.Errorf("a quiet survey announced the %q stage: %v", s, stages)
		}
	}
	cov := g.Coverage[0]
	if cov.AddrsSwept != 0 {
		t.Errorf("a quiet survey swept %d addresses", cov.AddrsSwept)
	}
	if len(cov.Protocols) != 0 {
		t.Errorf("a quiet survey with no ARP cache lists protocols it used: %v", cov.Protocols)
	}
	if notes := strings.Join(cov.Notes, "\n"); !strings.Contains(notes, "nothing was sent") {
		t.Errorf("the coverage does not say the survey was quiet:\n%s", notes)
	}
}
