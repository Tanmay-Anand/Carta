//go:build tools

// Package tools pins golang.org/x/mobile in go.mod. gomobile bind needs its
// bind package resolvable from this module, and nothing else imports it, so
// without this `go mod tidy` would drop it and the next bind would fail.
package tools

import _ "golang.org/x/mobile/bind"
