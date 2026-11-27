package snapshot_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/render"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
)

// testdata/v1.json and v1-earlier.json are frozen surveys of a fictional
// 192.168.10.0/24 (documentation MACs from RFC 7042), written in format 1.
// Carta 1 promises that every 1.x reads them: never regenerate these files to
// make a test pass. A change that breaks this test is a format change, and a
// format change is Carta 2.

func TestFormat1SurveysStillRead(t *testing.T) {
	for _, name := range []string{"testdata/v1.json", "testdata/v1-earlier.json"} {
		g, err := snapshot.Read(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if g.Format != 1 || g.Vantage.Prefix != "192.168.10.0/24" {
			t.Errorf("%s: format %d, prefix %q", name, g.Format, g.Vantage.Prefix)
		}
		alive := 0
		for _, e := range g.Entities {
			if v, _ := e.Best("alive"); v == "yes" {
				alive++
			}
		}
		if alive < 13 {
			t.Errorf("%s: %d devices alive; the fixture has 13 or 14", name, alive)
		}
	}
}

func TestFormat1SurveysStillDiff(t *testing.T) {
	before, _ := snapshot.Read("testdata/v1-earlier.json")
	after, _ := snapshot.Read("testdata/v1.json")
	rep := drift.Compare(before, after)
	kinds := map[string]bool{}
	for _, c := range rep.Changes {
		kinds[c.Kind] = true
		if c.Kind == "moved" && c.MatchedBy != "mac" {
			t.Errorf("the camera moved but was matched by %q, not its MAC", c.MatchedBy)
		}
	}
	for _, k := range []string{"appeared", "moved", "mutated"} {
		if !kinds[k] {
			t.Errorf("no %q change between the two fixtures: %+v", k, rep.Changes)
		}
	}
}

func TestFormat1SurveysStillRender(t *testing.T) {
	g, _ := snapshot.Read("testdata/v1.json")
	dir := t.TempDir()
	files, err := render.Pack(dir, g, &orient.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 7 {
		t.Errorf("rendered %d files", len(files))
	}
	inv, _ := os.ReadFile(dir + "/inventory.md")
	if !strings.Contains(string(inv), "nas.local") {
		t.Error("the inventory lost a device")
	}
}

// Writing a survey stamps the current format, and reading a pre-1.0 file
// without one treats it as format 1.
func TestFormatIsStampedAndDefaulted(t *testing.T) {
	g, _ := snapshot.Read("testdata/v1.json")
	g.Format = 0
	b, err := snapshot.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"format": 1`)) {
		t.Error("Marshal did not stamp the format")
	}
	old := bytes.Replace(b, []byte(`"format": 1,`), nil, 1)
	g2, err := snapshot.Unmarshal(old)
	if err != nil || g2.Format != 1 {
		t.Errorf("a snapshot without a format read as %d, %v", g2.Format, err)
	}
}

// A survey from a newer Carta is refused, not half-read.
func TestANewerFormatIsRefused(t *testing.T) {
	b, _ := os.ReadFile("testdata/v1.json")
	newer := bytes.Replace(b, []byte(`"format": 1`), []byte(`"format": 2`), 1)
	if _, err := snapshot.Unmarshal(newer); err == nil || !strings.Contains(err.Error(), "newer Carta") {
		t.Errorf("format 2 was accepted: %v", err)
	}
	if model.Format != 1 {
		t.Errorf("model.Format is %d; Carta 1 writes format 1", model.Format)
	}
}
