package valkey

import (
	"context"
	"log"
	"net"
	"os"
	"strings"

	valkey_go "github.com/valkey-io/valkey-go"
)

// localReadPort is the port every Valkey instance binds on its node's host
// (hostPort 6379). Combined with NODE_IP it addresses the node-local instance.
const localReadPort = "6379"

// BuildClientOption constructs the Valkey client option for the master/write
// path. For a Sentinel deployment (detected via the :26379 port) it configures
// Sentinel auth and offloads read-only commands to a replica. This is the
// fallback read path used when no node-local instance is available; the local
// read path is wired separately in NewClient.
//
// Exported for testing.
func BuildClientOption(address, password string) valkey_go.ClientOption {
	return buildOption(address, password, true)
}

// buildOption builds a Valkey client option. replicaReads enables the
// SendToReplicas read-offload for a Sentinel deployment.
//
// It MUST stay off for the pub/sub client: keyspace notifications (the expired
// events the timer + live-recheck watchers subscribe to) are published only by
// the master, so SendToReplicas — which routes the pub/sub SUBSCRIBE to a
// replica — pins the subscription to a node that never emits them, silently
// dropping every expiry. Reads still offload via the node-local client (Do),
// so the pub/sub client losing replica-reads costs nothing.
func buildOption(address, password string, replicaReads bool) valkey_go.ClientOption {
	opts := valkey_go.ClientOption{
		InitAddress:  []string{address},
		Password:     password,
		DisableCache: true,
	}

	if strings.HasSuffix(address, ":26379") {
		opts.Sentinel = valkey_go.SentinelOption{
			MasterSet: "myprimary",
			Password:  password,
		}
		if replicaReads {
			// Without a node-local read client, fall back to sending read-only
			// commands to a Sentinel replica. Writes still go to the master.
			opts.SendToReplicas = func(cmd valkey_go.Completed) bool {
				return cmd.IsReadOnly()
			}
		}
	}
	return opts
}

// Client routes writes to the Sentinel-elected master and read-only commands
// to the node-local Valkey instance.
//
// Topology: each Kubernetes node runs one Valkey pod that binds 6379 on the
// host (hostPort). Whatever role that local instance holds (master on the
// primary node, a replica everywhere else) it is always the lowest-latency
// instance for pods on that node. So:
//
//   - writes and topology  -> the embedded Sentinel client, which always tracks
//     the current master and fails over automatically;
//   - pub/sub (Receive)     -> a dedicated master-pinned Sentinel client with no
//     replica-read offload, so expiry notifications (master-only) are actually
//     received;
//   - read-only commands    -> a direct connection to NODE_IP:6379, the local
//     instance, with no cross-node hop.
//
// valkey-go's Sentinel client cannot prefer a local replica on its own:
// ReadNodeSelector is cluster-mode only, and the Sentinel path picks a replica
// at random. Splitting the read path out is what makes node-local reads work.
type Client struct {
	valkey_go.Client                  // master/write path plus everything not overridden
	local            valkey_go.Client // node-local read path; nil when unavailable
	pubsub           valkey_go.Client // master-pinned pub/sub path; nil for standalone
}

// Do sends read-only commands to the local instance and everything else to the
// master via Sentinel.
func (c *Client) Do(ctx context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	if c.local != nil && cmd.IsReadOnly() {
		return c.local.Do(ctx, cmd)
	}
	return c.Client.Do(ctx, cmd)
}

// DoMulti sends a batch to the local instance only when every command is
// read-only; any write in the batch routes the whole batch to the master.
func (c *Client) DoMulti(ctx context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	if c.local != nil && allReadOnly(multi) {
		return c.local.DoMulti(ctx, multi...)
	}
	return c.Client.DoMulti(ctx, multi...)
}

// Receive routes pub/sub to the master-pinned client. Keyspace notifications
// (the expired events the timer + live-recheck watchers subscribe to) are
// emitted only by the master; the default client's SendToReplicas read-offload
// would otherwise pin the subscription to a replica that never delivers them.
func (c *Client) Receive(ctx context.Context, subscribe valkey_go.Completed, fn func(msg valkey_go.PubSubMessage)) error {
	if c.pubsub != nil {
		return c.pubsub.Receive(ctx, subscribe, fn)
	}
	return c.Client.Receive(ctx, subscribe, fn)
}

// Close releases the master/write client, the local read client and the
// master-pinned pub/sub client.
func (c *Client) Close() {
	if c.local != nil {
		c.local.Close()
	}
	if c.pubsub != nil {
		c.pubsub.Close()
	}
	c.Client.Close()
}

func allReadOnly(multi []valkey_go.Completed) bool {
	for i := range multi {
		if !multi[i].IsReadOnly() {
			return false
		}
	}
	return true
}

// NewClient initializes a Valkey client.
//
// Writes always go to the Sentinel-elected master. For a Sentinel deployment
// with NODE_IP set, read-only commands go to the node-local instance
// (NODE_IP:6379) so each node reads from its own Valkey without a cross-node
// hop. Otherwise reads fall back to a Sentinel replica (or the single
// instance, for a standalone address).
func NewClient(address, password string) (valkey_go.Client, error) {
	master, err := valkey_go.NewClient(BuildClientOption(address, password))
	if err != nil {
		return nil, err
	}

	// Standalone (dev / single instance): that one node is the master, so its
	// own Do/Receive already land on the right place. No wrapper needed.
	if !strings.HasSuffix(address, ":26379") {
		return master, nil
	}

	// Sentinel: pub/sub must be pinned to the master (no SendToReplicas) or the
	// expiry watchers subscribe to a replica that never emits expired events.
	pubsub, err := valkey_go.NewClient(buildOption(address, password, false))
	if err != nil {
		master.Close()
		return nil, err
	}
	wrapped := &Client{Client: master, pubsub: pubsub}

	// Node-local read path: a Sentinel deployment where every node hosts a
	// Valkey instance on NODE_IP:6379.
	nodeIP := os.Getenv("NODE_IP")
	if nodeIP == "" {
		return wrapped, nil
	}

	local, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:  []string{net.JoinHostPort(nodeIP, localReadPort)},
		Password:     password,
		DisableCache: true,
	})
	if err != nil {
		// Local instance unreachable at startup: degrade to Sentinel-only
		// rather than fail the service. Reads go to a Sentinel replica;
		// correctness is unaffected, only locality is lost.
		log.Printf("valkey: node-local read client unavailable (%v); reading via Sentinel", err)
		return wrapped, nil
	}

	log.Printf("valkey: reading from node-local instance %s:%s", nodeIP, localReadPort)
	wrapped.local = local
	return wrapped, nil
}
