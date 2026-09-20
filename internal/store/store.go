// Package store persists a survey twice, on purpose.
//
// The JSON snapshot is the record of truth for drift: it is text, it diffs in
// git, a human can read it, and it will still open in ten years. The SQLite
// database is the queryable form — "which devices appeared since March and
// offer SMB" is a join, not a script — and it is what the UI reads.
//
// Neither is derived from the other at read time: both are written from the
// same model in the same run, so they cannot disagree about what was seen.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so the binary still cross-compiles

	"github.com/Tanmay-Anand/carta/internal/model"
)

// WriteSnapshot writes the JSON record of a survey.
//
// The graph is sorted first so that two surveys of an unchanged network produce
// byte-identical files. That property is what makes `git diff` on a snapshot a
// legitimate drift report, and it is worth more than it costs.
func WriteSnapshot(path string, g *model.Graph) error {
	g.Sort()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ReadSnapshot loads a survey and restores the entity-resolution indexes that
// are rebuilt rather than serialised.
func ReadSnapshot(path string) (*model.Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g model.Graph
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	g.Rebuild()
	return &g, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS survey (
  id TEXT PRIMARY KEY, started_at TEXT, host TEXT, interface TEXT, ip TEXT,
  prefix TEXT, gateway TEXT, wireless TEXT, os TEXT, tier TEXT
);
CREATE TABLE IF NOT EXISTS entity (
  survey_id TEXT, id TEXT, label TEXT, hostname TEXT, hostname_conf TEXT,
  vendor TEXT, vendor_conf TEXT, class TEXT, class_conf TEXT, alive TEXT,
  PRIMARY KEY (survey_id, id)
);
CREATE TABLE IF NOT EXISTS identity (
  survey_id TEXT, entity_id TEXT, kind TEXT, value TEXT
);
CREATE TABLE IF NOT EXISTS claim (
  survey_id TEXT, entity_id TEXT, key TEXT, value TEXT,
  confidence TEXT, method TEXT, evidence TEXT, at TEXT
);
CREATE TABLE IF NOT EXISTS edge (
  survey_id TEXT, from_id TEXT, to_id TEXT, kind TEXT, confidence TEXT, method TEXT
);
CREATE TABLE IF NOT EXISTS coverage (
  survey_id TEXT, prefix TEXT, swept INTEGER, total INTEGER, responded INTEGER,
  protocols TEXT, frontier TEXT, notes TEXT
);
CREATE TABLE IF NOT EXISTS finding (
  survey_id TEXT, severity TEXT, title TEXT, detail TEXT, confidence TEXT
);
CREATE INDEX IF NOT EXISTS idx_claim_entity ON claim(survey_id, entity_id);
CREATE INDEX IF NOT EXISTS idx_identity_value ON identity(value);
`

// WriteDB writes the queryable form of a survey.
func WriteDB(path string, g *model.Graph) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = os.Remove(path) // a survey directory holds exactly one survey
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(schema); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	v := g.Vantage
	if _, err := tx.Exec(`INSERT INTO survey VALUES (?,?,?,?,?,?,?,?,?,?)`,
		g.SurveyID, g.StartedAt.Format("2006-01-02T15:04:05Z07:00"), v.Host, v.Interface, v.IP,
		v.Prefix, v.Gateway, v.Wireless, v.OS, g.Meta["tier"]); err != nil {
		return err
	}

	for _, e := range g.Entities {
		host, hostConf := e.Best("hostname")
		vendor, vendorConf := e.Best("vendor")
		class, classConf := e.Best("class")
		alive, _ := e.Best("alive")
		if _, err := tx.Exec(`INSERT INTO entity VALUES (?,?,?,?,?,?,?,?,?,?)`,
			g.SurveyID, e.ID, e.Label(), host, hostConf.Label(), vendor, vendorConf.Label(),
			class, classConf.Label(), alive); err != nil {
			return err
		}
		for _, id := range e.Identities {
			if _, err := tx.Exec(`INSERT INTO identity VALUES (?,?,?,?)`,
				g.SurveyID, e.ID, id.Kind(), id.Value()); err != nil {
				return err
			}
		}
		for _, c := range e.Claims {
			if _, err := tx.Exec(`INSERT INTO claim VALUES (?,?,?,?,?,?,?,?)`,
				g.SurveyID, e.ID, c.Key, c.Value, c.Confidence.Label(),
				c.Source.Method, c.Source.Evidence, c.Source.At.Format("2006-01-02T15:04:05Z07:00")); err != nil {
				return err
			}
		}
	}
	for _, ed := range g.Edges {
		if _, err := tx.Exec(`INSERT INTO edge VALUES (?,?,?,?,?,?)`,
			g.SurveyID, ed.From, ed.To, string(ed.Kind), ed.Confidence.Label(), ed.Source.Method); err != nil {
			return err
		}
	}
	for _, c := range g.Coverage {
		if _, err := tx.Exec(`INSERT INTO coverage VALUES (?,?,?,?,?,?,?,?)`,
			g.SurveyID, c.Prefix, c.AddrsSwept, c.AddrsTotal, c.Responded,
			joinCSV(c.Protocols), joinCSV(c.Frontier), joinCSV(c.Notes)); err != nil {
			return err
		}
	}
	for _, f := range g.Findings {
		if _, err := tx.Exec(`INSERT INTO finding VALUES (?,?,?,?,?)`,
			g.SurveyID, f.Severity, f.Title, f.Detail, f.Confidence.Label()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func joinCSV(xs []string) string {
	b, _ := json.Marshal(xs)
	return string(b)
}
