package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"itsbagelbot/dashboard/internal/config"
	"itsbagelbot/dashboard/internal/crypto"
	"itsbagelbot/dashboard/internal/monitor"
	"itsbagelbot/dashboard/internal/rpc"

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

	nrApp, err := monitor.New("dashboard", log)
	if err != nil {
		log.Error("new relic init", "err", err)
		os.Exit(1)
	}
	defer monitor.Shutdown(nrApp)

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

	sessionAead, err := crypto.New(cfg.SessionKey)
	if err != nil {
		log.Error("session aead init", "err", err)
		os.Exit(1)
	}

	dash, err := rpc.NewDashboard(nc, cfg.DashboardRPCPrefix, cfg.CacheInvalidationSubject)
	if err != nil {
		log.Error("dashboard rpc", "err", err)
		os.Exit(1)
	}

	srv := &web.Server{
		Dashboard:   dash,
		Twitch:      twitch.New(cfg.TwitchClientID, cfg.TwitchClientSecret, cfg.BaseURL, cfg.BotScopes),
		Broadcaster: rpc.NewBroadcaster(nc, cfg.BroadcasterStatusSubject),
		AEAD:        aead,
		SessionAEAD: sessionAead,
		NATS:        nc,
		StatusSubj:  cfg.StatusSubjectPrefix,
		ConduitID:   cfg.TwitchConduitID,
		BaseURL:     cfg.BaseURL,
		Log:         log,
		NewRelic:    nrApp,
	}

	app := srv.Routes()

	go func() {
		log.Info("dashboard listening", "addr", cfg.ListenAddr)
		if err := app.Listen(cfg.ListenAddr); err != nil {
			log.Error("fiber server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Info("shutting down dashboard...")
	// Fiber Shutdown is graceful by default
	_ = app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
