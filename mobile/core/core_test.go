package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// TEST-NET-1: nothing answers there, so a test can never survey a real network.
const testProfile = `{
  "host": "test-phone", "os": "android 16 (API 36)", "interface": "wlan0",
  "ip": "192.0.2.23", "prefix_len": 29, "gateway": "192.0.2.1",
  "resolvers": ["192.0.2.1"], "dhcp_server": "192.0.2.1",
  "confounders": ["Android hides the ARP cache and MAC addresses from apps"]
}`

func TestNewSessionRejectsAProfileItCannotSurvey(t *testing.T) {
	for _, bad := range []string{
		`not json`,
		`{"ip": "fe80::1", "prefix_len": 64}`,
		`{"ip": "192.0.2.23", "prefix_len": 0}`,
	} {
		if _, err := NewSession(bad, ""); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// Cancelling before Run is the same as the deadline passing at once: a
// partial survey that says so, from the Android instrument, with no ARP.
func TestACancelledSessionReturnsAPartialSnapshot(t *testing.T) {
	s, err := NewSession(testProfile, `{"deadline_seconds": 30}`)
	if err != nil {
		t.Fatal(err)
	}
	s.Cancel()
	out, err := s.Run(nil)
	if err != nil {
		t.Fatalf("cancelled survey must not be an error: %v", err)
	}
	var snap struct {
		Vantage struct {
			Instrument  string   `json:"instrument"`
			Confounders []string `json:"confounders"`
		} `json:"vantage"`
		Coverage []struct {
			Notes     []string `json:"notes"`
			Protocols []string `json:"protocols"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Vantage.Instrument != Instrument {
		t.Errorf("instrument %q", snap.Vantage.Instrument)
	}
	if len(snap.Vantage.Confounders) != 1 {
		t.Errorf("the app's confounder must reach the snapshot: %v", snap.Vantage.Confounders)
	}
	notes := strings.Join(snap.Coverage[0].Notes, "\n")
	if !strings.Contains(notes, "cut short (cancelled)") || !strings.Contains(notes, "layer-2 evidence unavailable") {
		t.Errorf("notes:\n%s", notes)
	}

	if _, err := s.Run(nil); err == nil {
		t.Error("a session must refuse to run twice")
	}
}

func TestDiffAndFingerprintReadTheSnapshotsRunProduces(t *testing.T) {
	s, _ := NewSession(testProfile, "")
	s.Cancel()
	snap, err := s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp, err := Fingerprint(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"prefix":"192.0.2.16/29"`, `"gateway":"192.0.2.1"`, `"instrument":"carta-android"`, `"cut_short":"listen"`} {
		if !strings.Contains(fp, want) {
			t.Errorf("fingerprint missing %s: %s", want, fp)
		}
	}

	d, err := Diff(snap, snap)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Comparable bool   `json:"comparable"`
		Summary    string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(d), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Comparable || !strings.Contains(r.Summary, "No drift") {
		t.Errorf("a survey compared with itself: %s", d)
	}
}
