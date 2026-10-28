// Package core is CARTA's surface for gomobile: what an Android app calls to
// run a survey and compare two of them.
//
// Everything crosses the boundary as JSON strings. gomobile can only pass
// basic types, and the survey model is the same JSON the CLI writes to
// survey.json, so a snapshot taken on the phone opens in `carta ui` unchanged.
//
// The package decides nothing about the platform. The app works out where it
// is standing with the platform's own APIs and hands that in as a profile; the
// only Android-specific knowledge here is that such a vantage cannot read its
// ARP cache and must not fall back to a resolver it did not name.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
	"github.com/Tanmay-Anand/carta/internal/survey"
)

// Instrument is recorded on every survey this package takes, so drift can
// tell a phone's survey from a laptop's.
const Instrument = "carta-android"

// Progress receives stage changes and sweep progress. It is called on a Go
// thread, never the app's main thread.
type Progress interface {
	OnStage(stage string, done, total int)
}

// profileJSON is what the app reports about its own position.
type profileJSON struct {
	Host        string   `json:"host"`
	OS          string   `json:"os"`
	Interface   string   `json:"interface"`
	IP          string   `json:"ip"`
	PrefixLen   int      `json:"prefix_len"`
	Gateway     string   `json:"gateway"`
	Resolvers   []string `json:"resolvers"`
	Search      []string `json:"search"`
	DHCPServer  string   `json:"dhcp_server"`
	Wireless    string   `json:"wireless"`
	Confounders []string `json:"confounders"`
	Interfaces  []struct {
		Name  string   `json:"name"`
		Addrs []string `json:"addrs"`
		Up    bool     `json:"up"`
	} `json:"interfaces"`
}

type optionsJSON struct {
	DeadlineSeconds int  `json:"deadline_seconds"`
	TimeoutMS       int  `json:"timeout_ms"`
	Concurrency     int  `json:"concurrency"`
	Passive         bool `json:"passive"`
}

// Session is one survey. It runs once; a second survey is a second session.
// Cancel may be called from any thread, before or during Run.
type Session struct {
	prof     *orient.Profile
	opt      optionsJSON
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	started  bool
	deadline time.Duration
}

// NewSession validates the profile and options. It does not touch the network.
func NewSession(profile, options string) (*Session, error) {
	var p profileJSON
	if err := json.Unmarshal([]byte(profile), &p); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	prof, err := p.toProfile()
	if err != nil {
		return nil, err
	}
	var o optionsJSON
	if options != "" {
		if err := json.Unmarshal([]byte(options), &o); err != nil {
			return nil, fmt.Errorf("options: %w", err)
		}
	}
	deadline := 4 * time.Minute
	if o.DeadlineSeconds > 0 {
		deadline = time.Duration(o.DeadlineSeconds) * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{prof: prof, opt: o, ctx: ctx, cancel: cancel, deadline: deadline}, nil
}

func (p profileJSON) toProfile() (*orient.Profile, error) {
	ip := net.ParseIP(p.IP).To4()
	if ip == nil {
		return nil, fmt.Errorf("profile: %q is not an IPv4 address", p.IP)
	}
	if p.PrefixLen < 1 || p.PrefixLen > 32 {
		return nil, fmt.Errorf("profile: prefix length %d is out of range", p.PrefixLen)
	}
	prof := &orient.Profile{
		Host: p.Host, OS: p.OS, Interface: p.Interface,
		IP:      ip,
		Prefix:  &net.IPNet{IP: ip, Mask: net.CIDRMask(p.PrefixLen, 32)},
		Gateway: p.Gateway, Resolvers: p.Resolvers, Search: p.Search,
		DHCPServer: p.DHCPServer, Wireless: p.Wireless, Confounders: p.Confounders,
		Instrument: Instrument,
	}
	for _, ifc := range p.Interfaces {
		prof.Interfaces = append(prof.Interfaces, orient.InterfaceInfo{Name: ifc.Name, Addrs: ifc.Addrs, Up: ifc.Up})
	}
	return prof, nil
}

// Run performs the survey and returns its snapshot JSON.
//
// A survey cut short by Cancel or by the deadline returns what it found, with
// the coverage saying where it stopped — the same as the CLI on Ctrl-C. An
// error means the survey could not be taken at all.
func (s *Session) Run(p Progress) (string, error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return "", errors.New("a session runs once; start a new one")
	}
	s.started = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, s.deadline)
	defer cancel()

	opt := survey.Options{
		Profile:              s.prof,
		Passive:              s.opt.Passive,
		Concurrency:          s.opt.Concurrency,
		Timeout:              time.Duration(s.opt.TimeoutMS) * time.Millisecond,
		NoARP:                true,
		ExplicitResolverOnly: true,
	}
	if p != nil {
		opt.Progress = func(stage string, done, total int) { p.OnStage(stage, done, total) }
	}
	g, _, err := survey.Run(ctx, opt)
	if err != nil {
		return "", err
	}
	b, err := snapshot.Marshal(g)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Cancel stops the survey. Calling it more than once, or after Run returned,
// does nothing.
func (s *Session) Cancel() { s.cancel() }

// Diff compares two snapshots, older first, and returns the drift report as
// JSON with a one-line summary. Comparing surveys of two different networks,
// or from two different instruments, is not refused: the report's caveats say
// what is wrong with the comparison and "comparable" is false where it means
// nothing.
func Diff(before, after string) (string, error) {
	b, err := snapshot.Unmarshal([]byte(before))
	if err != nil {
		return "", fmt.Errorf("before: %w", err)
	}
	a, err := snapshot.Unmarshal([]byte(after))
	if err != nil {
		return "", fmt.Errorf("after: %w", err)
	}
	r := drift.Compare(b, a)
	out, err := json.Marshal(struct {
		drift.Report
		Summary string `json:"summary"`
	}{r, r.Summary()})
	return string(out), err
}

// Fingerprint summarises a stored snapshot for the list the operator picks a
// drift baseline from. It is everything the app shows to tell one network
// from another, and none of it is an identity: 192.168.1.1/24 is the
// fingerprint of half the home networks in the world.
func Fingerprint(snap string) (string, error) {
	g, err := snapshot.Unmarshal([]byte(snap))
	if err != nil {
		return "", err
	}
	live := 0
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v == "yes" {
			live++
		}
	}
	v := g.Vantage
	out, err := json.Marshal(struct {
		SurveyID   string    `json:"survey_id"`
		StartedAt  time.Time `json:"started_at"`
		Prefix     string    `json:"prefix"`
		Gateway    string    `json:"gateway"`
		DHCPServer string    `json:"dhcp_server"`
		Resolvers  []string  `json:"resolvers"`
		Search     []string  `json:"search"`
		Instrument string    `json:"instrument"`
		Devices    int       `json:"devices"`
		CutShort   string    `json:"cut_short,omitempty"`
	}{g.SurveyID, g.StartedAt, v.Prefix, v.Gateway, v.DHCPServer, v.Resolvers, v.SearchList,
		v.InstrumentName(), live, g.Meta["cut_short"]})
	return string(out), err
}
