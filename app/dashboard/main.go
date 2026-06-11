// The dashboard is the user-facing microservice: streamers sign in with
// Twitch, enable the bot on their channel, and manage it. It owns the
// `dashboard` MySQL schema (HeatWave) and reaches every other service over
// NATS, per the data-and-state ownership rules.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	"github.com/nats-io/nats.go"

	"itsbagelbot/dashboard/internal/config"
	"itsbagelbot/dashboard/internal/crypto"
	"itsbagelbot/dashboard/internal/rpc"
	"itsbagelbot/dashboard/internal/store"
	"itsbagelbot/dashboard/internal/twitch"
	"itsbagelbot/dashboard/internal/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBDSN)
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	if err := st.Migrate(context.Background()); err != nil {
		log.Error("db migrate", "err", err)
		os.Exit(1)
	}

	nc, err := nats.Connect(cfg.NATSURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second))
	if err != nil {
		log.Error("nats connect", "err", err)
		os.Exit(1)
	}

	aead, err := crypto.New(cfg.AEADKey)
	if err != nil {
		log.Error("aead init", "err", err)
		os.Exit(1)
	}

	sessions := scs.New()
	sessions.Store = mysqlstore.New(st.DB)
	sessions.Lifetime = 7 * 24 * time.Hour
	sessions.Cookie.Secure = strings.HasPrefix(cfg.BaseURL, "https://")
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.SameSite = http.SameSiteLaxMode

	srv := &web.Server{
		Sessions:    sessions,
		Store:       st,
		Twitch:      twitch.New(cfg.TwitchClientID, cfg.TwitchClientSecret, cfg.BaseURL, cfg.BotScopes),
		Broadcaster: rpc.NewBroadcaster(nc, cfg.BroadcasterStatusSubject),
		AEAD:        aead,
		NATS:        nc,
		StatusSubj:  cfg.StatusSubjectPrefix,
		ConduitID:   cfg.TwitchConduitID,
		Log:         log,
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("dashboard listening", "addr", cfg.ListenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	_ = srv.Shutdown(ctx)
}
