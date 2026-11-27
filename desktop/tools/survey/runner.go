package survey

import (
	"context"
	"net"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/survey"
)

// Instrument is recorded on every survey the desktop app takes, so drift can
// tell its surveys from the CLI's and the phone's: two instruments see
// different things, and that difference is not a change in the network.
const Instrument = "carta-desktop"

// Progress reports a stage boundary (total 0) or progress within a stage, by
// the core's stage ids: orient, listen, sweep, probe, nbns, rdns, resolve.
type Progress func(stage string, done, total int)

// Runner takes a survey. The view does not know whether it is real: a test,
// or the UI mock, hands it a Runner that never touches a network.
type Runner interface {
	// Run surveys the network this machine is on. A cancelled context is not
	// an error: what was found is returned, marked as cut short. The profile
	// is where the survey stood, which the document pack needs.
	Run(ctx context.Context, quiet bool, progress Progress) (*model.Graph, *orient.Profile, error)
}

// realRunner is the survey itself, the same engine and options as
// `carta survey`, with quiet as `--passive`.
type realRunner struct{}

func (realRunner) Run(ctx context.Context, quiet bool, progress Progress) (*model.Graph, *orient.Profile, error) {
	progress("orient", 0, 0)
	prof, err := orient.Run(ctx)
	if err != nil {
		return nil, nil, err
	}
	prof.Instrument = Instrument
	return survey.Run(ctx, survey.Options{Profile: prof, Passive: quiet, Progress: progress})
}

// stageNames are what the status line says for each stage id.
var stageNames = map[string]string{
	"orient": "Orienting", "listen": "Listening", "sweep": "Sweeping",
	"probe": "Probing services", "nbns": "Asking NetBIOS", "rdns": "Reverse DNS",
	"resolve": "Resolving",
}

func stageName(id string) string {
	if n, ok := stageNames[id]; ok {
		return n
	}
	return id
}

// fakeRunner walks the real stages at roughly a real survey's pace and returns
// the fictional network. It sends nothing; it is the mock's runner and the
// tests'.
type fakeRunner struct {
	// step is the pause per stage and per sweep step; tests shorten it.
	step time.Duration
}

// quietlyHeard is what a quiet survey of the fictional network knows: the
// devices in this machine's ARP cache, the ones it recently talked to.
var quietlyHeard = map[string]bool{
	"192.168.10.1": true, "192.168.10.5": true, "192.168.10.12": true, "192.168.10.20": true,
}

func (f fakeRunner) Run(ctx context.Context, quiet bool, progress Progress) (*model.Graph, *orient.Profile, error) {
	wait := func(n int) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Duration(n) * f.step):
			return true
		}
	}
	stage, swept, began := "orient", 0, time.Now()
	finish := func() (*model.Graph, *orient.Profile, error) {
		g := Synthetic()
		g.SurveyID = began.UTC().Format("20060102T150405Z")
		g.StartedAt = began
		g.Coverage[0].StartedAt, g.Coverage[0].EndedAt = began, time.Now()
		cut := ctx.Err() != nil
		keep := func(e *model.Entity) bool {
			for _, a := range e.Addresses() {
				if quiet {
					return quietlyHeard[a]
				}
				if cut {
					ip := net.ParseIP(a).To4()
					return ip != nil && int(ip[3]) <= swept
				}
			}
			return !quiet && !cut
		}
		var ents []*model.Entity
		for _, e := range g.Entities {
			if keep(e) {
				ents = append(ents, e)
			}
		}
		g.Entities = ents
		g.Coverage[0].AddrsSwept = swept
		g.Coverage[0].Responded = len(ents)
		if quiet || cut {
			// The fictional findings are about devices a partial survey may
			// not have seen; a real partial survey computes its own.
			g.Findings = nil
		}
		if quiet {
			g.Coverage[0].Notes = append(g.Coverage[0].Notes,
				"quiet survey: nothing was sent; every device here comes from this host's own ARP cache")
		}
		if cut {
			g.Meta["cut_short"] = stage
		}
		g.Vantage.Instrument = Instrument
		g.Rebuild()
		return g, &orient.Profile{Instrument: Instrument, Interface: "Wi-Fi"}, nil
	}

	progress("orient", 0, 0)
	if !wait(6) {
		return finish()
	}
	if quiet {
		// Nothing is asked: the caches are read and the survey ends.
		return finish()
	}
	stage = "listen"
	progress(stage, 0, 0)
	if !wait(12) {
		return finish()
	}
	stage = "sweep"
	for swept = 0; swept < 254; {
		swept = min(254, swept+6)
		progress(stage, swept, 254)
		if !wait(1) {
			return finish()
		}
	}
	for _, s := range []string{"probe", "nbns", "rdns"} {
		stage = s
		progress(s, 0, 0)
		if !wait(6) {
			return finish()
		}
	}
	return finish()
}
