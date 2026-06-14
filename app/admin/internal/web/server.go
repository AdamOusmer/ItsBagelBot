// Package web wires the HTTP surface: the shard fleet page, the polled
// fragment, the SSE bridge from the ingress status subjects, and health.
// There is no auth layer on purpose: the listener is only reachable on the
// nodes' Tailscale addresses, and tailnet ACLs are the access control.
package web

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/integrations/nrfiber"
	"github.com/newrelic/go-agent/v3/newrelic"

	"itsbagelbot/admin/internal/rpc"
	"itsbagelbot/admin/internal/twitch"
	"itsbagelbot/admin/ui"
	"itsbagelbot/admin/ui/components"
	"itsbagelbot/admin/ui/pages"
)

type Server struct {
	Ingress    *rpc.Ingress
	Users      *rpc.Users
	Twitch     *twitch.Client
	NATS       *nats.Conn
	StatusSubj string
	// BaseURL is the admin's tailnet origin; it decides whether the
	// short-lived oauth_state cookie is marked Secure.
	BaseURL string
	// BotUserID, when non-empty, is the Twitch user id the bot consent must
	// resolve to before its token is stored.
	BotUserID string
	Log       *slog.Logger
	NewRelic  *newrelic.Application

	snapshotMu       sync.Mutex
	snapshotUntil    time.Time
	snapshotValue    *rpc.Snapshot
	snapshotErr      string
	snapshotInflight bool
	snapshotReady    chan struct{} // closed when an in-flight refresh completes

	statsMu    sync.Mutex
	statsUntil time.Time
	statsValue *rpc.UserStats
	statsErr   string

	lanesMu      sync.Mutex
	lanesSampler *laneSampler

	storeOnce sync.Once
	laneKV    *laneStore

	// RPCMonitorURL is the NATS HTTP monitor base (e.g. http://nats:8222) the
	// RPC responder telemetry scrapes. Empty disables the RPC section.
	RPCMonitorURL string
	rpcMonOnce    sync.Once
	rpcMon        *rpcMonitor
}

// store returns the lazily-built lane metadata store (KV aliases), bound to the
// given JetStream context on first use.
func (s *Server) store(js nats.JetStreamContext) *laneStore {
	s.storeOnce.Do(func() { s.laneKV = newLaneStore(js) })
	return s.laneKV
}

// rpcStatus returns the cached RPC responder telemetry, lazily building the
// monitor client on first use.
func (s *Server) rpcStatus() ([]ui.RPCEndpoint, string) {
	if s.RPCMonitorURL == "" {
		return nil, "RPC monitor not configured"
	}
	s.rpcMonOnce.Do(func() { s.rpcMon = newRPCMonitor(s.RPCMonitorURL) })
	return s.rpcMon.snapshot()
}

func (s *Server) Routes() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(nrfiber.Middleware(s.NewRelic))
	app.Use(securityHeaders)
	app.Use(csrf.New(csrf.Config{
		KeyLookup:      "header:X-CSRF-Token",
		CookieName:     "csrf_",
		ContextKey:     "csrf",
		CookieSameSite: "Lax",
		CookieSecure:   true,
		CookieHTTPOnly: true,
	}))
	app.Use("/assets", filesystem.New(filesystem.Config{
		Root:   http.FS(ui.AssetsFS),
		MaxAge: 86400,
	}))

	app.Get("/", s.home)
	app.Get("/shards", s.shardsPage)
	app.Get("/lanes", s.lanesPage)
	app.Get("/users", s.usersPage)
	app.Get("/overview/user-stats", s.userStatsFragment)
	app.Get("/overview/shard-stat", s.shardStat)
	app.Get("/shards/live", s.fragment)
	app.Get("/shards/summary", s.shardSummary)
	app.Get("/lanes/live", s.lanesFragment)
	app.Get("/lanes/rpc", s.rpcFragment)
	app.Post("/lanes/alias", s.laneAlias)
	app.Post("/lanes/durable", s.laneDurable)
	app.Post("/lanes/delete", s.laneDelete)
	app.Get("/events", s.events)
	app.Get("/users/lookup", s.userLookup)
	app.Get("/users/recent", s.userRecent)
	app.Post("/users/action", s.userAction)
	// Bot-account OAuth. Both are GET top-level navigations: /auth/bot leaves
	// the origin to id.twitch.tv, /auth/bot/callback is where Twitch returns.
	// They must be plain <a> links, never htmx, so the browser performs the
	// cross-origin navigation.
	app.Get("/auth/bot", s.botAuthStart)
	app.Get("/auth/bot/callback", s.botAuthCallback)
	app.Get("/overview/bot-account", s.botAccountFragment)
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	return app
}

func securityHeaders(c *fiber.Ctx) error {
	nonceBytes := make([]byte, 16)
	rand.Read(nonceBytes)
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	c.Locals("nonce", nonce)

	c.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		fmt.Sprintf("script-src 'self' 'nonce-%s'", nonce),
		// No nonce in style-src: templ components use inline style="" attributes,
		// and a nonce in style-src makes browsers ignore 'unsafe-inline', which
		// would block those attributes. 'self' covers the embedded style.css.
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self'",
		"connect-src 'self'",
		"img-src 'self' data:",
	}, "; "))
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("Referrer-Policy", "same-origin")
	c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	// Static assets are immutable-ish and embedded; let the /assets filesystem
	// middleware's MaxAge govern caching (so the edge can cache them) instead of
	// forcing no-store on everything. Dynamic pages still get no-store.
	if !strings.HasPrefix(c.Path(), "/assets") {
		c.Set("Cache-Control", "no-store")
	}
	return c.Next()
}

func (s *Server) snapshot() (*rpc.Snapshot, string) {
	s.snapshotMu.Lock()

	// Cache hit: serve stale value immediately.
	if time.Now().Before(s.snapshotUntil) {
		v, e := s.snapshotValue, s.snapshotErr
		s.snapshotMu.Unlock()
		return v, e
	}

	// Another goroutine is already refreshing. Join it: wait on its channel
	// then return whatever it stored. This avoids concurrent RPCs while not
	// holding the mutex across the network call.
	if s.snapshotInflight {
		ch := s.snapshotReady
		s.snapshotMu.Unlock()
		<-ch
		s.snapshotMu.Lock()
		v, e := s.snapshotValue, s.snapshotErr
		s.snapshotMu.Unlock()
		return v, e
	}

	// We are the refresher. Claim the in-flight slot and release the lock
	// before touching the network.
	s.snapshotInflight = true
	s.snapshotReady = make(chan struct{})
	s.snapshotMu.Unlock()

	now := time.Now()
	snap, err := s.Ingress.Shards()

	s.snapshotMu.Lock()
	ch := s.snapshotReady
	s.snapshotInflight = false
	s.snapshotReady = nil
	if err != nil {
		s.Log.Warn("shard snapshot failed", "err", err)
		s.snapshotValue = nil
		s.snapshotErr = err.Error()
		s.snapshotUntil = now.Add(2 * time.Second)
	} else {
		s.snapshotValue = snap
		s.snapshotErr = ""
		s.snapshotUntil = now.Add(2 * time.Second)
	}
	v, e := s.snapshotValue, s.snapshotErr
	s.snapshotMu.Unlock()

	close(ch) // wake any waiters
	return v, e
}

func (s *Server) userStats() (*rpc.UserStats, string) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	now := time.Now()
	if now.Before(s.statsUntil) {
		return s.statsValue, s.statsErr
	}

	stats, err := s.Users.Stats()
	if err != nil {
		s.Log.Warn("user stats failed", "err", err)
		s.statsValue = nil
		s.statsErr = err.Error()
		s.statsUntil = now.Add(5 * time.Second)
		return nil, s.statsErr
	}
	s.statsValue = stats
	s.statsErr = ""
	s.statsUntil = now.Add(30 * time.Second)
	return stats, ""
}

// lanes returns the live JetStream consumer telemetry, sampled at most once per
// second so repeated htmx polls within a tick reuse the same broker read. The
// sampler is stateful across calls (it diffs delivery counters to derive a real
// throughput), so it lives on the Server behind its own mutex. The ~1s TTL is
// shorter than the 2s fragment poll, so each poll observes a fresh tick while
// concurrent polls share one read.
func (s *Server) lanes() ([]ui.Lane, string) {
	s.lanesMu.Lock()
	defer s.lanesMu.Unlock()

	if s.lanesSampler == nil {
		s.lanesSampler = newLaneSampler()
	}
	sampler := s.lanesSampler

	now := time.Now()
	if now.Before(sampler.until) {
		return sampler.value, sampler.err
	}

	js, err := s.NATS.JetStream()
	if err != nil {
		s.Log.Warn("jetstream context failed", "err", err)
		sampler.value = nil
		sampler.err = err.Error()
		sampler.until = now.Add(2 * time.Second)
		return nil, sampler.err
	}

	lanes, errMsg := sampler.collect(js, now)
	if errMsg != "" {
		s.Log.Warn("lane telemetry failed", "err", errMsg)
		sampler.value = nil
		sampler.err = errMsg
		sampler.until = now.Add(2 * time.Second)
		return nil, errMsg
	}
	if aliases := s.store(js).aliases(); len(aliases) > 0 {
		for i := range lanes {
			lanes[i].Alias = aliases[laneAliasKey(lanes[i].Stream, lanes[i].Consumer)]
		}
	}
	sampler.value = lanes
	sampler.err = ""
	sampler.until = now.Add(time.Second)
	return lanes, ""
}

func render(c *fiber.Ctx, component templ.Component) error {
	c.Set("Content-Type", "text/html; charset=utf-8")

	ctx := c.Context()
	var stdCtx context.Context = ctx

	if nonce, ok := c.Locals("nonce").(string); ok {
		stdCtx = context.WithValue(stdCtx, "nonce", nonce)
	}
	if token, ok := c.Locals("csrf").(string); ok {
		stdCtx = context.WithValue(stdCtx, "csrf", token)
	}

	return component.Render(stdCtx, c.Response().BodyWriter())
}

func (s *Server) home(c *fiber.Ctx) error {
	if c.Get("HX-Request") == "true" {
		return render(c, pages.HomePartial(time.Now()))
	}
	return render(c, pages.Home(time.Now()))
}

func (s *Server) shardsPage(c *fiber.Ctx) error {
	if c.Get("HX-Request") == "true" {
		return render(c, pages.ShardsPartial())
	}
	return render(c, pages.Shards("Shard Health - ItsBagelBot Admin"))
}

func (s *Server) lanesPage(c *fiber.Ctx) error {
	if c.Get("HX-Request") == "true" {
		return render(c, pages.LanesPartial())
	}
	return render(c, pages.Lanes("Lane Telemetry - ItsBagelBot Admin"))
}

// lanesFragment serves the #lanes-live region the page swaps in every 2s.
func (s *Server) lanesFragment(c *fiber.Ctx) error {
	lanes, errMsg := s.lanes()
	return render(c, components.Lanes(lanes, errMsg))
}

// rpcFragment serves the #rpc-live region: core-NATS request-reply responder
// health, scraped from the NATS monitor and refreshed on its own poll.
func (s *Server) rpcFragment(c *fiber.Ctx) error {
	eps, errMsg := s.rpcStatus()
	return render(c, components.RPCStatus(eps, errMsg))
}

func (s *Server) usersPage(c *fiber.Ctx) error {
	if c.Get("HX-Request") == "true" {
		return render(c, pages.UsersPartial())
	}
	return render(c, pages.Users("Users - ItsBagelBot Admin"))
}

func (s *Server) userStatsFragment(c *fiber.Ctx) error {
	stats, errMsg := s.userStats()
	return render(c, components.UserStatsCards(stats, errMsg))
}

func (s *Server) shardStat(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, components.ShardStatCard(snap, errMsg))
}

// fragment serves the #live region the page swaps in on every refresh.
func (s *Server) fragment(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, components.Live(snap, errMsg, time.Now()))
}

func (s *Server) shardSummary(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, components.ShardSummary(snap, errMsg, time.Now()))
}

func (s *Server) userLookup(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	if len(q) > 128 {
		return render(c, components.UserError("query too long"))
	}
	u, err := s.Users.Lookup(q)
	if err != nil {
		// An unknown numeric id is grantable: the grant provisions the row.
		if err.Error() == "user not found" && rpc.IsDigits(q) {
			return render(c, components.UserNotRegistered(q))
		}
		return render(c, components.UserError(err.Error()))
	}
	return render(c, components.UserDetail(*u, s.tokenPresent(u.ID), time.Now()))
}

// tokenPresent reports whether the user has a stored Twitch token; lookup
// failures degrade to "absent" rather than blocking the whole detail view.
func (s *Server) tokenPresent(id uint64) bool {
	ts, err := s.Users.TokenGet(fmt.Sprint(id))
	if err != nil {
		s.Log.Warn("token status failed", "user", id, "err", err)
		return false
	}
	return ts.Present
}

// setStateCookie writes the short-lived, HttpOnly, SameSite=Lax oauth_state
// cookie. Secure is set when the admin origin is https (its tailnet origin
// always is in production); leaving it off for a localhost http origin keeps
// local testing workable.
func (s *Server) setStateCookie(c *fiber.Ctx, value string) {
	c.Cookie(&fiber.Cookie{
		Name:     "oauth_state",
		Value:    value,
		MaxAge:   int((10 * time.Minute).Seconds()),
		HTTPOnly: true,
		Secure:   strings.HasPrefix(s.BaseURL, "https://"),
		SameSite: "Lax",
	})
}

func (s *Server) clearStateCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "oauth_state",
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   strings.HasPrefix(s.BaseURL, "https://"),
		SameSite: "Lax",
	})
}

// checkState constant-time compares the state cookie against the callback's
// state query param, defeating login-CSRF on the callback.
func (s *Server) checkState(c *fiber.Ctx) bool {
	val := c.Cookies("oauth_state")
	queryState := c.Query("state")
	if val == "" || len(val) != len(queryState) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(val), []byte(queryState)) == 1
}

// botAuthStart begins the bot-account consent: it mints a random state, parks
// it in a short-lived cookie, and 302-redirects the browser to id.twitch.tv.
func (s *Server) botAuthStart(c *fiber.Ctx) error {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("state generation failed")
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	s.setStateCookie(c, state)
	return c.Redirect(s.Twitch.BotConsentURL(state))
}

// botAuthCallback completes the consent: validate state, exchange the code,
// resolve the token's owner, optionally enforce the configured bot id, then
// persist the user token via the users-service token_set RPC under the
// resolved id. No token value is ever logged; the users service encrypts at
// rest.
func (s *Server) botAuthCallback(c *fiber.Ctx) error {
	if !s.checkState(c) {
		s.Log.Warn("oauth state mismatch on bot callback")
		return s.botResult(c, fiber.StatusBadRequest, false,
			"That authorization is no longer valid (the page may have been reloaded mid-flow). Start again.")
	}
	s.clearStateCookie(c)

	if errParam := c.Query("error"); errParam != "" {
		s.Log.Warn("bot consent denied", "error", errParam)
		return s.botResult(c, fiber.StatusBadRequest, false,
			"Twitch did not grant the authorization. Start again to retry.")
	}

	tok, err := s.Twitch.ExchangeBot(c.Context(), c.Query("code"))
	if err != nil {
		s.Log.Error("bot consent exchange failed", "err", err)
		return s.botResult(c, fiber.StatusInternalServerError, false,
			"Twitch did not accept the authorization. Start again to retry.")
	}

	owner, err := s.Twitch.FetchUser(c.Context(), tok)
	if err != nil {
		s.Log.Error("bot user fetch failed", "err", err)
		return s.botResult(c, fiber.StatusInternalServerError, false,
			"Could not read the authorized Twitch profile. Start again to retry.")
	}

	if s.BotUserID != "" && owner.ID != s.BotUserID {
		s.Log.Warn("bot consent owner mismatch", "expected", s.BotUserID, "got_login", owner.Login)
		return s.botResult(c, fiber.StatusForbidden, false,
			"Wrong Twitch account. The authorization must be granted by the configured bot account.")
	}

	if _, err := s.Users.TokenSet(owner.ID, tok.AccessToken, tok.RefreshToken); err != nil {
		s.Log.Error("bot token_set failed", "user", owner.ID, "err", err)
		return s.botResult(c, fiber.StatusInternalServerError, false,
			"The authorization succeeded but storing the token failed. Start again to retry.")
	}

	s.Log.Info("bot account token stored", "user", owner.ID, "login", owner.Login)
	return s.botResult(c, fiber.StatusOK, true,
		fmt.Sprintf("Bot account @%s is authorized and its token is stored.", owner.Login))
}

// botResult renders the post-callback page; the status drives the badge.
func (s *Server) botResult(c *fiber.Ctx, status int, ok bool, msg string) error {
	c.Status(status)
	return render(c, pages.BotCallbackResult(ok, msg))
}

// botAccountFragment renders the bot-account card on the overview, reflecting
// the stored-token status pulled from the users-service token RPC.
func (s *Server) botAccountFragment(c *fiber.Ctx) error {
	present := false
	if s.BotUserID != "" {
		ts, err := s.Users.TokenGet(s.BotUserID)
		if err != nil {
			s.Log.Warn("bot token status failed", "err", err)
		} else {
			present = ts.Present
		}
	}
	return render(c, components.BotAccount(s.BotUserID, present))
}

func (s *Server) userRecent(c *fiber.Ctx) error {
	users, err := s.Users.Recent(20)
	if err != nil {
		return render(c, components.UserError(err.Error()))
	}
	return render(c, components.UserRecent(users))
}

// userAction is the only mutating endpoint. Tailnet reachability is the
// access control, but the custom-header check below still matters: it stops
// a malicious public website in an operator's browser from firing
// cross-origin form POSTs at the private address (a custom header forces a
// CORS preflight, which this server never grants).
func (s *Server) userAction(c *fiber.Ctx) error {
	if c.Get("X-Admin-Request") != "1" {
		return c.Status(fiber.StatusForbidden).SendString("missing admin header")
	}
	userID := c.FormValue("user_id")
	action := c.FormValue("action")

	var (
		u   *rpc.AdminUser
		err error
	)
	switch action {
	case "vip", "paid", "free":
		u, err = s.Users.SetStatus(userID, action)
	case "reset":
		u, err = s.Users.Reset(userID)
	case "set_token":
		// The token values pass through to the users service, which encrypts
		// them at rest; nothing here logs or stores them.
		if _, err = s.Users.TokenSet(userID, c.FormValue("access_token"), c.FormValue("refresh_token")); err == nil {
			u, err = s.Users.Lookup(userID)
			if err != nil {
				// Token was committed; the refetch failed. Surface a partial-success
				// message rather than implying the write never happened.
				s.Log.Warn("token set succeeded but user refetch failed", "user", userID, "err", err)
				return render(c, components.UserError("token saved; reload the page to refresh the user view"))
			}
		}
	case "clear_token":
		if _, err = s.Users.TokenClear(userID); err == nil {
			u, err = s.Users.Lookup(userID)
			if err != nil {
				s.Log.Warn("token clear succeeded but user refetch failed", "user", userID, "err", err)
				return render(c, components.UserError("token cleared; reload the page to refresh the user view"))
			}
		}
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		s.Log.Warn("user action failed", "user", userID, "action", action, "err", err)
		return render(c, components.UserError(err.Error()))
	}
	s.Log.Info("user action", "user", userID, "action", action)
	return render(c, components.UserDetail(*u, s.tokenPresent(u.ID), time.Now()))
}

// events streams ingress shard up/down messages to the browser as SSE.
func (s *Server) events(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	msgs := make(chan *nats.Msg, 16)
	sub, err := s.NATS.ChanSubscribe(s.StatusSubj+".>", msgs)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString("status feed unavailable")
	}

	// Guard against double-unsubscribe: the stream-writer closure and the
	// handler-scope defer both call unsub(); sync.Once ensures exactly one fires.
	var unsubOnce sync.Once
	unsub := func() { unsubOnce.Do(func() { _ = sub.Unsubscribe() }) }
	// Handler-scope defer: fires if SetBodyStreamWriter's callback never runs
	// (e.g. client dropped between subscribe and writer setup).
	defer unsub()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer unsub()
		// Push a comment now: shard events can be hours apart, and the
		// browser's EventSource should not sit on an unanswered request.
		fmt.Fprint(w, ": connected\n\n")
		if err := w.Flush(); err != nil {
			return
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case m := <-msgs:
				if _, err := fmt.Fprintf(w, "data: %s %s\n\n", m.Subject, m.Data); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}
