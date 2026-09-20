// Package drift compares two surveys.
//
// This is the part that turns a scan into documentation that stays true. A map
// made once is wrong within a month; a map that reports its own divergence
// keeps being useful, and "what changed since March" is the question an
// engineer actually asks.
//
// The hard problem is identity across time, not comparison. A device whose DHCP
// lease moved it from .17 to .43 has not appeared and vanished, it has MOVED,
// and a report that says otherwise trains people to ignore it. So matching runs
// on the most durable identity available — MAC first, then a stable hostname,
// then the address — and each match records which one it used.
package drift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Change classifies one difference between two surveys.
type Change struct {
	Kind      string   // "appeared", "vanished", "moved", "mutated"
	Entity    string   // label in the newer survey where one exists
	Was       string   // previous state, for moved and mutated
	Now       string   // current state
	MatchedBy string   // which identity kind held across the two surveys
	Details   []string // per-attribute changes, for mutated
}

// Report is the full comparison.
type Report struct {
	BeforeID string
	AfterID  string
	Vantage  string
	Changes  []Change

	// Comparable records whether the two surveys were taken from a position
	// close enough for a comparison to mean anything. Two surveys from
	// different subnets do not produce drift, they produce noise, and saying so
	// is more useful than printing 200 spurious changes.
	Comparable bool
	Caveats    []string
}

// Compare diffs two surveys, oldest first.
func Compare(before, after *model.Graph) Report {
	r := Report{
		BeforeID: before.SurveyID,
		AfterID:  after.SurveyID,
		Vantage:  after.Vantage.Prefix,
	}

	r.Comparable = before.Vantage.Prefix == after.Vantage.Prefix
	if !r.Comparable {
		r.Caveats = append(r.Caveats, fmt.Sprintf(
			"the surveys were taken on different prefixes (%s, then %s) — everything below is a difference between two networks, not drift within one",
			before.Vantage.Prefix, after.Vantage.Prefix))
	}
	if before.Vantage.Interface != after.Vantage.Interface {
		r.Caveats = append(r.Caveats, fmt.Sprintf(
			"the vantage interface changed (%s, then %s) — what is visible depends on where the instrument stands",
			before.Vantage.Interface, after.Vantage.Interface))
	}
	if !coverageComparable(before, after) {
		r.Caveats = append(r.Caveats, "coverage differed between the two surveys — a device reported as vanished may simply not have been swept")
	}

	matchedAfter := map[string]bool{}
	for _, b := range before.Entities {
		a, by := match(b, after.Entities, matchedAfter)
		if a == nil {
			if alive(b) {
				r.Changes = append(r.Changes, Change{
					Kind: "vanished", Entity: b.Label(),
					Was: describe(b), MatchedBy: "",
				})
			}
			continue
		}
		matchedAfter[a.ID] = true

		if moved, was, now := movedAddress(b, a); moved {
			r.Changes = append(r.Changes, Change{
				Kind: "moved", Entity: a.Label(), Was: was, Now: now, MatchedBy: by,
			})
		}
		if details := mutations(b, a); len(details) > 0 {
			r.Changes = append(r.Changes, Change{
				Kind: "mutated", Entity: a.Label(), MatchedBy: by, Details: details,
			})
		}
	}
	for _, a := range after.Entities {
		if !matchedAfter[a.ID] && alive(a) {
			r.Changes = append(r.Changes, Change{Kind: "appeared", Entity: a.Label(), Now: describe(a)})
		}
	}

	sort.SliceStable(r.Changes, func(i, j int) bool {
		return kindRank(r.Changes[i].Kind) < kindRank(r.Changes[j].Kind)
	})
	return r
}

// match finds an entity in the newer survey that is the same device.
//
// The order of the attempts IS the identity model: a burned-in MAC survives a
// reboot, a rename and a new lease; a hostname survives a new lease but not a
// rename; an address survives nothing but is all there is for a device that
// only ever answered a ping.
func match(b *model.Entity, candidates []*model.Entity, taken map[string]bool) (*model.Entity, string) {
	for _, kind := range []string{"mac", "host", "ip"} {
		for _, want := range identityValues(b, kind) {
			// A randomised MAC is not an identity across visits, and matching on
			// one produces a phantom device every time the phone reconnects.
			if kind == "mac" && randomised(want) {
				continue
			}
			for _, c := range candidates {
				if taken[c.ID] {
					continue
				}
				for _, got := range identityValues(c, kind) {
					if strings.EqualFold(want, got) {
						return c, kind
					}
				}
			}
		}
	}
	return nil, ""
}

func identityValues(e *model.Entity, kind string) []string {
	switch kind {
	case "mac":
		return e.MACs()
	case "host":
		return e.Hostnames()
	default:
		return e.Addresses()
	}
}

// randomised reports a locally-administered MAC, which a phone regenerates per
// network and sometimes per day.
func randomised(mac string) bool {
	if len(mac) < 2 {
		return false
	}
	var b byte
	_, err := fmt.Sscanf(mac[:2], "%02x", &b)
	return err == nil && b&0x02 != 0
}

func movedAddress(b, a *model.Entity) (bool, string, string) {
	was, now := strings.Join(b.Addresses(), ", "), strings.Join(a.Addresses(), ", ")
	if was == now || was == "" || now == "" {
		return false, "", ""
	}
	return true, was, now
}

// mutations lists the attribute changes worth reporting.
//
// Confidence changes alone are not drift — the same device identified by a
// better method this time has not changed — so only values are compared, and
// only for the keys that describe what a device IS.
func mutations(b, a *model.Entity) []string {
	var out []string
	for _, key := range []string{"hostname", "vendor", "class", "os", "model"} {
		bv, _ := b.Best(key)
		av, _ := a.Best(key)
		if bv != av && bv != "" && av != "" {
			out = append(out, fmt.Sprintf("%s: %q → %q", key, bv, av))
		}
		if bv == "" && av != "" {
			out = append(out, fmt.Sprintf("%s: now known to be %q", key, av))
		}
	}
	bs, as := serviceSet(b), serviceSet(a)
	for s := range as {
		if !bs[s] {
			out = append(out, "service appeared: "+s)
		}
	}
	for s := range bs {
		if !as[s] {
			out = append(out, "service gone: "+s)
		}
	}
	sort.Strings(out)
	return out
}

func serviceSet(e *model.Entity) map[string]bool {
	out := map[string]bool{}
	for _, c := range e.All("service") {
		out[c.Value] = true
	}
	return out
}

func describe(e *model.Entity) string {
	var parts []string
	if addrs := e.Addresses(); len(addrs) > 0 {
		parts = append(parts, strings.Join(addrs, ", "))
	}
	if v, _ := e.Best("vendor"); v != "" {
		parts = append(parts, v)
	}
	if c, _ := e.Best("class"); c != "" {
		parts = append(parts, c)
	}
	return strings.Join(parts, " · ")
}

func alive(e *model.Entity) bool {
	v, _ := e.Best("alive")
	return v == "yes"
}

func coverageComparable(b, a *model.Graph) bool {
	if len(b.Coverage) == 0 || len(a.Coverage) == 0 {
		return false
	}
	return b.Coverage[0].AddrsSwept == a.Coverage[0].AddrsSwept
}

func kindRank(k string) int {
	switch k {
	case "appeared":
		return 0
	case "vanished":
		return 1
	case "moved":
		return 2
	default:
		return 3
	}
}

// Summary is the one line printed at the end of a survey.
func (r Report) Summary() string {
	if len(r.Changes) == 0 {
		return "No drift since the last survey."
	}
	counts := map[string]int{}
	for _, c := range r.Changes {
		counts[c.Kind]++
	}
	var parts []string
	for _, k := range []string{"appeared", "vanished", "moved", "mutated"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	return "Drift since the last survey: " + strings.Join(parts, ", ") + "."
}
