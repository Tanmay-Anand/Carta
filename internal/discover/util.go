package discover

import (
	"errors"
	"strconv"
	"strings"
	"syscall"
)

func itoa(i int) string { return strconv.Itoa(i) }

// isRefused distinguishes "the host said no" from "nothing was there".
//
// The distinction is the difference between a device that exists and an empty
// address, and it is worth the platform-specific ugliness: a TCP RST can only
// come from a live IP stack, while a timeout is silence, which is not evidence
// of absence.
func isRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	// Windows returns WSAECONNREFUSED (10061), which is not syscall.ECONNREFUSED
	// on that platform, so fall back to the message. Checking the string is
	// unpleasant but stable, and the alternative is a per-OS file for one case.
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "refused") || strings.Contains(s, "10061")
}
