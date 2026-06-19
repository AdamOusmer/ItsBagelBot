// Package projection is the worker's read side of the settings projection.
//
// The pipeline needs three things about the broadcaster an event belongs to:
// the user's tier (the regress status), the enabled modules, and the user's
// custom commands. This package is the single contract for all three. Each
// lookup follows the same tiers, read-only the whole way down:
//
//  1. in-process cache (theine, short TTL) - the hot path, no I/O;
//  2. Valkey settings:<user_id> hash - the shared projection (read only);
//  3. NATS RPC to the projector - the authority that owns Valkey, asked on a
//     cold key. The worker never writes Valkey; the projector populates it.
//
// User and module state live in Valkey. Commands are not projected there, so
// they skip tier 2 and resolve cache -> RPC.
package projection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// User is the projected tier state of one broadcaster.
type User struct {
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
	IsLive   bool   `json:"is_live"`
}

// Premium reports whether the user should be served on the premium lane. It
// mirrors the projector's tier rule so the worker's regress status agrees with
// what ingress laned the event on.
func (u User) Premium() bool {
	if !u.IsActive {
		return false
	}
	switch u.Status {
	case "premium", "vip", "paid":
		return true
	default:
		return false
	}
}

// Module is one enabled/disabled feature toggle of a user, with its raw config.
type Module struct {
	Name      string          `json:"name"`
	IsEnabled bool            `json:"is_enabled"`
	Configs   json.RawMessage `json:"configs,omitempty"`
}

// Command is one custom chat command of a user.
type Command struct {
	Name             string `json:"name"`
	Response         string `json:"response,omitempty"`
	IsActive         bool   `json:"is_active"`
	StreamOnlineOnly bool   `json:"stream_online_only"`
	Perm             string `json:"perm,omitempty"`
	Cooldown         uint   `json:"cooldown,omitempty"`
	AllowedUserID    uint64 `json:"allowed_user_id,omitempty"`
}

// Reader is the contract the pipeline depends on. Keeping it an interface lets
// the pipeline be tested against a fake without Valkey or NATS.
type Reader interface {
	User(ctx context.Context, userID uint64) (User, error)
	Modules(ctx context.Context, userID uint64) (map[string]Module, error)
	Command(ctx context.Context, userID uint64, name string) (Command, bool, error)
}

// Subjects names the projector RPC each read falls through to on a Valkey miss.
type Subjects struct {
	Users    string
	Modules  string
	Commands string
}

// Client is the default Reader: in-process cache fronting a read-only Valkey
// view, with a projector RPC fallback on a cold key.
type Client struct {
	store    *Valkey
	nc       *nats.Conn
	subjects Subjects
	log      *zap.Logger

	users    *cache.Cache[User]
	modules  *cache.Cache[map[string]Module]
	commands *cache.Cache[map[string]Command]

	rpcTimeout time.Duration
}

// NewClient wires a Client. ttl is the in-process cache lifetime; keep it
// short (tens of seconds) so module/command edits propagate quickly while
// still absorbing per-message bursts.
func NewClient(store *Valkey, nc *nats.Conn, subjects Subjects, ttl time.Duration, log *zap.Logger) *Client {
	return &Client{
		store:      store,
		nc:         nc,
		subjects:   subjects,
		log:        log,
		users:      cache.New[User](cache.DefaultCapacity, ttl),
		modules:    cache.New[map[string]Module](cache.DefaultCapacity, ttl),
		commands:   cache.New[map[string]Command](cache.DefaultCapacity, ttl),
		rpcTimeout: 1500 * time.Millisecond,
	}
}

// Close releases the in-process caches.
func (c *Client) Close() {
	c.users.Close()
	c.modules.Close()
	c.commands.Close()
}

func (c *Client) User(ctx context.Context, userID uint64) (User, error) {
	return c.users.GetOrLoad(ctx, key("user", userID), func(ctx context.Context) (User, error) {
		status, active, live, err := c.store.GetUser(ctx, userID)
		if err == nil && status != "" {
			return User{Status: status, IsActive: active, IsLive: live}, nil
		}

		reply, err := bus.RequestJSONTimeout[User](ctx, c.nc, c.subjects.Users, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			// Unknown users fall back to standard, never premium, so a
			// projector outage cannot promote traffic.
			return User{Status: "standard", IsLive: live}, nil
		}
		reply.IsLive = live
		return reply, nil
	})
}

func (c *Client) Modules(ctx context.Context, userID uint64) (map[string]Module, error) {
	return c.modules.GetOrLoad(ctx, key("modules", userID), func(ctx context.Context) (map[string]Module, error) {
		if mods, err := c.store.GetModules(ctx, userID); err == nil && len(mods) > 0 {
			return mods, nil
		}

		reply, err := bus.RequestJSONTimeout[struct {
			Modules []Module `json:"modules"`
		}](ctx, c.nc, c.subjects.Modules, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			return map[string]Module{}, nil
		}
		out := make(map[string]Module, len(reply.Modules))
		for _, m := range reply.Modules {
			out[m.Name] = m
		}
		return out, nil
	})
}

func (c *Client) Command(ctx context.Context, userID uint64, name string) (Command, bool, error) {
	cmds, err := c.commands.GetOrLoad(ctx, key("commands", userID), func(ctx context.Context) (map[string]Command, error) {
		// Commands are not projected into Valkey; the projector answers from
		// the commands service over RPC.
		reply, err := bus.RequestJSONTimeout[struct {
			Commands []Command `json:"commands"`
		}](ctx, c.nc, c.subjects.Commands, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			return map[string]Command{}, nil
		}
		out := make(map[string]Command, len(reply.Commands))
		for _, cmd := range reply.Commands {
			out[cmd.Name] = cmd
		}
		return out, nil
	})
	if err != nil {
		return Command{}, false, err
	}
	cmd, ok := cmds[name]
	return cmd, ok, nil
}

func projectionRequest(userID uint64) map[string]string {
	return map[string]string{"user_id": fmt.Sprint(userID)}
}

func key(kind string, userID uint64) string {
	return kind + ":" + fmt.Sprint(userID)
}
