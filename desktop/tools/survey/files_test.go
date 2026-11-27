package survey

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/pack"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
)

func TestTheFirstRunSaysNoSurveyYet(t *testing.T) {
	tool := openedIn(t, t.TempDir())
	if tool.st.g != nil {
		t.Fatal("an empty folder produced a survey")
	}
	got := texts(tool.root)
	for _, want := range []string{"No survey yet", "The map appears after the first survey.", "Ready"} {
		if !contains(got, want) {
			t.Errorf("the first run does not say %q: %v", want, got)
		}
	}
}

// A survey is saved as the CLI saves one, marked as the desktop's, and is what
// the app opens on next time.
func TestASurveyIsSavedAndReopenedOnStart(t *testing.T) {
	dir := t.TempDir()
	tool := openedIn(t, dir)
	tool.start(false)
	waitFinished(t, tool)
	id := tool.st.g.SurveyID
	for _, f := range []string{filepath.Join(id, "survey.json"), filepath.Join(id, "inventory.md"), pack.BaselineName} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("not saved: %s", f)
		}
	}
	if tool.st.run.savedTo != filepath.Join(dir, id) {
		t.Errorf("saved to %q", tool.st.run.savedTo)
	}
	saved, _ := snapshot.Read(filepath.Join(dir, id, "survey.json"))
	if saved.Vantage.Instrument != Instrument {
		t.Errorf("saved as instrument %q, want %q", saved.Vantage.Instrument, Instrument)
	}

	again := openedIn(t, dir)
	if again.st.g == nil || again.st.g.SurveyID != id {
		t.Fatal("the app did not reopen on the last survey")
	}
}

func TestOpenShowsASavedSurveyWithoutComparing(t *testing.T) {
	tool := opened(t, 0)
	if err := tool.open("../../../internal/store/snapshot/testdata/v1-earlier.json"); err != nil {
		t.Fatal(err)
	}
	if tool.st.g.SurveyID != "20261116T091400Z" || tool.st.earlier != nil || tool.st.drift != nil {
		t.Errorf("opened %q, earlier %v, drift %v", tool.st.g.SurveyID, tool.st.earlier, tool.st.drift)
	}
	if err := tool.open("no-such-file.json"); err == nil {
		t.Error("opening a missing file did not fail")
	}
}

func TestCompareListsEarlierSurveysAndOpensDrift(t *testing.T) {
	dir := t.TempDir()
	for _, g := range []string{"v1-earlier.json", "v1.json"} {
		s, _ := snapshot.Read("../../../internal/store/snapshot/testdata/" + g)
		if _, err := pack.Save(dir, s, &orient.Profile{}); err != nil {
			t.Fatal(err)
		}
	}
	tool := openedIn(t, dir)
	earlier := tool.earlierSurveys()
	if len(earlier) != 1 || earlier[0].SurveyID != "20261116T091400Z" {
		t.Fatalf("earlier surveys: %d", len(earlier))
	}
	tool.compareWith(earlier[0])
	if tool.st.tab != tabDrift || tool.st.drift == nil || len(tool.st.drift.Changes) == 0 {
		t.Fatal("comparing did not open the drift")
	}
	if !contains(texts(tool.right), "MOVED") {
		t.Error("the drift tab does not show the camera that moved")
	}
	tool.clearComparison()
	if tool.st.drift != nil || tool.st.tab == tabDrift {
		t.Error("clearing the comparison left it showing")
	}
}

// Closing the window mid-survey cancels it, and the survey is still saved,
// complete and readable, before Stop returns.
func TestClosingMidSurveyStillSavesWhatWasFound(t *testing.T) {
	dir := t.TempDir()
	tool := openedIn(t, dir)
	tool.runner = fakeRunner{step: 20 * time.Millisecond}
	tool.start(false)
	tool.Stop()
	entries, _ := os.ReadDir(dir)
	var found bool
	for _, e := range entries {
		if e.IsDir() {
			if g, err := snapshot.Read(filepath.Join(dir, e.Name(), "survey.json")); err == nil {
				found = g.Meta["cut_short"] != ""
			}
		}
	}
	if !found {
		t.Error("closing mid-survey left no saved, cut-short survey")
	}
}
