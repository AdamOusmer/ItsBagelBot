package web

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/integrations/nrfiber"
	"github.com/newrelic/go-agent/v3/newrelic"

	"itsbagelbot/dashboard/internal/crypto"
	"itsbagelbot/dashboard/internal/rpc"

	"itsbagelbot/dashboard/internal/twitch"
	"itsbagelbot/dashboard/ui"
)

type SessionData struct {
	UserID      string `json:"user_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	ExpiresAt   int64  `json:"expires_at"`
}

type Server struct {
	Dashboard   *rpc.Dashboard
	Twitch      *twitch.Client
	Broadcaster *rpc.Broadcaster
	AEAD        *crypto.AEAD
	SessionAEAD *crypto.AEAD
	NATS        *nats.Conn
	StatusSubj  string
	ConduitID   string
	BaseURL     string
	Log         *slog.Logger
	NewRelic    *newrelic.Application
}

func (s *Server) Routes() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(nrfiber.Middleware(s.NewRelic))

	app.Get("/", s.landing)
	app.Get("/auth/login", s.authStart(false))
	app.Get("/auth/callback", s.authCallback)
	app.Get("/auth/enable-bot", s.requireUser(s.authStart(true)))
	app.Get("/auth/enable-bot/callback", s.requireUser(s.botCallback))
	app.Post("/auth/logout", s.logout)
	app.Get("/app", s.requireUser(s.dashboard))
	app.Get("/app/events", s.requireUser(s.events))
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	return app
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
	c.Set("Content-Type", "text/html; charset=utf-8")
	return ui.Landing().Render(c.Context(), c.Response().BodyWriter())
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
	c.Set("Content-Type", "text/html; charset=utf-8")
	return ui.ErrorPage(title, msg, retryHref, retryLabel).Render(c.Context(), c.Response().BodyWriter())
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

	enc, err := s.AEAD.Seal([]byte(tok.RefreshToken), []byte(userID))
	if err != nil {
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Could not secure the grant. Please try again.", "/auth/enable-bot", "Try again")
	}
	scopes := strings.Join(scopesOf(tok), " ")
	if err := s.Dashboard.SaveBotGrant(c.Context(), userID, scopes, enc); err != nil {
		s.Log.Error("grant save failed", "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Something broke",
			"Could not save the grant. Please try again.", "/auth/enable-bot", "Try again")
	}

	if err := s.Twitch.EnsureChatSubscription(c.Context(), userID, s.ConduitID); err != nil {
		s.Log.Error("chat subscription failed", "broadcaster", userID, "err", err)
		return s.errorPage(c, fiber.StatusInternalServerError, "Almost there",
			"Your authorization was saved, but subscribing to your chat failed. Please try again.",
			"/auth/enable-bot", "Try again")
	}

	s.Log.Info("bot enabled", "broadcaster", userID)
	return c.Redirect("/app")
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

	c.Set("Content-Type", "text/html; charset=utf-8")
	return ui.Dashboard(sess.Login, sess.DisplayName, tier, enabled).Render(c.Context(), c.Response().BodyWriter())
}

func (s *Server) events(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	msgs := make(chan *nats.Msg, 16)
	sub, err := s.NATS.ChanSubscribe(s.StatusSubj+".>", msgs)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).SendString("status feed unavailable")
	}

	done := c.Context().Done()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer sub.Unsubscribe()
		for {
			select {
			case <-done:
				return
			case m := <-msgs:
				fmt.Fprintf(w, "data: %s %s\n\n", m.Subject, m.Data)
				w.Flush()
			}
		}
	})

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.NATS.Close()
	return s.Dashboard.Close()
}
