package pack

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
)

// The first survey into a folder writes the whole pack and becomes the
// baseline; the second also writes drift.md against the first.
func TestSaveWritesThePackAndKeepsABaseline(t *testing.T) {
	out := t.TempDir()
	earlier, err := snapshot.Read("../store/snapshot/testdata/v1-earlier.json")
	if err != nil {
		t.Fatal(err)
	}
	later, _ := snapshot.Read("../store/snapshot/testdata/v1.json")

	first, err := Save(out, earlier, &orient.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Drift != nil {
		t.Error("the first survey into a folder has nothing to drift from")
	}
	want := []string{"survey.json", "survey.db", "README.md", "inventory.md", "inventory.csv",
		"topology.mmd", "addressing.md", "findings.md", "provenance.md"}
	got := names(first.Files)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing %s in %v", w, got)
		}
	}
	if b, err := snapshot.Read(filepath.Join(out, BaselineName)); err != nil || b.SurveyID != earlier.SurveyID {
		t.Fatalf("baseline is not the first survey: %v", err)
	}

	second, err := Save(out, later, &orient.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Drift == nil || len(second.Drift.Changes) == 0 {
		t.Fatal("the second survey wrote no drift")
	}
	if !slices.Contains(names(second.Files), "drift.md") {
		t.Error("drift.md missing from the second survey's files")
	}
	if b, _ := snapshot.Read(filepath.Join(out, BaselineName)); b.SurveyID != later.SurveyID {
		t.Error("the baseline did not move to the newest survey")
	}
	for _, f := range second.Files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("listed but not written: %s", f)
		}
	}
}

func names(paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}
