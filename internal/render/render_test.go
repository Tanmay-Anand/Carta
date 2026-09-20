package render

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/store"
)

func sample() (*model.Graph, *orient.Profile) {
	at := time.Unix(1700000000, 0).UTC()
	g := model.NewGraph("20260101T000000Z")
	g.StartedAt = at
	g.Vantage = model.Vantage{
		Host: "msi", Interface: "wlan0", IP: "192.168.0.7", Prefix: "192.168.0.0/24",
		Gateway: "192.168.0.1", Resolvers: []string{"192.168.0.1"}, OS: "windows/amd64",
		Confounders: []string{"VPN-like interface up: wg0"},
	}
	g.Meta["tier"] = "P + 0 (unprivileged)"

	g.Observe([]model.Identity{model.IPIdentity("192.168.0.1"), model.MACIdentity("b8:fb:b3:95:58:0d")},
		model.Claim{Key: "alive", Value: "yes", Confidence: model.High, Source: model.Source{Method: "arp-cache", At: at}},
		model.Claim{Key: "hostname", Value: "Archer-C5", Confidence: model.Asserted, Source: model.Source{Method: "upnp-description", At: at}},
		model.Claim{Key: "class", Value: "router / gateway", Confidence: model.High, Source: model.Source{Method: "route-table", At: at}},
		model.Claim{Key: "service", Value: "telnet (23/tcp)", Confidence: model.Moderate, Source: model.Source{Method: "tcp-connect", At: at}},
	)
	// A device that answered and revealed nothing: the case the whole design is
	// about.
	g.Observe([]model.Identity{model.IPIdentity("192.168.0.4"), model.MACIdentity("7e:5d:c3:c7:a9:be")},
		model.Claim{Key: "alive", Value: "yes", Confidence: model.High, Source: model.Source{Method: "tcp-connect", At: at}},
		model.Claim{Key: "probed", Value: "no open ports in the risk-ranked set", Confidence: model.High, Source: model.Source{Method: "tcp-connect", At: at}},
	)
	g.Coverage = []model.Coverage{{
		Prefix: "192.168.0.0/24", AddrsSwept: 254, AddrsTotal: 254, Responded: 2,
		PortsProbed: []int{22, 80, 443}, Protocols: []string{"mdns", "ssdp", "arp-cache"},
		StartedAt: at, EndedAt: at.Add(30 * time.Second),
	}}
	g.AddFinding(model.Finding{Severity: "risk", Title: "1 device offers plaintext remote access",
		Detail: "telnet", Confidence: model.High})
	g.Sort()

	prof := &orient.Profile{
		Host: "msi", Interface: "wlan0", DHCPServer: "192.168.0.1",
		Interfaces: []orient.InterfaceInfo{{Name: "wlan0", MAC: "dc:56:7b:64:5b:bd", Addrs: []string{"192.168.0.7/24"}, Up: true}},
	}
	return g, prof
}

func TestInventoryMarksWhatWasNotLearnedRatherThanLeavingItBlank(t *testing.T) {
	g, _ := sample()
	out := Inventory(g)

	if !strings.Contains(out, "—") {
		t.Error("unknown fields must be marked, not left empty")
	}
	// Every identifying claim carries its confidence into the document.
	if !strings.Contains(out, "asserted") || !strings.Contains(out, "observed") {
		t.Errorf("confidence labels missing from the inventory:\n%s", out)
	}
	if !strings.Contains(out, "Responded, but revealed nothing") {
		t.Error("silent-but-present devices need their own section")
	}
	if !strings.Contains(out, "192.168.0.4") {
		t.Error("a device that revealed nothing is still a device and must appear")
	}
}

func TestAsBuiltStatesCoverageAndInstrumentBias(t *testing.T) {
	g, prof := sample()
	out := AsBuilt(g, prof)

	for _, want := range []string{"192.168.0.0/24", "Known bias in this instrument", "VPN-like interface up", "Coverage"} {
		if !strings.Contains(out, want) {
			t.Errorf("as-built is missing %q", want)
		}
	}
}

func TestIncompleteCoverageIsCalledOut(t *testing.T) {
	g, prof := sample()
	g.Coverage[0].AddrsSwept = 100
	out := AsBuilt(g, prof)
	if !strings.Contains(out, "unknown**, not empty") {
		t.Errorf("a partial sweep must say so:\n%s", out)
	}
}

func TestMermaidShapesCarryHowWellADeviceIsKnown(t *testing.T) {
	g, _ := sample()
	out := Mermaid(g)

	if !strings.Contains(out, `nip_192_168_0_1["Archer-C5`) {
		t.Errorf("a named device should be a box:\n%s", out)
	}
	if !strings.Contains(out, `nip_192_168_0_4(("192.168.0.4`) {
		t.Errorf("an unidentified device should be a circle:\n%s", out)
	}
	if !strings.Contains(out, "no response — unknown, not empty") {
		t.Error("unanswered address space must be drawn, not omitted")
	}
	if !strings.Contains(out, "the internet") {
		t.Error("the frontier beyond the gateway belongs on the map")
	}
}

func TestRenderedTextCannotBreakOutOfItsTableCell(t *testing.T) {
	// A hostile device can name itself anything; the renderer is the boundary.
	at := time.Unix(1700000000, 0)
	g, _ := sample()
	g.Observe([]model.Identity{model.IPIdentity("192.168.0.9")},
		model.Claim{Key: "alive", Value: "yes", Confidence: model.High, Source: model.Source{Method: "tcp-connect", At: at}},
		model.Claim{Key: "hostname", Value: "evil | --- | injected", Confidence: model.Asserted, Source: model.Source{Method: "mdns", At: at}})
	g.Sort()

	out := Inventory(g)
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "injected") {
			continue
		}
		// Count only the pipes still acting as cell separators: an escaped pipe
		// is inert, which is the whole point of escaping it.
		separators := strings.Count(line, "|") - strings.Count(line, `\|`)
		if separators != 8 {
			t.Errorf("a crafted hostname changed the table shape (%d separators): %q", separators, line)
		}
		if !strings.Contains(line, `\|`) {
			t.Errorf("the pipe in a hostname must be escaped: %q", line)
		}
	}
}

func TestPackWritesEveryDocumentAndTheyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	g, prof := sample()

	files, err := Pack(dir, g, prof)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 7 {
		t.Errorf("expected 7 documents, got %d: %v", len(files), files)
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() == 0 {
			t.Errorf("%s: empty or missing", filepath.Base(f))
		}
	}

	// The snapshot is the drift baseline, so it has to survive a round trip
	// with its claims and confidences intact.
	snap := filepath.Join(dir, "survey.json")
	if err := store.WriteSnapshot(snap, g); err != nil {
		t.Fatal(err)
	}
	back, err := store.ReadSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Entities) != len(g.Entities) {
		t.Fatalf("entity count changed across the round trip: %d → %d", len(g.Entities), len(back.Entities))
	}
	e := back.Entity(model.IPIdentity("192.168.0.1"))
	if e == nil {
		t.Fatal("entity lookup broken after load — the union-find index was not rebuilt")
	}
	if v, conf := e.Best("hostname"); v != "Archer-C5" || conf != model.Asserted {
		t.Errorf("claim lost fidelity: %q at %v", v, conf)
	}
}

func TestDatabaseIsQueryable(t *testing.T) {
	dir := t.TempDir()
	g, _ := sample()
	path := filepath.Join(dir, "survey.db")
	if err := store.WriteDB(path, g); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entity`).Scan(&n); err != nil || n != 2 {
		t.Errorf("entity rows: %d (%v)", n, err)
	}
	// The question the SQLite form exists to answer.
	var label, conf string
	err = db.QueryRow(`SELECT e.label, c.confidence FROM entity e
	                   JOIN claim c ON c.entity_id = e.id
	                   WHERE c.key = 'service' AND c.value LIKE 'telnet%'`).Scan(&label, &conf)
	if err != nil {
		t.Fatalf("join over claims failed: %v", err)
	}
	if label != "Archer-C5" || conf != "inferred" {
		t.Errorf("got %q / %q", label, conf)
	}
}
