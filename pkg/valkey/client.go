package valkey

import (
	"log"
	"os"
	"strings"

	valkey_go "github.com/valkey-io/valkey-go"
)

// BuildClientOption constructs the Valkey client option based on the address.
// This is exported for testing purposes.
func BuildClientOption(address, password string) valkey_go.ClientOption {
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
		// Route all read-only commands (GET, HGETALL) to replicas!
		opts.SendToReplicas = func(cmd valkey_go.Completed) bool {
			return cmd.IsReadOnly()
		}

		// Prioritize local replica by matching the node IP
		if nodeIP := os.Getenv("NODE_IP"); nodeIP != "" {
			fallback := valkey_go.PreferReplicaNodeSelector()
			opts.ReadNodeSelector = func(slot uint16, nodes []valkey_go.NodeInfo) int {
				for i, r := range nodes {
					if strings.HasPrefix(r.Addr, nodeIP+":") {
						log.Printf("PreferLocal: selected local node %s for NODE_IP %s", r.Addr, nodeIP)
						return i
					}
				}
				// fallback: use built-in replica load balancer
				idx := fallback(slot, nodes)
				if idx != -1 {
					log.Printf("PreferLocal: fallback to node %s (NODE_IP %s)", nodes[idx].Addr, nodeIP)
					return idx
				} else if len(nodes) > 0 {
					log.Printf("PreferLocal: fallback to primary node %s (NODE_IP %s)", nodes[0].Addr, nodeIP)
					return 0
				}
				return -1 // should not happen
			}
		}
	}
	return opts
}

// NewClient initializes a Valkey client using standard configuration.
// If connecting to a Sentinel cluster (detected via :26379 port),
// it configures Sentinel auth and automatically routes reads to replicas.
func NewClient(address, password string) (valkey_go.Client, error) {
	opts := BuildClientOption(address, password)
	return valkey_go.NewClient(opts)
}
