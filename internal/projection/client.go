// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	usersCacheCapacity    int64 = 4096
	modulesCacheCapacity  int64 = 4096
	commandsCacheCapacity int64 = 8192
	fetchesCacheCapacity  int64 = 8192

	moduleOutageBackoff = 2 * time.Second
)

type User struct {
	VoicePlan          string           `json:"voice_plan,omitempty"`
	AccountCreatedAt   int64            `json:"account_created_at,omitempty"`
	Lifecycle          *lifecycle.State `json:"lifecycle,omitempty"`
	Banned             bool             `json:"banned"`
	Status             string           `json:"status"`
	IsActive           bool             `json:"is_active"`
	Locale             string           `json:"locale,omitempty"`
	CommandsPageHidden bool             `json:"commands_page_hidden,omitempty"`

	LeaderboardPageHidden bool   `json:"leaderboard_page_hidden,omitempty"`
	SongQueuePageHidden   bool   `json:"songqueue_page_hidden,omitempty"`
	CreatorCode           string `json:"creator_code,omitempty"`
	Alpha                 bool   `json:"alpha,omitempty"`
}

func (u User) IsAlpha() bool { return u.Alpha }

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

type commandEntry struct {
	cmd   Command
	found bool
}

type fetchEntry struct {
	fetch FetchView
	found bool
}

type modulesReply struct {
	Modules []ModuleView `json:"modules"`
}

type commandsReply struct {
	Commands []Command `json:"commands"`
}

type fetchesReply struct {
	Fetches []FetchView `json:"fetches"`
}

type Command struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response,omitempty"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm,omitempty"`
	Cooldown         uint     `json:"cooldown,omitempty"`
	UserCooldown     uint     `json:"user_cooldown,omitempty"`
	AllowedUserID    string   `json:"allowed_user_id,omitempty"`
	Uses             int64    `json:"uses,omitempty,string"`
	BumpCounter      string   `json:"bump_counter,omitempty"`
}

type Reader interface {
	User(ctx context.Context, userID uint64) (User, error)
	Modules(ctx context.Context, userID uint64) (map[string]ModuleView, error)
	Module(ctx context.Context, userID uint64, name string) (ModuleView, bool, error)
	Command(ctx context.Context, userID uint64, name string) (Command, bool, error)
}

type Subjects struct {
	Users       string
	Modules     string
	Commands    string
	Fetches     string
	Automations string
}

type Client struct {
	store    *Store
	nc       *nats.Conn
	subjects Subjects
	log      *zap.Logger

	users         *cache.Cache[User]
	modules       *cache.Cache[map[string]ModuleView]
	moduleOutages *cache.Cache[error]
	commands      *cache.Cache[commandEntry]
	fetches       *cache.Cache[fetchEntry]

	automations *automationLists

	commandLookup perName[CommandView, commandEntry]
	fetchLookup   perName[FetchView, fetchEntry]

	rpcTimeout time.Duration
}

type Config struct {
	Store         *Store
	NC            *nats.Conn
	Subjects      Subjects
	TTL           time.Duration
	Log           *zap.Logger
	Invalidations cache.Feed
}

func NewClient(cfg Config) *Client {
	c := &Client{
		store:      cfg.Store,
		nc:         cfg.NC,
		subjects:   cfg.Subjects,
		log:        cfg.Log,
		users:      cache.New[User](usersCacheCapacity, cfg.TTL, cache.StaleOnError(cfg.TTL)).InvalidateOn(cfg.Invalidations, cache.UserKeys("user:"), invalidate.ScopeUserProjection, invalidate.ScopeLive),
		modules:    cache.New[map[string]ModuleView](modulesCacheCapacity, cfg.TTL, cache.StaleOnError(cfg.TTL)).InvalidateOn(cfg.Invalidations, cache.UserKeys("modules:"), invalidate.ScopeModules),
		commands:   bindNamedEntries(cache.New[commandEntry](commandsCacheCapacity, cfg.TTL, cache.StaleOnError(cfg.TTL)), cfg.Invalidations, invalidate.ScopeCommands, "command:"),
		fetches:    bindNamedEntries(cache.New[fetchEntry](fetchesCacheCapacity, cfg.TTL, cache.StaleOnError(cfg.TTL)), cfg.Invalidations, invalidate.ScopeFetches, "fetch:"),
		rpcTimeout: 1500 * time.Millisecond,

		moduleOutages: cache.New[error](modulesCacheCapacity, moduleOutageBackoff).InvalidateOn(cfg.Invalidations, cache.UserKeys("modules:"), invalidate.ScopeModules),
	}
	c.automations = newAutomationCache(cfg.TTL).InvalidateOn(cfg.Invalidations, cache.UserKeys("automations:"), invalidate.ScopeAutomations)
	c.commandLookup = perName[CommandView, commandEntry]{
		entries: c.commands,
		key:     cmdKey,
		local:   c.store.GetCommand,
		entry:   commandEntryOf,
		remote:  c.commandsRPC,
	}
	c.fetchLookup = perName[FetchView, fetchEntry]{
		entries: c.fetches,
		key:     fetchKey,
		local:   c.store.GetFetch,
		entry:   fetchEntryOf,
		remote:  c.fetchesRPC,
	}
	return c
}

func (c *Client) Close() {
	c.users.Close()
	c.modules.Close()
	c.moduleOutages.Close()
	c.commands.Close()
	c.fetches.Close()
	c.automations.Close()
}

func (c *Client) StartOccupancyLogger(ctx context.Context, interval time.Duration) {
	cache.StartOccupancyLogger(ctx, c.log, interval, map[string]cache.OccupancySource{
		"projection_users":    c.users,
		"projection_modules":  c.modules,
		"projection_commands": c.commands,
		"projection_fetches":  c.fetches,
	})
}

func bindNamedEntries[V any](entries *cache.Cache[V], feed cache.Feed, scope, prefix string) *cache.Cache[V] {
	if feed == nil {
		return entries
	}
	feed.Watch(scope, func(e cache.Event) {
		userPrefix := cache.UserKey(prefix, e.ID) + ":"
		if len(e.Keys) == 0 {
			entries.InvalidateWhere(func(key string) bool { return strings.HasPrefix(key, userPrefix) })
			return
		}
		for _, name := range e.Keys {
			entries.Invalidate(userPrefix + strings.ToLower(name))
		}
	})
	feed.WatchGap(entries.Clear)
	return entries
}

func (c *Client) User(ctx context.Context, userID uint64) (User, error) {
	return c.users.GetOrLoad(ctx, key("user", userID), func(ctx context.Context) (User, error) {
		user, found, err := c.store.GetUserRuntime(ctx, userID)
		if err == nil && found {
			loadSource(ctx, "projection.user.source", "valkey")
			return user, nil
		}

		reply, err := bus.RequestJSONTimeout[User](ctx, c.nc, c.subjects.Users, projectionRequest(userID), c.rpcTimeout)
		if bus.NotFoundReply(err) {
			loadSource(ctx, "projection.user.source", "standard_fallback")
			return User{Status: "standard"}, nil
		}
		if err != nil {
			loadSource(ctx, "projection.user.source", "rpc_error")
			return User{}, err
		}
		loadSource(ctx, "projection.user.source", "rpc")
		return reply, nil
	})
}

// The returned map is the cached one; callers must not mutate it.
func (c *Client) Modules(ctx context.Context, userID uint64) (map[string]ModuleView, error) {
	return c.modules.GetOrLoad(ctx, key("modules", userID), func(ctx context.Context) (map[string]ModuleView, error) {
		if mods, projected, err := c.store.GetModules(ctx, userID); err == nil && projected {
			loadSource(ctx, "projection.modules.source", "valkey")
			return mods, nil
		}

		outageKey := key("modules", userID)
		if outage, down := c.moduleOutages.Get(outageKey); down {
			loadSource(ctx, "projection.modules.source", "rpc_backoff")
			return nil, outage
		}

		reply, err := bus.RequestJSONTimeout[modulesReply](ctx, c.nc, c.subjects.Modules, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			loadSource(ctx, "projection.modules.source", "rpc_error")
			if ctx.Err() == nil {
				c.moduleOutages.Set(outageKey, err)
			}
			// An empty map here would be cached for the TTL and silently disable automod.
			return nil, err
		}
		loadSource(ctx, "projection.modules.source", "rpc")
		return ModuleMap(reply.Modules), nil
	})
}

func (c *Client) Module(ctx context.Context, userID uint64, name string) (ModuleView, bool, error) {
	views, err := c.Modules(ctx, userID)
	if err != nil {
		return ModuleView{}, false, err
	}
	view, ok := views[name]
	return view, ok, nil
}

type lookupName string

type cacheKind string

type perName[V, E any] struct {
	entries *cache.Cache[E]
	key     func(uint64, lookupName) string
	local   func(context.Context, uint64, string) (V, bool, bool, error)
	entry   func(V, bool) E
	remote  func(context.Context, uint64, lookupName) (E, error)
}

func (l perName[V, E]) get(ctx context.Context, userID uint64, name string) (E, error) {
	var zero E
	if name == "" {
		return zero, nil
	}
	lname := lookupName(strings.ToLower(name))
	return l.entries.GetOrLoad(ctx, l.key(userID, lname), func(ctx context.Context) (E, error) {
		view, found, projected, err := l.local(ctx, userID, string(lname))
		if err != nil {
			return l.remote(ctx, userID, lname)
		}
		if found || projected {
			return l.entry(view, found), nil
		}
		return l.remote(ctx, userID, lname)
	})
}

func (c *Client) Command(ctx context.Context, userID uint64, name string) (Command, bool, error) {
	entry, err := c.commandLookup.get(ctx, userID, name)
	if err != nil {
		return Command{}, false, err
	}
	return entry.cmd, entry.found, nil
}

func commandEntryOf(view CommandView, found bool) commandEntry {
	if !found {
		return commandEntry{found: false}
	}
	return commandEntry{cmd: commandFromView(view), found: true}
}

func (c *Client) commandsRPC(ctx context.Context, userID uint64, lname lookupName) (commandEntry, error) {
	reply, err := bus.RequestJSONTimeout[commandsReply](ctx, c.nc, c.subjects.Commands, projectionRequest(userID), c.rpcTimeout)
	if err != nil {
		return commandEntry{}, err
	}
	return findCommand(reply.Commands, lname), nil
}

func findCommand(commands []Command, lname lookupName) commandEntry {
	for _, cmd := range commands {
		if commandMatches(cmd, lname) {
			return commandEntry{cmd: cmd, found: true}
		}
	}
	return commandEntry{found: false}
}

func commandMatches(cmd Command, lname lookupName) bool {
	if strings.ToLower(cmd.Name) == string(lname) {
		return true
	}
	for _, alias := range cmd.Aliases {
		if strings.ToLower(alias) == string(lname) {
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
		UserCooldown:     v.UserCooldown,
		AllowedUserID:    v.AllowedUserID,
		Uses:             v.Uses,
		BumpCounter:      v.BumpCounter,
	}
}

func projectionRequest(userID uint64) map[string]string {
	return map[string]string{"user_id": strconv.FormatUint(userID, 10)}
}

func (c *Client) FetchDefs(ctx context.Context, userID uint64, name string) (FetchView, bool, error) {
	entry, err := c.fetchLookup.get(ctx, userID, name)
	if err != nil {
		return FetchView{}, false, err
	}
	return entry.fetch, entry.found, nil
}

func fetchEntryOf(view FetchView, found bool) fetchEntry {
	return fetchEntry{fetch: view, found: found}
}

func (c *Client) fetchesRPC(ctx context.Context, userID uint64, lname lookupName) (fetchEntry, error) {
	reply, err := bus.RequestJSONTimeout[fetchesReply](ctx, c.nc, c.subjects.Fetches, projectionRequest(userID), c.rpcTimeout)
	if err != nil {
		return fetchEntry{}, err
	}
	return findFetch(reply.Fetches, lname), nil
}

func findFetch(fetches []FetchView, lname lookupName) fetchEntry {
	for _, f := range fetches {
		if strings.ToLower(f.Name) == string(lname) {
			return fetchEntry{fetch: f, found: true}
		}
	}
	return fetchEntry{found: false}
}

func key(kind cacheKind, userID uint64) string {
	return cache.UserKey(string(kind)+":", userID)
}

func cmdKey(userID uint64, name lookupName) string {
	return cache.PairKey("command:", userID, string(name))
}

func fetchKey(userID uint64, name lookupName) string {
	return cache.PairKey("fetch:", userID, string(name))
}
