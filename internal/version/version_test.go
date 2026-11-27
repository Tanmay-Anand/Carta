package version

import (
	"regexp"
	"testing"
)

// Gradle derives the Android versionCode as major*10000 + minor*100 + patch,
// so each part must be a plain number and minor and patch must stay below 100.
func TestTheVersionIsOneGradleCanRead(t *testing.T) {
	m := regexp.MustCompile(`^(\d+)\.(\d{1,2})\.(\d{1,2})$`).FindStringSubmatch(Number)
	if m == nil {
		t.Fatalf("VERSION %q is not MAJOR.MINOR.PATCH with minor and patch below 100", Number)
	}
}
