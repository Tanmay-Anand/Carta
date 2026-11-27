// Package version is the one place Carta's version lives.
//
// The CLI, the desktop app and the Android app all report it, and two
// discovery probes put it in their User-Agent. It is a file rather than a
// constant so that Gradle can read the same bytes the Go binaries embed: the
// release workflow checks the tag against it, and Obtainium compares the tag
// with the installed versionName, so the three must never disagree.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// Number is the release, MAJOR.MINOR.PATCH.
var Number = strings.TrimSpace(raw)
