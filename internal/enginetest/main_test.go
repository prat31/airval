package enginetest

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if any goroutine outlives the harnesses (worker stages, ingester,
// SSE streams, leases, NATS consumers…). Process-wide pools owned by libraries are ignored.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		goleak.IgnoreAnyFunction("github.com/klauspost/compress/zstd.(*blockDec).startDecoder"),
		goleak.IgnoreAnyFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreAnyFunction("go.opencensus.io/stats/view.(*worker).start"),
	)
}
