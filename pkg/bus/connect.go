package bus

import (
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/pkg/env"
)

// options are shared by every connection the fleet opens, core or JetStream:
// endless reconnects, a client name for server-side monitoring, and
// user/password credentials when the environment provides them. Local
// development runs against an open server, so missing credentials are fine;
// the broker is the one enforcing them.
func options(name string) []nats.Option {

	opts := []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectBufSize(8 * 1024 * 1024), // 8 MB buffer so publishes during a reconnect are not lost
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.Timeout(15 * time.Second),
		nats.RetryOnFailedConnect(true),
	}

	if name != "" {
		opts = append(opts, nats.Name(name))
	}

	if user := env.Get("NATS_USER", ""); user != "" {
		opts = append(opts, nats.UserInfo(user, env.Get("NATS_PASSWORD", "")))
	}

	return opts
}

// Connect opens a core NATS connection for request-reply RPC and ephemeral
// subscriptions, carrying the same credentials as the JetStream publisher
// and subscriber. name identifies the service in NATS monitoring.
func Connect(url string, name string) (*nats.Conn, error) {
	return nats.Connect(url, options(name)...)
}

// RPCURL returns the core NATS endpoint used for request/reply traffic. It
// intentionally falls back to the durable bus URL so local development and old
// deployments keep working, while production can point RPC at a node-local leaf
// without moving JetStream publisher/subscriber traffic.
func RPCURL(busURL string) string {
	return env.Get("NATS_RPC_URL", busURL)
}
