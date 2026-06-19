package valkey

import (
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
