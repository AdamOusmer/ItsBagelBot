// The admin tool is the operator-facing window into the Twitch ingress: the
// live state of every Conduit shard, where it runs in the BEAM cluster, and
// the shard up/down event stream. It reads everything over NATS (the shard
// snapshot via request-reply from Ingress.AdminRpc, events from the status
// subjects) and owns no data. It is reachable only over the tailnet; see
// deploy/k8s/admin.yaml.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"itsbagelbot/admin/internal/config"
	"itsbagelbot/admin/internal/monitor"
	"itsbagelbot/admin/internal/rpc"
	"itsbagelbot/admin/internal/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()

	nrApp, err := monitor.New("admin", log)
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

	srv := &web.Server{
		Ingress:    rpc.NewIngress(nc, cfg.AdminSubject),
		Users:      rpc.NewUsers(nc, cfg.UserSubjectPrefix),
		NATS:       nc,
		StatusSubj: cfg.StatusSubjectPrefix,
		Log:        log,
		NewRelic:   nrApp,
	}

	app := srv.Routes()

	go func() {
		log.Info("admin listening", "addr", cfg.ListenAddr)
		if err := app.Listen(cfg.ListenAddr); err != nil {
			log.Error("fiber server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Info("shutting down admin...")
	_ = app.Shutdown()
	nc.Close()
}
