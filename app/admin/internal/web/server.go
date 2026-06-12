// Package web wires the HTTP surface: the shard fleet page, the polled
// fragment, the SSE bridge from the ingress status subjects, and health.
// There is no auth layer on purpose: the listener is only reachable on the
// nodes' Tailscale addresses, and tailnet ACLs are the access control.
package web

import (
	"bufio"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/integrations/nrfiber"
	"github.com/newrelic/go-agent/v3/newrelic"

	"itsbagelbot/admin/internal/rpc"
	"itsbagelbot/admin/ui"
)

type Server struct {
	Ingress    *rpc.Ingress
	Users      *rpc.Users
	NATS       *nats.Conn
	StatusSubj string
	Log        *slog.Logger
	NewRelic   *newrelic.Application

	snapshotMu    sync.Mutex
	snapshotUntil time.Time
	snapshotValue *rpc.Snapshot
	snapshotErr   string

	statsMu    sync.Mutex
	statsUntil time.Time
	statsValue *rpc.UserStats
	statsErr   string
}

func (s *Server) Routes() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(nrfiber.Middleware(s.NewRelic))
	app.Use(securityHeaders)

	app.Get("/", s.home)
	app.Get("/shards", s.shardsPage)
	app.Get("/users", s.usersPage)
	app.Get("/overview/user-stats", s.userStatsFragment)
	app.Get("/overview/shard-stat", s.shardStat)
	app.Get("/shards/live", s.fragment)
	app.Get("/shards/summary", s.shardSummary)
	app.Get("/events", s.events)
	app.Get("/users/lookup", s.userLookup)
	app.Get("/users/recent", s.userRecent)
	app.Post("/users/action", s.userAction)
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	return app
}

func securityHeaders(c *fiber.Ctx) error {
	c.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' https://fonts.gstatic.com",
		"connect-src 'self'",
		"img-src 'self' data:",
	}, "; "))
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	c.Set("Cache-Control", "no-store")
	return c.Next()
}

func (s *Server) snapshot() (*rpc.Snapshot, string) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	now := time.Now()
	if now.Before(s.snapshotUntil) {
		return s.snapshotValue, s.snapshotErr
	}

	snap, err := s.Ingress.Shards()
	if err != nil {
		s.Log.Warn("shard snapshot failed", "err", err)
		s.snapshotValue = nil
		s.snapshotErr = err.Error()
		s.snapshotUntil = now.Add(2 * time.Second)
		return nil, s.snapshotErr
	}
	s.snapshotValue = snap
	s.snapshotErr = ""
	s.snapshotUntil = now.Add(2 * time.Second)
	return snap, ""
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

func render(c *fiber.Ctx, component templ.Component) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return component.Render(c.Context(), c.Response().BodyWriter())
}

func (s *Server) home(c *fiber.Ctx) error {
	return render(c, ui.Admin(time.Now()))
}

func (s *Server) shardsPage(c *fiber.Ctx) error {
	return render(c, ui.ShardsPage())
}

func (s *Server) usersPage(c *fiber.Ctx) error {
	return render(c, ui.UsersPage())
}

func (s *Server) userStatsFragment(c *fiber.Ctx) error {
	stats, errMsg := s.userStats()
	return render(c, ui.UserStatsCards(stats, errMsg))
}

func (s *Server) shardStat(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, ui.ShardStatCard(snap, errMsg))
}

// fragment serves the #live region the page swaps in on every refresh.
func (s *Server) fragment(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, ui.Live(snap, errMsg, time.Now()))
}

func (s *Server) shardSummary(c *fiber.Ctx) error {
	snap, errMsg := s.snapshot()
	return render(c, ui.ShardSummary(snap, errMsg, time.Now()))
}

func (s *Server) userLookup(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	if len(q) > 128 {
		return render(c, ui.UserError("query too long"))
	}
	u, err := s.Users.Lookup(q)
	if err != nil {
		// An unknown numeric id is grantable: the grant provisions the row.
		if err.Error() == "user not found" && rpc.IsDigits(q) {
			return render(c, ui.UserNotRegistered(q))
		}
		return render(c, ui.UserError(err.Error()))
	}
	return render(c, ui.UserDetail(*u, time.Now()))
}

func (s *Server) userRecent(c *fiber.Ctx) error {
	users, err := s.Users.Recent(20)
	if err != nil {
		return render(c, ui.UserError(err.Error()))
	}
	return render(c, ui.UserRecent(users))
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
	case "vip", "paid", "standard":
		u, err = s.Users.SetStatus(userID, action)
	case "reset":
		u, err = s.Users.Reset(userID)
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		s.Log.Warn("user action failed", "user", userID, "action", action, "err", err)
		return render(c, ui.UserError(err.Error()))
	}
	s.Log.Info("user action", "user", userID, "action", action)
	return render(c, ui.UserDetail(*u, time.Now()))
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

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer sub.Unsubscribe()
		// Push a comment now: shard events can be hours apart, and the
		// browser's EventSource should not sit on an unanswered request.
		fmt.Fprint(w, ": connected\n\n")
		_ = w.Flush()

		ctx := c.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-msgs:
				fmt.Fprintf(w, "data: %s %s\n\n", m.Subject, m.Data)
				_ = w.Flush()
			}
		}
	})

	return nil
}
