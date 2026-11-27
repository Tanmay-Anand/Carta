// Package pack writes a finished survey to disk: the document pack, the JSON
// snapshot, the SQLite database, the drift report against the previous
// survey, and the baseline the next survey will be compared with.
//
// It is one function so that every instrument that writes surveys to a
// folder (the CLI and the desktop app) writes exactly the same files. Two
// copies of this sequence would drift apart, and a survey folder that depends
// on which program took it is a folder nobody can trust.
package pack

import (
	"os"
	"path/filepath"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
	"github.com/Tanmay-Anand/carta/internal/render"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
	"github.com/Tanmay-Anand/carta/internal/store/sqlitedb"
)

// Result is what Save wrote.
type Result struct {
	Dir   string   // the survey's own folder, OUT/SURVEY_ID
	Files []string // every file written into Dir
	// Drift compares this survey with the previous baseline. Nil for the first
	// survey written to OUT.
	Drift *drift.Report
}

// BaselineName is the most recent survey, kept at a stable path in OUT so the
// next run has something to compare with without anyone remembering a name.
const BaselineName = "baseline.json"

// Save writes the survey into OUT/SURVEY_ID and makes it the new baseline.
func Save(out string, g *model.Graph, prof *orient.Profile) (Result, error) {
	dir := filepath.Join(out, g.SurveyID)
	res := Result{Dir: dir}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	if err := snapshot.Write(filepath.Join(dir, "survey.json"), g); err != nil {
		return res, err
	}
	res.Files = append(res.Files, filepath.Join(dir, "survey.json"))
	if err := sqlitedb.Write(filepath.Join(dir, "survey.db"), g); err != nil {
		return res, err
	}
	res.Files = append(res.Files, filepath.Join(dir, "survey.db"))
	docs, err := render.Pack(dir, g, prof)
	res.Files = append(res.Files, docs...)
	if err != nil {
		return res, err
	}

	baseline := filepath.Join(out, BaselineName)
	if prev, err := snapshot.Read(baseline); err == nil {
		rep := drift.Compare(prev, g)
		p := filepath.Join(dir, "drift.md")
		if err := os.WriteFile(p, []byte(render.Drift(rep)), 0o644); err != nil {
			return res, err
		}
		res.Files = append(res.Files, p)
		res.Drift = &rep
	}
	return res, snapshot.Write(baseline, g)
}
