// Package projection is the worker's read side of the settings projection.
//
// The pipeline needs three things about the broadcaster an event belongs to:
// the user's tier (the regress status), the enabled modules, and the user's
// custom commands. This package is the single contract for all of them. Each
// lookup follows the same tiers, read-only the whole way down:
//
//  1. in-process cache (theine, short TTL) - the hot path, no I/O;
//  2. Valkey settings:<user_id> hash - the shared projection (read only);
//  3. NATS RPC on a cold key. Modules and commands ask the projector's
//     dashboard get verbs - the projector owns Valkey, so its miss path
//     hydrates the projection and the next read is a Valkey hit. Users ask
//     the users service's projection verb. The worker never writes Valkey;
//     the projector populates it.
//
// Commands are cached per command (key command:<id>:<name>), loaded with a
// single HGET against the projection, so editing one command never forces a
// whole-dictionary reload and the push invalidation can drop exactly the
// entries that changed.
package projection

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// User is the projected tier state of one broadcaster. Live state is NOT here:
// the worker reads it from the dedicated live:<id> store (module.IsLiveChecker),
// never from this projection.
type User struct {
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
	// Locale is the broadcaster's console UI language ("en", "fr", …), used to
	// answer system commands in their language. Empty means the projection has
	// no locale yet; callers treat that as the default language.
	Locale string `json:"locale,omitempty"`
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

// commandEntry is one cached per-command lookup. found distinguishes a real
// "no such command" (cached so repeated unknown "!word" spam costs nothing)
// from a present command.
type commandEntry struct {
	cmd   Command
	found bool
}

// Command is one custom chat command of a user.
type Command struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response,omitempty"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm,omitempty"`
	Cooldown         uint     `json:"cooldown,omitempty"`
	AllowedUserID    string   `json:"allowed_user_id,omitempty"`
}

// Reader is the contract the pipeline depends on. Keeping it an interface lets
// the pipeline be tested against a fake without Valkey or NATS.
type Reader interface {
	User(ctx context.Context, userID uint64) (User, error)
	Modules(ctx context.Context, userID uint64) ([]ModuleView, error)
	Command(ctx context.Context, userID uint64, name string) (Command, bool, error)
}

// Subjects names the RPC each read falls through to on a Valkey miss:
// Modules and Commands are the projector's dashboard get verbs, Users is the
// users service's projection verb.
type Subjects struct {
	Users    string
	Modules  string
	Commands string
}

// Client is the default Reader: in-process cache fronting a read-only Valkey
// view, with a projector RPC fallback on a cold key.
type Client struct {
	store    *Store
	nc       *nats.Conn
	subjects Subjects
	log      *zap.Logger

	users    *cache.Cache[User]
	modules  *cache.Cache[[]ModuleView]
	commands *cache.Cache[commandEntry]

	rpcTimeout      time.Duration
	invalidationSub *nats.Subscription
}

// Config wires a Client. TTL is the in-process cache lifetime; keep it short
// (tens of seconds) so module/command edits propagate quickly while still
// absorbing per-message bursts.
type Config struct {
	Store    *Store
	NC       *nats.Conn
	Subjects Subjects
	TTL      time.Duration
	Log      *zap.Logger
}

func NewClient(cfg Config) *Client {
	return &Client{
		store:      cfg.Store,
		nc:         cfg.NC,
		subjects:   cfg.Subjects,
		log:        cfg.Log,
		users:      cache.New[User](cache.DefaultCapacity, cfg.TTL),
		modules:    cache.New[[]ModuleView](cache.DefaultCapacity, cfg.TTL),
		commands:   cache.New[commandEntry](cache.DefaultCapacity, cfg.TTL),
		rpcTimeout: 1500 * time.Millisecond,
	}
}

// Close releases the in-process caches and any active invalidation subscription.
func (c *Client) Close() {
	if c.invalidationSub != nil {
		_ = c.invalidationSub.Unsubscribe()
	}
	c.users.Close()
	c.modules.Close()
	c.commands.Close()
}

// StartInvalidationListener subscribes to push invalidation messages on
// prefix+".>" (e.g. "bagel.cache.invalidate.>"). When a message arrives the
// scope (last subject token) determines which cache entry to drop immediately,
// backing the short in-process TTL with near-real-time eviction on writes.
//
// The shared invalidate.DTO carries the broadcaster id and, for command-scoped
// events, the granular keys (the command name and its aliases). Commands are
// cached per command, so only those exact entries are evicted; the worker never
// reloads a whole command dictionary on an edit. There is no proactive prewarm:
// the next message for that command reloads it lazily, singleflight-collapsed,
// so editing one command on a 50-pod fleet no longer triggers 50 HGETALLs.
//
// Scope -> cache mapping:
//   - "commands"                    -> per-command entries named in Keys
//   - "modules"                     -> modules cache (whole)
//   - "status" / "grant" / "locale" -> users cache
//   - "delegation"                  -> ignored (worker does not cache delegations)
func (c *Client) StartInvalidationListener(prefix string) {
	subject := prefix + ".>"
	sub, err := c.nc.Subscribe(subject, c.onInvalidation)
	if err != nil {
		c.log.Error("projection: failed to subscribe to cache invalidation", zap.String("subject", subject), zap.Error(err))
		return
	}
	c.invalidationSub = sub
	c.log.Info("projection: cache invalidation listener started", zap.String("subject", subject))
}

// onInvalidation decodes one push-invalidation message and evicts the caches
// its scope (the subject's last token) names.
func (c *Client) onInvalidation(msg *nats.Msg) {
	var payload invalidate.DTO
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		c.log.Debug("projection: cache invalidation: bad payload", zap.Error(err), zap.String("subject", msg.Subject))
		return
	}
	id, err := strconv.ParseUint(payload.BroadcasterID, 10, 64)
	if err != nil || id == 0 {
		c.log.Warn("projection: cache invalidation: bad broadcaster_id", zap.String("raw", payload.BroadcasterID))
		return
	}

	parts := strings.Split(msg.Subject, ".")
	c.evictScope(parts[len(parts)-1], id, payload.Keys)
}

// evictScope drops the cache entries a scope names for one broadcaster.
func (c *Client) evictScope(scope string, id uint64, keys []string) {
	switch scope {
	case "commands":
		// Drop the custom command entry for every key carried by the event.
		for _, name := range keys {
			c.commands.Invalidate(cmdKey(id, strings.ToLower(name)))
		}
	case "modules":
		c.modules.Invalidate(key("modules", id))
	case "status", "grant", "live", "locale":
		// Tier/ban (status/grant), the legacy live field and the UI locale all
		// live on the projected User, so drop it. The dedicated live store keeps
		// its own listener for the live key; this only keeps User coherent.
		c.users.Invalidate(key("user", id))
	case "delegation":
		// Worker does not cache delegations; nothing to evict.
	default:
		c.log.Debug("projection: cache invalidation: unknown scope", zap.String("scope", scope))
	}
}

func (c *Client) User(ctx context.Context, userID uint64) (User, error) {
	return c.users.GetOrLoad(ctx, key("user", userID), func(ctx context.Context) (User, error) {
		status, active, _, locale, err := c.store.GetUser(ctx, userID)
		if err == nil && status != "" {
			return User{Status: status, IsActive: active, Locale: locale}, nil
		}

		reply, err := bus.RequestJSONTimeout[User](ctx, c.nc, c.subjects.Users, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			// Unknown users fall back to standard, never premium, so a
			// projector outage cannot promote traffic.
			return User{Status: "standard"}, nil
		}
		return reply, nil
	})
}

// Modules returns the broadcaster's enabled ModuleView set from the in-process
// cache, filling a cold entry from the Valkey projection (tier 2) or the
// projector RPC (tier 3). A genuine empty answer (projected, no modules) is
// cached like any other; a LOAD FAILURE is not. GetOrLoad only stores the value
// when the loader returns nil, so surfacing the RPC error here keeps a transient
// projector blip from caching an empty set for the whole TTL, which would mask
// automod and every per-channel module toggle until it expired. The error is
// cheap to return: every caller already fails open or nacks on it (the pipeline
// nacks for redelivery, clip/timers fall open per-call), and none of them cache
// it, so each keeps its own policy instead of inheriting a poisoned entry.
func (c *Client) Modules(ctx context.Context, userID uint64) ([]ModuleView, error) {
	return c.modules.GetOrLoad(ctx, key("modules", userID), func(ctx context.Context) ([]ModuleView, error) {
		if mods, projected, err := c.store.GetModules(ctx, userID); err == nil && projected {
			return mods, nil
		}

		reply, err := bus.RequestJSONTimeout[struct {
			Modules []ModuleView `json:"modules"`
		}](ctx, c.nc, c.subjects.Modules, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			return nil, err
		}
		return reply.Modules, nil
	})
}

// Command resolves one custom command by the name (or alias) a viewer typed.
// The hot path is a single per-command cache entry backed by one Valkey HGET;
// only a cold (not-yet-projected) user falls through to the projector RPC,
// which still returns the whole list (rare, per-user-once). Negative results
// are cached too, so unknown "!word" spam never reaches Valkey twice.
func (c *Client) Command(ctx context.Context, userID uint64, name string) (Command, bool, error) {
	if name == "" {
		return Command{}, false, nil
	}
	lname := strings.ToLower(name)

	entry, err := c.commands.GetOrLoad(ctx, cmdKey(userID, lname), func(ctx context.Context) (commandEntry, error) {
		return c.loadCommand(ctx, userID, lname)
	})
	if err != nil {
		return Command{}, false, err
	}
	return entry.cmd, entry.found, nil
}

// loadCommand resolves one command from the Valkey projection (tier 2), falling
// back to the projector RPC's whole list for a cold, not-yet-projected user
// (tier 3). A negative result is a valid cached entry, not an error.
func (c *Client) loadCommand(ctx context.Context, userID uint64, lname string) (commandEntry, error) {
	if view, found, projected, err := c.store.GetCommand(ctx, userID, lname); err == nil && projected {
		if !found {
			return commandEntry{found: false}, nil
		}
		return commandEntry{cmd: commandFromView(view), found: true}, nil
	}

	reply, err := bus.RequestJSONTimeout[struct {
		Commands []Command `json:"commands"`
	}](ctx, c.nc, c.subjects.Commands, projectionRequest(userID), c.rpcTimeout)
	if err != nil {
		return commandEntry{found: false}, nil
	}
	return findCommand(reply.Commands, lname), nil
}

// findCommand picks the command whose name or an alias matches lname (already
// lower-cased), or a negative entry when none match.
func findCommand(commands []Command, lname string) commandEntry {
	for _, cmd := range commands {
		if commandMatches(cmd, lname) {
			return commandEntry{cmd: cmd, found: true}
		}
	}
	return commandEntry{found: false}
}

// commandMatches reports whether cmd is triggered by lname (its name or any
// alias, case-insensitively).
func commandMatches(cmd Command, lname string) bool {
	if strings.ToLower(cmd.Name) == lname {
		return true
	}
	for _, alias := range cmd.Aliases {
		if strings.ToLower(alias) == lname {
			return true
		}
	}
	return false
}

func commandFromView(v CommandView) Command {
	return Command{
		Name:             v.Name,
		Aliases:          v.Aliases,
		Response:         v.Response,
		IsActive:         v.IsActive,
		StreamOnlineOnly: v.StreamOnlineOnly,
		Perm:             v.Perm,
		Cooldown:         v.Cooldown,
		AllowedUserID:    v.AllowedUserID,
	}
}

func projectionRequest(userID uint64) map[string]string {
	return map[string]string{"user_id": strconv.FormatUint(userID, 10)}
}

func key(kind string, userID uint64) string {
	return cache.UserKey(kind+":", userID)
}

func cmdKey(userID uint64, name string) string {
	return cache.PairKey("command:", userID, name)
}
