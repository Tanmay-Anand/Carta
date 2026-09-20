// Package model holds CARTA's view of a network: entities, the claims made
// about them, and how much each claim deserves to be believed.
//
// The central design rule of this package is that a map is not a picture, it
// is a set of claims about the world, and those claims arrive with wildly
// different qualities of evidence. "10.1.4.22 completed a TCP handshake" is an
// observation. "10.1.4.22 is a Dell" is an inference from an OUI that may have
// been resold, randomised, or inherited from a hypervisor. A tool that renders
// both as an identical box is worse than no tool, because an engineer plans
// changes against it and cannot tell which half is wrong.
//
// So confidence is not decoration here. It is carried on every claim, it is
// never averaged into a false probability, and it is printed next to the claim
// in every rendered document.
package model

import "fmt"

// Confidence is a deliberately coarse ordinal scale.
//
// Four levels, no percentages. A percentage implies a calibration nobody has
// done: there is no corpus that says an OUI match is "73% reliable", and
// inventing one would launder a guess into a number that looks measured. Four
// levels can be assigned consistently by rule, which is the property that
// actually matters when two surveys are compared months apart.
type Confidence int

const (
	// Unknown is the zero value: no evidence either way. It exists so that an
	// unset field is visibly unset rather than silently "low".
	Unknown Confidence = iota

	// Low: an inference from a signal that is trivially altered or frequently
	// wrong. TTL-based OS guesses, self-reported banner strings, device class
	// inferred from an open port.
	Low

	// Moderate: an inference from a signal that is usually right but has known
	// systematic failure modes. OUI vendor lookup, shared-broadcast-domain
	// inferred from co-presence in an ARP cache, hostname from reverse DNS
	// (which records what the operator once typed, not what the device is).
	Moderate

	// High: direct observation by this instrument. The address answered a TCP
	// handshake; the MAC appeared in the host's own ARP cache after traffic was
	// exchanged; the TLS certificate was served by the endpoint itself.
	High

	// Asserted: the device stated it about itself, unprompted, in a protocol
	// designed for that purpose. An mDNS service record, an SSDP device
	// description, a NetBIOS name reply. Still not proof — anything can lie —
	// but it is the device's own claim rather than our deduction.
	Asserted
)

// Label is the single word printed in reports and shown in the UI.
func (c Confidence) Label() string {
	switch c {
	case Asserted:
		return "asserted"
	case High:
		return "observed"
	case Moderate:
		return "inferred"
	case Low:
		return "weak"
	default:
		return "unknown"
	}
}

func (c Confidence) String() string { return c.Label() }

// MarshalJSON writes the label rather than the integer, so a snapshot committed
// to git diffs as words and survives a future reordering of the constants.
func (c Confidence) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", c.Label())), nil
}

func (c *Confidence) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"asserted"`:
		*c = Asserted
	case `"observed"`:
		*c = High
	case `"inferred"`:
		*c = Moderate
	case `"weak"`:
		*c = Low
	case `"unknown"`:
		*c = Unknown
	default:
		return fmt.Errorf("model: unknown confidence %s", b)
	}
	return nil
}

// Promotion rules.
//
// Two independent sources agreeing is worth something, but not everything, and
// the rules for what it is worth are fixed here rather than decided ad hoc at
// each call site.

// Corroborate returns the confidence of a claim that two independent methods
// agree on. Agreement promotes by exactly one level and never reaches Asserted,
// because Asserted means "the device said so" and no amount of third-party
// agreement turns an inference into a self-statement.
//
// Two sources of the SAME kind do not corroborate — an OUI read twice is one
// piece of evidence — so callers must only pass distinct methods. Independence
// is a judgement the caller makes; this function does the arithmetic.
func Corroborate(a, b Confidence) Confidence {
	best := max(a, b)
	if a == Unknown || b == Unknown {
		return best
	}
	if best >= High {
		return best
	}
	return best + 1
}

// Conflict returns the confidence to carry when two sources disagree about the
// same key. The higher source wins the value, but it is demoted one level,
// because a contradiction is evidence that at least one of our methods is wrong
// about this device and we do not know which.
//
// The losing claim is never discarded: Entity.Claims keeps both, and the
// rendered inventory prints the disagreement. A survey that quietly picks a
// winner is how a wrong map becomes a confident map.
func Conflict(winner Confidence) Confidence {
	if winner <= Low {
		return Low
	}
	return winner - 1
}
