package components

import (
	"os"
	"testing"
)

// Component tests exercise executors against httptest servers on loopback, so
// the SSRF guard must allow private egress for the package's test run. The
// dedicated SSRF test toggles it off explicitly.
func TestMain(m *testing.M) {
	AllowPrivateEgress = true
	os.Exit(m.Run())
}
