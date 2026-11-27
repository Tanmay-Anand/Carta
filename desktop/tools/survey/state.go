package survey

import (
	"encoding/binary"
	"net"

	"github.com/Tanmay-Anand/carta/desktop/kit"
	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
)

// Tabs of the right-hand panel, in order.
const (
	tabDevice = iota
	tabFindings
	tabDevices
	tabDrift
)

// state is everything the survey view shows. Widgets are rebuilt from it, so
// a light/dark switch or a new survey is one rebuild, not a set of updates
// that can disagree with each other.
type state struct {
	g       *model.Graph  // the survey on screen
	earlier *model.Graph  // the survey it is compared with, if any
	drift   *drift.Report // g against earlier

	selected *model.Entity
	// selectNote explains the selection after a new survey: kept by a weak
	// match, or cleared because the device was not seen this time.
	selectNote string

	tab    int
	filter string

	run runState
}

// runState is the survey in progress, or how the last one ended.
type runState struct {
	running, stopping bool
	quiet             bool
	stage             string
	done, total       int
	// The sweep's own count, kept after the sweep ends so the matrix stays
	// filled in while the later stages run.
	sweepDone, sweepTotal int
	stoppedIn         string // the stage a cut-short survey stopped in
	savedTo           string // the folder the last survey was saved in
	err               string
}

// alive lists the devices that answered, in the survey's order.
func alive(g *model.Graph) []*model.Entity {
	var out []*model.Entity
	if g == nil {
		return nil
	}
	for _, e := range g.Entities {
		if isAlive(e) {
			out = append(out, e)
		}
	}
	return out
}

// coverage is the dot matrix and its counts for a finished survey: one dot
// per host address in the prefix, in address order.
func coverage(g *model.Graph) (dots []kit.Dot, answered, silent, unreached int) {
	if g == nil || len(g.Coverage) == 0 {
		return nil, 0, 0, 0
	}
	c := g.Coverage[0]
	_, prefix, err := net.ParseCIDR(c.Prefix)
	if err != nil || prefix.IP.To4() == nil {
		return nil, 0, 0, 0
	}
	heard := map[string]bool{}
	for _, e := range alive(g) {
		for _, a := range e.Addresses() {
			heard[a] = true
		}
	}
	base := binary.BigEndian.Uint32(prefix.IP.To4())
	dots = make([]kit.Dot, c.AddrsTotal)
	for i := range dots {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, base+uint32(i)+1) // skip the network address
		switch {
		case heard[ip.String()]:
			// Heard is heard, swept or not: a quiet survey hears devices from
			// this machine's caches without asking anything.
			dots[i] = kit.Answered
			answered++
		case i < c.AddrsSwept:
			dots[i] = kit.Silent
			silent++
		default:
			dots[i] = kit.Unreached
			unreached++
		}
	}
	return dots, answered, silent, unreached
}

// progressDots is the matrix while a sweep runs: addresses asked so far are
// silent until the survey ends and says which of them answered.
func progressDots(total, done int) []kit.Dot {
	dots := make([]kit.Dot, total)
	for i := 0; i < done && i < total; i++ {
		dots[i] = kit.Silent
	}
	return dots
}
