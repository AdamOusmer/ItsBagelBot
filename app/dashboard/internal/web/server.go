package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/integrations/nrfiber"
	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/pkg/bus"

	"itsbagelbot/dashboard/internal/crypto"
	"itsbagelbot/dashboard/internal/rpc"

	"itsbagelbot/dashboard/internal/twitch"
	"itsbagelbot/dashboard/ui"
	"itsbagelbot/dashboard/ui/pages"
)

type SessionData struct {
	UserID      string `json:"user_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	ExpiresAt   int64  `json:"expires_at"`
}

// toggleCooldown is the minimum gap between receive-toggle flips per
// broadcaster: every flip creates or deletes EventSub subscriptions, which
// spends Twitch API budget the whole fleet shares.
const toggleCooldown = time.Minute

type Server struct {
	Dashboard   *rpc.Dashboard
	Commands    *rpc.Commands
	Twitch      *twitch.Client
	Broadcaster *rpc.Broadcaster
	AEAD        *crypto.AEAD
	SessionAEAD *crypto.AEAD
	NATS        *nats.Conn
	Outgress    message.Publisher // JetStream publisher for the system lane
	SystemSubj  string            // outgress system lane subject
	BaseURL     string
	Log         *slog.Logger
	NewRelic    *newrelic.Application

	toggleMu sync.Mutex
	toggleAt map[string]time.Time
}

// outgressMessage mirrors the outgress wire contract for the system lane;
// only the fields an eventsub job carries.
type outgressMessage struct {
	Type          string          `json:"type"`
	BroadcasterID string          `json:"broadcaster_id"`
	Payload       json.RawMessage `json:"payload"`
}

// publishEventSub enqueues the receive toggle's intent on the outgress
// system lane. Outgress executes it under the shared Helix rate-limit
// bucket, retrying with paced redelivery, so a momentary Twitch hiccup or an
// exhausted budget delays the flip instead of failing it.
func (s *Server) publishEventSub(ctx context.Context, broadcasterID string, enabled bool) error {
	payload, _ := json.Marshal(map[string]bool{"enabled": enabled})

	return bus.PublishJSON(ctx, s.Outgress, s.SystemSubj, outgressMessage{
		Type:          "eventsub",
		BroadcasterID: broadcasterID,
		Payload:       payload,
	})
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
		// Logout is a plain HTML form POST (not htmx) so it never carries the
		// X-CSRF-Token header. Forced logout is idempotent and harmless, so we
		// exempt it here. All other POSTs (/app/bot) go through htmx and send
		// the header via hx-headers on <body>.
		Next: func(c *fiber.Ctx) bool {
			return c.Method() == fiber.MethodPost && c.Path() == "/auth/logout"
		},
	}))

	app.Use("/assets", filesystem.New(filesystem.Config{
		Root: http.FS(ui.AssetsFS),
		// 1 day; Cloudflare honors this Cache-Control for the static assets it
		// fronts, so fonts/logo/js/css are served from edge cache instead of
		// re-fetched from origin on every navigation.
		MaxAge: 86400,
	}))

	app.Get("/", s.landing)
	app.Get("/auth/login", s.authStart(false))
	app.Get("/auth/callback", s.authCallback)
	app.Get("/auth/enable-bot", s.requireUser(s.authStart(true)))
	app.Get("/auth/enable-bot/callback", s.requireUser(s.botCallback))
	app.Post("/auth/logout", s.logout)
	app.Get("/app", s.requireUser(s.dashboard))
	app.Get("/app/modules", s.requireUser(s.dashboardModules))
	app.Get("/app/commands", s.requireUser(s.dashboardCommands))
	app.Post("/app/commands/save", s.requireUser(s.commandSave))
	app.Post("/app/commands/delete", s.requireUser(s.commandDelete))
	app.Post("/app/bot", s.requireUser(s.botToggle))
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
	// Static assets under /assets are immutable-ish and get their own
	// Cache-Control from the filesystem middleware (MaxAge below). Setting
	// no-store here would leak onto those responses and force Cloudflare and
	// browsers to re-download fonts/logo/js/css on every navigation. HTML
	// responses still get no-store so session-bound pages are never cached.
	if !strings.HasPrefix(c.Path(), "/assets") {
		c.Set("Cache-Control", "no-store")
	}
	return c.Next()
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

func (s *Server) setSecureCookie(c *fiber.Ctx, name, value string, d time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(d.Seconds()),
		HTTPOnly: true,
		Secure:   strings.HasPrefix(s.BaseURL, "https://"),
		SameSite: "Lax",
	})
}

func (s *Server) clearCookie(c *fiber.Ctx, name string) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    "",
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   strings.HasPrefix(s.BaseURL, "https://"),
		SameSite: "Lax",
	})
}

func (s *Server) currentSession(c *fiber.Ctx) (SessionData, bool) {
	val := c.Cookies("bagel_session")
	if val == "" {
		return SessionData{}, false
	}
	enc, err := base64.RawURLEncoding.DecodeString(val)
	if err != nil {
		return SessionData{}, false
	}
	dec, err := s.SessionAEAD.Open(enc, []byte("session"))
	if err != nil {
		return SessionData{}, false
	}
	var data SessionData
	if err := json.Unmarshal(dec, &data); err != nil {
		return SessionData{}, false
	}
	if time.Now().Unix() > data.ExpiresAt {
		return SessionData{}, false
	}
	return data, true
}

func (s *Server) currentUserID(c *fiber.Ctx) string {
	sess, ok := s.currentSession(c)
	if !ok {
		return ""
	}
	return sess.UserID
}

func (s *Server) requireUser(next fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.currentUserID(c) == "" {
			return c.Redirect("/")
		}
		return next(c)
	}
}

func (s *Server) landing(c *fiber.Ctx) error {
	if s.currentUserID(c) != "" {
		return c.Redirect("/app")
	}
	return render(c, pages.Landing())
}

func (s *Server) authStart(bot bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("state generation failed")
		}
		state := base64.RawURLEncoding.EncodeToString(buf)

		s.setSecureCookie(c, "oauth_state", state, 10*time.Minute)

		url := s.Twitch.LoginURL(state)
		if bot {
			url = s.Twitch.BotConsentURL(state)
		}
		return c.Redirect(url)
	}
}

func (s *Server) checkState(c *fiber.Ctx) bool {
	val := c.Cookies("oauth_state")
	queryState := c.Query("state")
	if val == "" || len(val) != len(queryState) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(val), []byte(queryState)) == 1
}

func (s *Server) errorPage(c *fiber.Ctx, status int, title, msg, retryHref, retryLabel string) error {
	c.Status(status)
	return render(c, pages.ErrorPage(title, msg, retryHref, retryLabel))
}

func (s *Server) authCallback(c *fiber.Ctx) error {
	if s.currentUserID(c) != "" && !s.checkState(c) {
		return c.Redirect("/app")
	}
	if !s.checkState(c) {
		s.Log.Warn("oauth state mismatch on login callback")
		return s.errorPage(c, fiber.StatusBadRequest, "Login expired",
			"That sign-in attempt is no longer valid. This can happen when the page is reloaded mid-login.",
			"/auth/login", "Try signing in again")
	}
	s.clearCookie(c, "oauth_state")

	tok, err := s.Twitch.ExchangeLogin(c.Context(), c.Query("code"))
	if err != nil {
		s.Log.Error("login exchange failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Login failed",
			"Twitch did not accept the sign-in. Please try again.",
			"/auth/login", "Try signing in again")
	}
	user, err := s.Twitch.FetchUser(c.Context(), tok)
	if err != nil {
		s.Log.Error("user fetch failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Login failed",
			"We could not read your Twitch profile. Please try again.",
			"/auth/login", "Try signing in again")
	}
	if err := s.Dashboard.UpsertUser(c.Context(), user.ID, user.Login, user.DisplayName); err != nil {
		s.Log.Error("user upsert failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"We could not save your account. Please try again in a moment.",
			"/auth/login", "Try signing in again")
	}

	data := SessionData{
		UserID:      user.ID,
		Login:       user.Login,
		DisplayName: user.DisplayName,
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	js, _ := json.Marshal(data)
	enc, err := s.SessionAEAD.Seal(js, []byte("session"))
	if err != nil {
		s.Log.Error("session encrypt failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Session error. Please try again.", "/auth/login", "Try signing in again")
	}

	s.setSecureCookie(c, "bagel_session", base64.RawURLEncoding.EncodeToString(enc), 7*24*time.Hour)

	s.Log.Info("login", "user", user.Login)
	return c.Redirect("/app")
}

func (s *Server) botCallback(c *fiber.Ctx) error {
	if !s.checkState(c) {
		s.Log.Warn("oauth state mismatch on bot callback")
		return c.Redirect("/app")
	}
	s.clearCookie(c, "oauth_state")

	tok, err := s.Twitch.ExchangeBot(c.Context(), c.Query("code"))
	if err != nil {
		s.Log.Error("bot consent exchange failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Authorization failed",
			"Twitch did not accept the authorization. Please try again.",
			"/auth/enable-bot", "Try again")
	}
	userID := s.currentUserID(c)

	grantee, err := s.Twitch.FetchUser(c.Context(), tok)
	if err != nil || grantee.ID != userID {
		return s.errorPage(c, fiber.StatusForbidden, "Wrong Twitch account",
			"The authorization must be granted by the same Twitch account you are logged in as.",
			"/auth/enable-bot", "Try again")
	}

	if err := s.Dashboard.SaveBotGrant(c.Context(), userID, tok.AccessToken, tok.RefreshToken); err != nil {
		s.Log.Error("grant save failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Could not save the grant. Please try again.", "/auth/enable-bot", "Try again")
	}

	if err := s.publishEventSub(c.Context(), userID, true); err != nil {
		s.Log.Error("eventsub job publish failed", "broadcaster", userID, "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Almost there",
			"Your authorization was saved, but enabling your channel events failed. Please try again.",
			"/auth/enable-bot", "Try again")
	}

	if err := s.Dashboard.SetActive(c.Context(), userID, true); err != nil {
		s.Log.Error("activate failed", "broadcaster", userID, "err", err)
	}

	s.Log.Info("bot enabled", "broadcaster", userID)
	return c.Redirect("/app")
}

// botToggle flips whether the bot receives this channel's events. It enqueues
// the intent on the outgress system lane (on creates the EventSub
// subscriptions on the Conduit, off deletes them) so the Helix calls share
// the fleet rate-limit bucket, then records the new active state. The grant
// itself stays stored either way, so resuming never needs a new consent.
func (s *Server) botToggle(c *fiber.Ctx) error {
	userID := s.currentUserID(c)
	on := c.FormValue("state") == "on"

	if wait := s.toggleWait(userID); wait > 0 {
		return s.errorPage(c, fiber.StatusTooManyRequests, "Not so fast",
			fmt.Sprintf("Event delivery was changed recently. You can change it again in %d seconds.", int(wait.Seconds())+1),
			"/app", "Back to dashboard")
	}

	hasGrant, err := s.Dashboard.HasBotGrant(c.Context(), userID)
	if err != nil {
		s.Log.Error("grant lookup failed", "broadcaster", userID, "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Could not check your authorization. Please try again.", "/app", "Back to dashboard")
	}
	if !hasGrant {
		return c.Redirect("/auth/enable-bot")
	}

	if err := s.publishEventSub(c.Context(), userID, on); err != nil {
		s.Log.Error("eventsub job publish failed", "broadcaster", userID, "on", on, "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Changing your channel events failed. Please try again in a minute.",
			"/app", "Back to dashboard")
	}

	if err := s.Dashboard.SetActive(c.Context(), userID, on); err != nil {
		s.Log.Error("active_set failed", "broadcaster", userID, "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Could not save the new state. Please try again.", "/app", "Back to dashboard")
	}

	s.Log.Info("receive toggle", "broadcaster", userID, "on", on)

	if c.Get("HX-Request") == "true" {
		tier := s.Broadcaster.Tier(userID)
		enabled, _ := s.Dashboard.HasBotGrant(c.Context(), userID)
		receiving, _ := s.Dashboard.IsActive(c.Context(), userID)
		sess, _ := s.currentSession(c)
		return render(c, pages.OverviewContent(sess.DisplayName, tier, enabled, receiving))
	}

	return c.Redirect("/app")
}

// toggleWait enforces the per-broadcaster cooldown. A non-zero return is how
// long the caller still has to wait; zero starts a new cooldown window
// immediately, so even failed flips count against the budget.
func (s *Server) toggleWait(userID string) time.Duration {
	s.toggleMu.Lock()
	defer s.toggleMu.Unlock()

	now := time.Now()
	if at, ok := s.toggleAt[userID]; ok {
		if rem := toggleCooldown - now.Sub(at); rem > 0 {
			return rem
		}
	}

	if s.toggleAt == nil {
		s.toggleAt = make(map[string]time.Time)
	}
	// Opportunistic sweep keeps the map from growing with one entry per
	// broadcaster forever.
	if len(s.toggleAt) > 1024 {
		for id, at := range s.toggleAt {
			if now.Sub(at) > toggleCooldown {
				delete(s.toggleAt, id)
			}
		}
	}

	s.toggleAt[userID] = now
	return 0
}

func scopesOf(tok interface{ Extra(string) any }) []string {
	if raw, ok := tok.Extra("scope").([]any); ok {
		out := make([]string, 0, len(raw))
		for _, s := range raw {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func (s *Server) logout(c *fiber.Ctx) error {
	s.clearCookie(c, "bagel_session")
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/")
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect("/")
}

func (s *Server) dashboard(c *fiber.Ctx) error {
	sess, _ := s.currentSession(c)
	userID := sess.UserID
	tier := s.Broadcaster.Tier(userID)
	enabled, err := s.Dashboard.HasBotGrant(c.Context(), userID)
	if err != nil {
		s.Log.Error("grant lookup failed", "err", err)
	}

	receiving := false
	if enabled {
		if receiving, err = s.Dashboard.IsActive(c.Context(), userID); err != nil {
			s.Log.Error("active lookup failed", "err", err)
		}
	}

	if c.Get("HX-Request") == "true" {
		return render(c, pages.OverviewContent(sess.DisplayName, tier, enabled, receiving))
	}

	return render(c, pages.Overview(sess.Login, sess.DisplayName, tier, enabled, receiving))
}

func (s *Server) dashboardModules(c *fiber.Ctx) error {
	sess, _ := s.currentSession(c)
	return render(c, pages.Modules(sess.Login))
}

func (s *Server) dashboardCommands(c *fiber.Ctx) error {
	sess, _ := s.currentSession(c)
	cmds, err := s.Commands.List(c.Context(), sess.UserID)
	if err != nil {
		s.Log.Warn("commands list failed", "user", sess.UserID, "err", err)
		cmds = nil
	}
	sel := c.Query("select")
	var selected *rpc.CommandView
	for i := range cmds {
		if cmds[i].Name == sel {
			selected = &cmds[i]
			break
		}
	}
	if c.Get("HX-Request") == "true" {
		return render(c, pages.CommandsPanel(cmds, selected))
	}
	return render(c, pages.Commands(sess.Login, cmds, selected))
}

func (s *Server) commandSave(c *fiber.Ctx) error {
	sess, _ := s.currentSession(c)
	name := c.FormValue("name")
	response := c.FormValue("response")
	isActive := c.FormValue("is_active") == "on"
	cmds, err := s.Commands.Upsert(c.Context(), sess.UserID, name, response, isActive)
	if err != nil {
		s.Log.Warn("commands upsert failed", "user", sess.UserID, "name", name, "err", err)
		// degrade: re-list so the panel still shows current state
		cmds, _ = s.Commands.List(c.Context(), sess.UserID)
	}
	var selected *rpc.CommandView
	for i := range cmds {
		if cmds[i].Name == name {
			selected = &cmds[i]
			break
		}
	}
	return render(c, pages.CommandsPanel(cmds, selected))
}

func (s *Server) commandDelete(c *fiber.Ctx) error {
	sess, _ := s.currentSession(c)
	name := c.FormValue("name")
	cmds, _ := s.Commands.Delete(c.Context(), sess.UserID, name)
	return render(c, pages.CommandsPanel(cmds, nil))
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.NATS.Close()
	return s.Dashboard.Close()
}
