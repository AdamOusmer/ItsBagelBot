package core

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shared transport must arm the HTTP/2 health check. Real network
// behaviour is not what is asserted here (a silently-reaped connection cannot
// be staged in a unit test); the configuration is, because the whole failure
// mode is a field left at its zero value: with SendPingTimeout unset the h2
// transport never pings, and since every request to a host rides one h2
// connection, a dead one hangs requests until the client timeout fires.
func TestSharedTransportArmsH2HealthCheck(t *testing.T) {
	tr := newSharedTransport()

	// Without ForceAttemptHTTP2 the bundled h2 transport is never installed
	// and HTTP2Config below would be dead configuration.
	assert.True(t, tr.ForceAttemptHTTP2)

	require.NotNil(t, tr.HTTP2, "h2 config must be set or the transport never health-checks")
	assert.Equal(t, h2ReadIdleTimeout, tr.HTTP2.SendPingTimeout)
	assert.Equal(t, h2PingTimeout, tr.HTTP2.PingTimeout)
	assert.NotZero(t, tr.HTTP2.SendPingTimeout, "a zero SendPingTimeout disarms the health-check timer")
}

// The two h2 timeouts only pay off inside the budgets around them: the ping
// verdict has to land while the request is still alive, and the read-idle
// window has to be shorter than the idle-connection reap it is meant to catch
// inside of.
func TestSharedTransportH2TimeoutsFitBudgets(t *testing.T) {
	tr := newSharedTransport()
	require.NotNil(t, tr.HTTP2)

	// defaultClientTimeout is the per-call budget NewHTTPClient falls back to.
	const defaultClientTimeout = 10 * time.Second
	assert.Less(t, tr.HTTP2.PingTimeout, defaultClientTimeout,
		"the PONG verdict must arrive before the request's own deadline, or it is useless")
	assert.Less(t, tr.HTTP2.SendPingTimeout, tr.IdleConnTimeout,
		"a connection reaped inside the idle window is exactly what the ping is for")
}

// The idle window and the h2 health check are one setting wearing two names.
// Holding connections for minutes is only safe while the ping is armed, so a
// connection kept to the end of the window must have proved itself alive many
// times over before the last request rides it. Weakening either side alone
// restores the pre-ping failure: a silently-reaped socket handed to a request
// that then hangs for the whole client timeout.
func TestSharedTransportIdleWindowIsCoveredByHealthCheck(t *testing.T) {
	tr := newSharedTransport()
	require.NotNil(t, tr.HTTP2)
	require.NotZero(t, tr.HTTP2.SendPingTimeout,
		"an idle window this long with the health check disarmed is exactly the old bug")

	// Every ping is one liveness proof. Requiring the window to be worth many
	// of them keeps the pairing honest whichever side a future edit moves.
	const minLivenessProofs = 8
	proofs := int(tr.IdleConnTimeout / tr.HTTP2.SendPingTimeout)
	assert.GreaterOrEqual(t, proofs, minLivenessProofs,
		"a connection held this long must be pinged far more often than it is reaped")

	// A lost ping has to reach its verdict inside the window as well, or the
	// reap would beat the detection to a connection nobody proved dead.
	assert.Less(t, tr.HTTP2.SendPingTimeout+tr.HTTP2.PingTimeout, tr.IdleConnTimeout,
		"the full detect-and-close cycle must fit inside the idle window")
}

// The raise exists so the pool survives the quiet gap between chat command
// bursts, which the stock 90s does not. A value that drifted back toward the
// default would silently restore a TLS handshake on the first request of every
// burst, with nothing else failing to signal it.
func TestSharedTransportIdleWindowOutlastsBurstGaps(t *testing.T) {
	stock := http.DefaultTransport.(*http.Transport).IdleConnTimeout
	require.Equal(t, 90*time.Second, stock,
		"net/http's default is the baseline this setting is a deliberate departure from")

	tr := newSharedTransport()
	assert.Equal(t, idleConnTimeout, tr.IdleConnTimeout)
	assert.Greater(t, tr.IdleConnTimeout, stock,
		"inheriting the stock window means paying a handshake per burst")
	assert.GreaterOrEqual(t, tr.IdleConnTimeout, 5*time.Minute,
		"per-replica gaps to one upstream run minutes once three replicas split the load")
}

// The HTTP/1 idle pool is sized for bursts too. It is dead configuration for
// h2 upstreams — those connections live in the bundled transport's own pool,
// which never reads these fields — but it is the whole pool for any upstream
// that falls back to HTTP/1.1, and the stock per-host default of 2 would put a
// handshake in front of most of a burst there.
func TestSharedTransportSizesTheHTTP1IdlePool(t *testing.T) {
	tr := newSharedTransport()
	assert.Greater(t, tr.MaxIdleConnsPerHost, http.DefaultMaxIdleConnsPerHost,
		"the stingy 2-per-host default is what pooling here exists to escape")
	assert.GreaterOrEqual(t, tr.MaxIdleConns, tr.MaxIdleConnsPerHost,
		"a global cap below the per-host cap would make the per-host one unreachable")
}

// Transport.Clone deep-copies HTTP2, so cloning DefaultTransport must not be
// picking the config up from somewhere else, and each call must hand back an
// independently-owned config rather than a shared pointer a caller could
// mutate under the live transport.
func TestSharedTransportOwnsItsH2Config(t *testing.T) {
	require.Nil(t, http.DefaultTransport.(*http.Transport).HTTP2,
		"DefaultTransport carries no h2 config; ours is the only source")

	a, b := newSharedTransport(), newSharedTransport()
	require.NotNil(t, a.HTTP2)
	require.NotNil(t, b.HTTP2)
	assert.NotSame(t, a.HTTP2, b.HTTP2)
}

// NewHTTPClient must actually run on the configured transport; a client that
// fell back to http.DefaultTransport would silently lose both the pooling and
// the health check.
func TestNewHTTPClientUsesSharedTransport(t *testing.T) {
	c := NewHTTPClient("https://example.invalid", nil, 0)
	assert.Same(t, sharedTransport, c.hc.Transport)
	assert.Equal(t, 10*time.Second, c.hc.Timeout, "a non-positive timeout falls back to 10s")
}
