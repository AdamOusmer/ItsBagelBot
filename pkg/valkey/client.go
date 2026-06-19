package valkey

import (
	"strings"

	valkey_go "github.com/valkey-io/valkey-go"
)

// NewClient creates a new Valkey client, handling Sentinel routing and read/write splitting automatically.
func NewClient(address string, password string) (valkey_go.Client, error) {
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

	return valkey_go.NewClient(opts)
}
