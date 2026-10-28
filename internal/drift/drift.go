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
	Kind      string   `json:"kind"`                 // "appeared", "vanished", "moved", "mutated"
	Entity    string   `json:"entity"`               // label in the newer survey where one exists
	Was       string   `json:"was,omitempty"`        // previous state, for moved and mutated
	Now       string   `json:"now,omitempty"`        // current state
	MatchedBy string   `json:"matched_by,omitempty"` // which identity kind held across the two surveys
	Details   []string `json:"details,omitempty"`    // per-attribute changes, for mutated

	// Match is how strongly the two sides are believed to be the same device,
	// from the identity that matched them: a burned-in MAC is an observation,
	// a hostname is an inference, and an address alone is weak — on a DHCP
	// network a rotated lease pairs two different devices that happen to have
	// swapped addresses. Unknown for appeared and vanished, which matched
	// nothing.
	Match model.Confidence `json:"match"`
}

// Report is the full comparison.
type Report struct {
	BeforeID string   `json:"before_id"`
	AfterID  string   `json:"after_id"`
	Vantage  string   `json:"vantage"`
	Changes  []Change `json:"changes"`

	// Comparable records whether the two surveys were taken from a position
	// close enough for a comparison to mean anything. Two surveys from
	// different subnets do not produce drift, they produce noise, and saying so
	// is more useful than printing 200 spurious changes.
	Comparable bool     `json:"comparable"`
	Caveats    []string `json:"caveats"`
}

// Compare diffs two surveys, oldest first.
func Compare(before, after *model.Graph) Report {
	r := Report{
		BeforeID: before.SurveyID,
		AfterID:  after.SurveyID,
		Vantage:  after.Vantage.Prefix,
	}

	r.Caveats = append(r.Caveats, networkCaveats(&r, before.Vantage, after.Vantage)...)
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
				Kind: "moved", Entity: a.Label(), Was: was, Now: now, MatchedBy: by, Match: matchConfidence(by),
			})
		}
		if details := mutations(b, a); len(details) > 0 {
			r.Changes = append(r.Changes, Change{
				Kind: "mutated", Entity: a.Label(), MatchedBy: by, Details: details, Match: matchConfidence(by),
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

// matchConfidence is how much a match on each identity kind is worth.
func matchConfidence(by string) model.Confidence {
	switch by {
	case "mac":
		return model.High
	case "host":
		return model.Moderate
	case "ip":
		return model.Low
	}
	return model.Unknown
}

// networkCaveats decides whether the two surveys describe the same network.
//
// There is no reliable network identity to go on. An SSID is not read, a
// BSSID changes from one access point to the next, and a phone cannot see the
// gateway's MAC. What is left is a fingerprint of configuration — prefix,
// gateway, DHCP server, resolvers, search domain — and 192.168.1.1/24 is the
// fingerprint of half the home networks in the world. So a matching
// fingerprint is necessary, not sufficient: a mismatch in prefix or gateway
// makes the surveys incomparable, a mismatch anywhere else is printed, and a
// match proves nothing, which is why the operator picks the baseline rather
// than the tool guessing it.
func networkCaveats(r *Report, b, a model.Vantage) []string {
	var out []string
	r.Comparable = b.Prefix == a.Prefix && sameOrUnknown(b.Gateway, a.Gateway)
	if b.Prefix != a.Prefix {
		out = append(out, fmt.Sprintf(
			"the surveys were taken on different prefixes (%s, then %s) — everything below is a difference between two networks, not drift within one",
			b.Prefix, a.Prefix))
	} else if !sameOrUnknown(b.Gateway, a.Gateway) {
		out = append(out, fmt.Sprintf(
			"the default gateway differs (%s, then %s) — this is probably a different network that happens to use the same addresses, and everything below may be a difference between two networks",
			b.Gateway, a.Gateway))
	}
	if !sameOrUnknown(b.DHCPServer, a.DHCPServer) {
		out = append(out, fmt.Sprintf("the DHCP server differs (%s, then %s)", b.DHCPServer, a.DHCPServer))
	}
	if !sameSet(b.Resolvers, a.Resolvers) {
		out = append(out, fmt.Sprintf("the DNS servers differ (%s, then %s)",
			strings.Join(b.Resolvers, ", "), strings.Join(a.Resolvers, ", ")))
	}
	if !sameSet(b.SearchList, a.SearchList) {
		out = append(out, fmt.Sprintf("the DNS search domains differ (%s, then %s)",
			orNone(b.SearchList), orNone(a.SearchList)))
	}
	if bi, ai := b.InstrumentName(), a.InstrumentName(); bi != ai {
		out = append(out, fmt.Sprintf(
			"the surveys were taken by different instruments (%s, then %s), which see different things — a phone cannot read the ARP cache, so devices only visible at layer 2 will look like they vanished",
			bi, ai))
	}
	return out
}

func sameOrUnknown(b, a string) bool { return b == "" || a == "" || b == a }

// sameSet compares two lists ignoring order; an empty side is unknown.
func sameSet(b, a []string) bool {
	if len(b) == 0 || len(a) == 0 {
		return true
	}
	bs, as := map[string]bool{}, map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	for _, x := range a {
		as[x] = true
	}
	if len(bs) != len(as) {
		return false
	}
	for x := range bs {
		if !as[x] {
			return false
		}
	}
	return true
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
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
	return strings.Join(parts, "; ")
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
	out := "Drift since the last survey: " + strings.Join(parts, ", ") + "."
	if n := r.weakMatches(); n > 0 {
		out += fmt.Sprintf(" %d matched by address only and may be different devices.", n)
	}
	return out
}

func (r Report) weakMatches() int {
	n := 0
	for _, c := range r.Changes {
		if c.MatchedBy != "" && c.Match <= model.Low {
			n++
		}
	}
	return n
}
