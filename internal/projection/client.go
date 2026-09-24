// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	usersCacheCapacity    int64 = 4096
	modulesCacheCapacity  int64 = 4096
	commandsCacheCapacity int64 = 8192
	fetchesCacheCapacity  int64 = 8192
)

type User struct {
	Status             string `json:"status"`
	IsActive           bool   `json:"is_active"`
	Locale             string `json:"locale,omitempty"`
	CommandsPageHidden bool   `json:"commands_page_hidden,omitempty"`
}

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

type Command struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response,omitempty"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm,omitempty"`
	Cooldown         uint     `json:"cooldown,omitempty"`
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
	Users    string
	Modules  string
	Commands string
	Fetches  string
}

type Client struct {
	store    *Store
	nc       *nats.Conn
	subjects Subjects
	log      *zap.Logger

	users    *cache.Cache[User]
	modules  *cache.Cache[map[string]ModuleView]
	commands *cache.Cache[commandEntry]
	fetches  *cache.Cache[fetchEntry]

	commandLookup perName[CommandView, commandEntry]
	fetchLookup   perName[FetchView, fetchEntry]

	rpcTimeout      time.Duration
	invalidationSub *nats.Subscription
}

type Config struct {
	Store    *Store
	NC       *nats.Conn
	Subjects Subjects
	TTL      time.Duration
	Log      *zap.Logger
}

func NewClient(cfg Config) *Client {
	c := &Client{
		store:      cfg.Store,
		nc:         cfg.NC,
		subjects:   cfg.Subjects,
		log:        cfg.Log,
		users:      cache.New[User](usersCacheCapacity, cfg.TTL),
		modules:    cache.New[map[string]ModuleView](modulesCacheCapacity, cfg.TTL),
		commands:   cache.New[commandEntry](commandsCacheCapacity, cfg.TTL),
		fetches:    cache.New[fetchEntry](fetchesCacheCapacity, cfg.TTL),
		rpcTimeout: 1500 * time.Millisecond,
	}
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
	if c.invalidationSub != nil {
		_ = c.invalidationSub.Unsubscribe()
	}
	c.users.Close()
	c.modules.Close()
	c.commands.Close()
	c.fetches.Close()
}

func (c *Client) StartOccupancyLogger(ctx context.Context, interval time.Duration) {
	cache.StartOccupancyLogger(ctx, c.log, interval, map[string]cache.OccupancySource{
		"projection_users":    c.users,
		"projection_modules":  c.modules,
		"projection_commands": c.commands,
		"projection_fetches":  c.fetches,
	})
}

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

func (c *Client) onInvalidation(msg *nats.Msg) {
	var payload invalidate.DTO
	if err := codec.Unmarshal(msg.Data, &payload); err != nil {
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

func (c *Client) evictScope(scope string, id uint64, keys []string) {
	switch scope {
	case "commands":
		for _, name := range keys {
			c.commands.Invalidate(cmdKey(id, strings.ToLower(name)))
		}
	case "fetches":
		for _, name := range keys {
			c.fetches.Invalidate(fetchKey(id, strings.ToLower(name)))
		}
	case "modules":
		c.modules.Invalidate(key("modules", id))
	case "status", "grant", "live", "locale", "commands_page":
		c.users.Invalidate(key("user", id))
	case "delegation":
	default:
		c.log.Debug("projection: cache invalidation: unknown scope", zap.String("scope", scope))
	}
}

func (c *Client) User(ctx context.Context, userID uint64) (User, error) {
	return c.users.GetOrLoad(ctx, key("user", userID), func(ctx context.Context) (User, error) {
		status, active, _, locale, commandsPageHidden, err := c.store.GetUser(ctx, userID)
		if err == nil && status != "" {
			return User{Status: status, IsActive: active, Locale: locale, CommandsPageHidden: commandsPageHidden}, nil
		}

		reply, err := bus.RequestJSONTimeout[User](ctx, c.nc, c.subjects.Users, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			return User{Status: "standard"}, nil
		}
		return reply, nil
	})
}

// The returned map is the cached one; callers must not mutate it.
func (c *Client) Modules(ctx context.Context, userID uint64) (map[string]ModuleView, error) {
	return c.modules.GetOrLoad(ctx, key("modules", userID), func(ctx context.Context) (map[string]ModuleView, error) {
		if mods, projected, err := c.store.GetModules(ctx, userID); err == nil && projected {
			return mods, nil
		}

		reply, err := bus.RequestJSONTimeout[struct {
			Modules []ModuleView `json:"modules"`
		}](ctx, c.nc, c.subjects.Modules, projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			// An empty map here would be cached for the TTL and silently disable automod.
			return nil, err
		}
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

type perName[V, E any] struct {
	entries *cache.Cache[E]
	key     func(uint64, string) string
	local   func(context.Context, uint64, string) (V, bool, bool, error)
	entry   func(V, bool) E
	remote  func(context.Context, uint64, string) E
}

func (l perName[V, E]) get(ctx context.Context, userID uint64, name string) (E, error) {
	var zero E
	if name == "" {
		return zero, nil
	}
	lname := strings.ToLower(name)
	return l.entries.GetOrLoad(ctx, l.key(userID, lname), func(ctx context.Context) (E, error) {
		if view, found, projected, err := l.local(ctx, userID, lname); err == nil && projected {
			return l.entry(view, found), nil
		}
		return l.remote(ctx, userID, lname), nil
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

func (c *Client) commandsRPC(ctx context.Context, userID uint64, lname string) commandEntry {
	reply, err := bus.RequestJSONTimeout[struct {
		Commands []Command `json:"commands"`
	}](ctx, c.nc, c.subjects.Commands, projectionRequest(userID), c.rpcTimeout)
	if err != nil {
		return commandEntry{found: false}
	}
	return findCommand(reply.Commands, lname)
}

func findCommand(commands []Command, lname string) commandEntry {
	for _, cmd := range commands {
		if commandMatches(cmd, lname) {
			return commandEntry{cmd: cmd, found: true}
		}
	}
	return commandEntry{found: false}
}

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

func (c *Client) fetchesRPC(ctx context.Context, userID uint64, lname string) fetchEntry {
	reply, err := bus.RequestJSONTimeout[struct {
		Fetches []FetchView `json:"fetches"`
	}](ctx, c.nc, c.subjects.Fetches, projectionRequest(userID), c.rpcTimeout)
	if err != nil {
		return fetchEntry{found: false}
	}
	return findFetch(reply.Fetches, lname)
}

func findFetch(fetches []FetchView, lname string) fetchEntry {
	for _, f := range fetches {
		if strings.ToLower(f.Name) == lname {
			return fetchEntry{fetch: f, found: true}
		}
	}
	return fetchEntry{found: false}
}

func key(kind string, userID uint64) string {
	return cache.UserKey(kind+":", userID)
}

func cmdKey(userID uint64, name string) string {
	return cache.PairKey("command:", userID, name)
}

func fetchKey(userID uint64, name string) string {
	return cache.PairKey("fetch:", userID, name)
}
