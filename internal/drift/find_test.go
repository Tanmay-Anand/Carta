package drift_test

import (
	"testing"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
)

func pair(t *testing.T) (before, after *model.Graph) {
	t.Helper()
	before, err := snapshot.Read("../store/snapshot/testdata/v1-earlier.json")
	if err != nil {
		t.Fatal(err)
	}
	after, err = snapshot.Read("../store/snapshot/testdata/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return before, after
}

func byIP(g *model.Graph, ip string) *model.Entity {
	return g.Entity(model.IPIdentity(ip))
}

// The camera moved from .61 to .60 between the surveys; its burned-in MAC
// still finds it, and that is an observation-strength match.
func TestFindFollowsAMovedDeviceByItsMAC(t *testing.T) {
	before, after := pair(t)
	m, by, conf := drift.Find(byIP(before, "192.168.10.61"), after)
	if m == nil || by != "mac" || conf != model.High {
		t.Fatalf("got %v by %q (%v)", m, by, conf)
	}
	if addrs := m.Addresses(); len(addrs) != 1 || addrs[0] != "192.168.10.60" {
		t.Errorf("matched %v, not the camera's new address", addrs)
	}
}

// A phone with a randomised MAC can be found only by its address, which on a
// DHCP network may now belong to something else, and Find says it is weak.
func TestFindSaysAnAddressOnlyMatchIsWeak(t *testing.T) {
	before, after := pair(t)
	m, by, conf := drift.Find(byIP(before, "192.168.10.41"), after)
	if m == nil || by != "ip" || conf != model.Low {
		t.Fatalf("got %v by %q (%v)", m, by, conf)
	}
}

// A device the newer survey never saw is not found, rather than matched to
// whatever is nearest.
func TestFindReturnsNothingForADeviceThatDidNotAnswer(t *testing.T) {
	before, after := pair(t)
	// .42 is new in the later survey, so the earlier one has nothing to find.
	m, by, conf := drift.Find(byIP(after, "192.168.10.42"), before)
	if m != nil || by != "" || conf != model.Unknown {
		t.Errorf("found %v by %q (%v) for a device that was not there", m, by, conf)
	}
	if m, _, _ := drift.Find(nil, before); m != nil {
		t.Error("nil entity matched something")
	}
}
