// Package web wires the HTTP surface: OAuth flows, the dashboard page, the
// SSE bridge from NATS status subjects, and health.
package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/nats-io/nats.go"

	"itsbagelbot/dashboard/internal/crypto"
	"itsbagelbot/dashboard/internal/rpc"
	"itsbagelbot/dashboard/internal/store"
	"itsbagelbot/dashboard/internal/twitch"
	"itsbagelbot/dashboard/ui"
)

type Server struct {
	Sessions    *scs.SessionManager
	Store       *store.Store
	Twitch      *twitch.Client
	Broadcaster *rpc.Broadcaster
	AEAD        *crypto.AEAD
	NATS        *nats.Conn
	StatusSubj  string
	ConduitID   string
	Log         *slog.Logger
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.landing)
	mux.HandleFunc("GET /auth/login", s.authStart(false))
	mux.HandleFunc("GET /auth/callback", s.authCallback)
	mux.HandleFunc("GET /auth/enable-bot", s.requireUser(s.authStart(true)))
	mux.HandleFunc("GET /auth/enable-bot/callback", s.requireUser(s.botCallback))
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /app", s.requireUser(s.dashboard))
	mux.HandleFunc("GET /app/events", s.requireUser(s.events))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	return s.Sessions.LoadAndSave(mux)
}

func (s *Server) currentUserID(r *http.Request) string {
	return s.Sessions.GetString(r.Context(), "twitch_user_id")
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.currentUserID(r) == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	if s.currentUserID(r) != "" {
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	_ = ui.Landing().Render(r.Context(), w)
}

// authStart begins either the identity login or the bot-scope consent. State
// is bound to the session to stop CSRF on the callback.
func (s *Server) authStart(bot bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "state generation failed", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(buf)
		s.Sessions.Put(r.Context(), "oauth_state", state)

		url := s.Twitch.LoginURL(state)
		if bot {
			url = s.Twitch.BotConsentURL(state)
		}
		http.Redirect(w, r, url, http.StatusFound)
	}
}

// checkState compares without consuming: browsers (Safari especially) may
// fetch the callback URL more than once (preload, refresh), and burning the
// state on first read turns the user-visible navigation into a false
// mismatch. The state is removed only after a successful login.
func (s *Server) checkState(r *http.Request) bool {
	want := s.Sessions.GetString(r.Context(), "oauth_state")
	return want != "" && want == r.URL.Query().Get("state")
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, msg, retryHref, retryLabel string) {
	w.WriteHeader(status)
	_ = ui.ErrorPage(title, msg, retryHref, retryLabel).Render(r.Context(), w)
}

func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	// A replayed callback after a completed login (preload, back button,
	// refresh) is not an error; the user is signed in, send them in.
	if s.currentUserID(r) != "" && !s.checkState(r) {
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	if !s.checkState(r) {
		s.Log.Warn("oauth state mismatch on login callback")
		s.errorPage(w, r, http.StatusBadRequest, "Login expired",
			"That sign-in attempt is no longer valid. This can happen when the page is reloaded mid-login.",
			"/auth/login", "Try signing in again")
		return
	}
	tok, err := s.Twitch.ExchangeLogin(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.Log.Error("login exchange failed", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Login failed",
			"Twitch did not accept the sign-in. Please try again.",
			"/auth/login", "Try signing in again")
		return
	}
	user, err := s.Twitch.FetchUser(r.Context(), tok)
	if err != nil {
		s.Log.Error("user fetch failed", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Login failed",
			"We could not read your Twitch profile. Please try again.",
			"/auth/login", "Try signing in again")
		return
	}
	if err := s.Store.UpsertUser(r.Context(), store.User{
		TwitchUserID: user.ID, Login: user.Login, DisplayName: user.DisplayName,
	}); err != nil {
		s.Log.Error("user upsert failed", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Something broke",
			"We could not save your account. Please try again in a moment.",
			"/auth/login", "Try signing in again")
		return
	}

	// Rotate the session token on privilege change; only now is the
	// one-time state actually consumed.
	if err := s.Sessions.RenewToken(r.Context()); err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, "Something broke",
			"Session error. Please try again.", "/auth/login", "Try signing in again")
		return
	}
	s.Sessions.Remove(r.Context(), "oauth_state")
	s.Sessions.Put(r.Context(), "twitch_user_id", user.ID)
	s.Sessions.Put(r.Context(), "login", user.Login)
	s.Sessions.Put(r.Context(), "display_name", user.DisplayName)
	s.Log.Info("login", "user", user.Login)
	http.Redirect(w, r, "/app", http.StatusFound)
}

// botCallback stores the broadcaster's refresh token, AEAD-encrypted and bound
// to their user ID, then nudges the fleet via the cache invalidation subject
// so tier/grant changes propagate immediately.
func (s *Server) botCallback(w http.ResponseWriter, r *http.Request) {
	if !s.checkState(r) {
		// Replay after a completed grant lands here too; the dashboard
		// shows the real grant state either way.
		s.Log.Warn("oauth state mismatch on bot callback")
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	tok, err := s.Twitch.ExchangeBot(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.Log.Error("bot consent exchange failed", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Authorization failed",
			"Twitch did not accept the authorization. Please try again.",
			"/auth/enable-bot", "Try again")
		return
	}
	userID := s.currentUserID(r)

	// The grant must belong to the logged-in broadcaster, not whoever the
	// Twitch session in the browser happens to be.
	grantee, err := s.Twitch.FetchUser(r.Context(), tok)
	if err != nil || grantee.ID != userID {
		s.errorPage(w, r, http.StatusForbidden, "Wrong Twitch account",
			"The authorization must be granted by the same Twitch account you are logged in as.",
			"/auth/enable-bot", "Try again")
		return
	}

	enc, err := s.AEAD.Seal([]byte(tok.RefreshToken), []byte(userID))
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, "Something broke",
			"Could not secure the grant. Please try again.", "/auth/enable-bot", "Try again")
		return
	}
	scopes := strings.Join(scopesOf(tok), " ")
	if err := s.Store.SaveBotGrant(r.Context(), userID, scopes, enc); err != nil {
		s.Log.Error("grant save failed", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Something broke",
			"Could not save the grant. Please try again.", "/auth/enable-bot", "Try again")
		return
	}

	// The grant alone routes nothing: Twitch only sends chat into the
	// Conduit once this subscription exists.
	if err := s.Twitch.EnsureChatSubscription(r.Context(), userID, s.ConduitID); err != nil {
		s.Log.Error("chat subscription failed", "broadcaster", userID, "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Almost there",
			"Your authorization was saved, but subscribing to your chat failed. Please try again.",
			"/auth/enable-bot", "Try again")
		return
	}

	s.Sessions.Remove(r.Context(), "oauth_state")
	s.Log.Info("bot enabled", "broadcaster", userID)
	http.Redirect(w, r, "/app", http.StatusFound)
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

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_ = s.Sessions.Destroy(r.Context())
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	userID := s.currentUserID(r)
	tier := s.Broadcaster.Tier(userID)
	enabled, err := s.Store.HasBotGrant(r.Context(), userID)
	if err != nil {
		s.Log.Error("grant lookup failed", "err", err)
	}
	_ = ui.Dashboard(
		s.Sessions.GetString(r.Context(), "login"),
		s.Sessions.GetString(r.Context(), "display_name"),
		tier,
		enabled,
	).Render(r.Context(), w)
}

// events streams ingress status messages to the browser as SSE.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	msgs := make(chan *nats.Msg, 16)
	sub, err := s.NATS.ChanSubscribe(s.StatusSubj+".>", msgs)
	if err != nil {
		http.Error(w, "status feed unavailable", http.StatusBadGateway)
		return
	}
	defer sub.Unsubscribe()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-msgs:
			fmt.Fprintf(w, "data: %s %s\n\n", m.Subject, m.Data)
			flusher.Flush()
		}
	}
}

// Shutdown is a hook for graceful teardown.
func (s *Server) Shutdown(ctx context.Context) error {
	s.NATS.Close()
	return s.Store.DB.Close()
}
