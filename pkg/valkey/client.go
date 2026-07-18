package valkey

import (
	"crypto/tls"
	"log"
	"net"
	"os"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const (
	// Writes always cross the network to the elected primary. A bounded
	// dedicated pool avoids large automatic pipeline batches under bursts.
	// Production-path tests at concurrency 50 improved write p95, p99 and
	// throughput; smaller pools produced long acquisition tails. Small buffers
	// keep the maximum per-process connection footprint predictable.
	writePoolSize       = 64
	writePoolMinSize    = 4
	writePoolBufferSize = 32 << 10
	writePoolIdleTime   = 30 * time.Second

	// Local reads stay auto-pipelined, but spread over 32 small-buffer
	// connections. Production-path A/B tests improved throughput without the
	// memory cost of valkey-go's default 512 KiB buffers per direction.
	localPipelineMultiplex = 5
	localBufferSize        = 8 << 10
)

// BuildClientOption constructs the Valkey client option for the primary path.
// For a Sentinel deployment (detected via the plaintext or TLS Sentinel port)
// it configures
// Sentinel auth. Read-only commands stay primary-consistent unless Client
// explicitly routes them to its node-local client.
//
// Exported for testing.
func BuildClientOption(address, password string) valkey_go.ClientOption {
	return withWritePool(buildOption(address, password, nil))
}

func withWritePool(opts valkey_go.ClientOption) valkey_go.ClientOption {
	opts.DisableAutoPipelining = true
	opts.BlockingPoolSize = writePoolSize
	opts.BlockingPoolCleanup = writePoolIdleTime
	opts.BlockingPoolMinSize = writePoolMinSize
	opts.ReadBufferEachConn = writePoolBufferSize
	opts.WriteBufferEachConn = writePoolBufferSize
	return opts
}

// buildOption builds a primary-consistent Valkey client option. Replica reads
// are deliberately not enabled here: Client owns the node-local read route,
// while this connection remains the authoritative primary route used by
// writes, Primary views and keyspace-notification subscriptions.
func buildOption(address, password string, tlsConfig *tls.Config) valkey_go.ClientOption {
	opts := valkey_go.ClientOption{
		InitAddress:  []string{address},
		Password:     password,
		DisableCache: true,
		TLSConfig:    cloneTLSConfig(tlsConfig),
	}
	if tlsConfig != nil {
		// During the guarded rollout older Sentinels can briefly report 6379.
		// Rewrite that discovered endpoint to the native TLS listener before
		// dialing; current 6380/26380 addresses pass through unchanged.
		opts.DialCtxFn = nativeTLSDial
	}

	if isSentinelAddress(address) {
		opts.Sentinel = valkey_go.SentinelOption{
			MasterSet: "myprimary",
			Password:  password,
			TLSConfig: cloneTLSConfig(tlsConfig),
		}
	}
	return opts
}

// Client routes writes to the Sentinel-elected master and read-only commands
// to the node-local Valkey instance.
//
// Topology: each Kubernetes node runs one Valkey pod. The valkey-local Service
// has internalTrafficPolicy=Local, so kube-proxy selects that node's endpoint
// without a Tailnet or hostPort loop. Whatever role that local instance holds
// (master on the primary node, a replica elsewhere) it is the lowest-latency
// instance for pods on that node. So:
//
//   - writes, topology and primary-consistent reads -> the embedded Sentinel
//     client, which always tracks the current master and fails over
//     automatically;
//   - pub/sub (Receive)     -> a dedicated master-pinned Sentinel client with no
//     replica-read offload, so expiry notifications (master-only) are actually
//     received;
//   - read-only commands    -> valkey-local over TLS, with no cross-node hop.
//
// valkey-go's Sentinel client cannot prefer a local replica on its own:
// ReadNodeSelector is cluster-mode only. Splitting the read path out is what
// makes node-local reads deterministic while keeping a no-extra-pool primary
// route available for read-after-write consistency.
type Client struct {
	valkey_go.Client                   // master/write path plus everything not overridden
	local            valkey_go.Client  // node-local read path; nil when unavailable
	pubsub           *lazyValkeyClient // master-pinned pub/sub path; nil for standalone
}

// NewClient initializes a Valkey client.
//
// Writes always go to the Sentinel-elected master. For a Sentinel deployment
// with NODE_IP set, read-only commands go to VALKEY_LOCAL_ADDR when configured,
// otherwise to NODE_IP. The production Service uses internalTrafficPolicy=Local
// so each node reads its own Valkey without a cross-node hop. Without a local
// endpoint, reads stay on the primary for correctness instead of choosing a
// random remote replica.
func NewClient(address, password string) (valkey_go.Client, error) {
	tlsConfig, err := clientTLSConfig()
	if err != nil {
		return nil, err
	}
	address = secureAddress(address, tlsConfig != nil)
	master, err := valkey_go.NewClient(withWritePool(buildOption(address, password, tlsConfig)))
	if err != nil {
		return nil, err
	}

	// Standalone (dev / single instance): that one node is the master, so its
	// own Do/Receive already land on the right place. Keep the lightweight
	// wrapper so sampled dependency telemetry behaves the same in every mode.
	if !isSentinelAddress(address) {
		return &Client{Client: master}, nil
	}

	// Sentinel pub/sub is isolated on its own master-pinned client so long-lived
	// expiry watchers do not share the ordinary command path. Construct it lazily
	// because most services never call Receive and need no additional pool.
	wrapped := &Client{
		Client: master,
		pubsub: newLazyValkeyClient(
			buildOption(address, password, tlsConfig),
			valkey_go.NewClient,
		),
	}

	// Node-local read path: a Sentinel deployment where every node hosts a
	// Valkey instance on NODE_IP:6380 in production.
	nodeIP := os.Getenv("NODE_IP")
	if nodeIP == "" {
		return wrapped, nil
	}

	localAddress := (localEndpoint{
		nodeIP:     nodeIP,
		configured: os.Getenv("VALKEY_LOCAL_ADDR"),
		tlsEnabled: tlsConfig != nil,
	}).address()
	local, err := valkey_go.NewClient((localReadConfig{
		address:   localAddress,
		password:  password,
		tlsConfig: tlsConfig,
	}).option())
	if err != nil {
		// Local instance unreachable at startup: degrade to Sentinel-only
		// rather than fail the service. Reads stay primary-consistent;
		// correctness is unaffected, only locality is lost.
		log.Printf("valkey: node-local read client unavailable (%v); reading via Sentinel", err)
		return wrapped, nil
	}

	log.Printf("valkey: reading from node-local instance %s", localAddress)
	wrapped.local = local
	return wrapped, nil
}

type localReadConfig struct {
	address   string
	password  string
	tlsConfig *tls.Config
}

func (c localReadConfig) option() valkey_go.ClientOption {
	return valkey_go.ClientOption{
		InitAddress:         []string{c.address},
		Password:            c.password,
		DisableCache:        true,
		TLSConfig:           cloneTLSConfig(c.tlsConfig),
		PipelineMultiplex:   localPipelineMultiplex,
		ReadBufferEachConn:  localBufferSize,
		WriteBufferEachConn: localBufferSize,
	}
}

type localEndpoint struct {
	nodeIP     string
	configured string
	tlsEnabled bool
}

func (e localEndpoint) address() string {
	if e.configured != "" {
		return secureAddress(e.configured, e.tlsEnabled)
	}
	port := plainDataPort
	if e.tlsEnabled {
		port = tlsDataPort
	}
	return net.JoinHostPort(e.nodeIP, port)
}
