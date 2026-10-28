// Package snapshot is the JSON record of a survey.
//
// A survey is persisted twice, on purpose. The JSON snapshot is the record of
// truth for drift: it is text, it diffs in git, a human can read it, and it
// will still open in ten years. The SQLite database (package sqlitedb) is the
// queryable form. Neither is derived from the other at read time: both are
// written from the same model in the same run, so they cannot disagree about
// what was seen.
//
// The two live in separate packages because Go links whole packages: anything
// that imports the database pulls in the SQLite engine, and the Android build,
// which only ever needs this half, should not carry several megabytes of it
// per ABI.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Marshal renders a survey as its canonical JSON.
//
// The graph is sorted first so that two surveys of an unchanged network produce
// byte-identical files. That property is what makes `git diff` on a snapshot a
// legitimate drift report, and it is worth more than it costs.
func Marshal(g *model.Graph) ([]byte, error) {
	g.Sort()
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Unmarshal loads a survey and restores the entity-resolution indexes that are
// rebuilt rather than serialised.
func Unmarshal(b []byte) (*model.Graph, error) {
	var g model.Graph
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, err
	}
	g.Rebuild()
	return &g, nil
}

// Write writes the JSON record of a survey to path.
func Write(path string, g *model.Graph) error {
	b, err := Marshal(g)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Read loads the survey at path.
func Read(path string) (*model.Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g, err := Unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return g, nil
}
