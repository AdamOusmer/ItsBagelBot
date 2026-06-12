// broadcaster-data is the data-plane service that owns the broadcaster_data
// schema (the ent layer: users, configs, tokens, timers). It answers the
// broadcaster status RPC over NATS that the Twitch ingress and dashboard
// already speak, and is the only service allowed to touch this schema.
//
// HA: replicas subscribe with a queue group, so NATS load-balances requests
// across pods and any single pod (or node) can die without dropping the RPC.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/db/ent"
	"ItsBagelBot/internal/db/ent/user"
	"ItsBagelBot/pkg/logger"
)

const rpcQueueGroup = "broadcaster-data"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type statusRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type statusReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Tier          string `json:"tier"`
}

// tierOf resolves the broadcaster's lane tier. The tier lives in the user's
// Configs JSON blob under "tier"; anything else (no user, no config, no key,
// inactive account) is standard.
func tierOf(ctx context.Context, db *ent.Client, broadcasterID string) string {
	var id uint64
	if _, err := fmt.Sscanf(broadcasterID, "%d", &id); err != nil {
		return "standard"
	}

	u, err := db.User.Query().
		Where(user.ID(id), user.IsActive(true)).
		WithConfigs().
		Only(ctx)
	if err != nil || u.Edges.Configs == nil {
		return "standard"
	}

	var cfg struct {
		Tier string `json:"tier"`
	}
	if json.Unmarshal(u.Edges.Configs.Configs, &cfg) == nil && cfg.Tier == "premium" {
		return "premium"
	}
	return "standard"
}

func main() {
	log := logger.New(env("ENV", "production"))
	defer log.Sync()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&tls=%s",
		env("DB_USER", "broadcaster_data_svc"), os.Getenv("DB_PASSWORD"),
		env("DB_HOST", "127.0.0.1"), env("DB_PORT", "3306"),
		env("DB_NAME", "broadcaster_data"), env("DB_TLS_MODE", "skip-verify"))

	db, err := ent.Open("mysql", dsn)
	if err != nil {
		log.Fatal("db open", zap.Error(err))
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.Schema.Create(ctx); err != nil {
		cancel()
		log.Fatal("db migrate", zap.Error(err))
	}
	cancel()

	natsURL := fmt.Sprintf("nats://%s:%s", env("NATS_HOST", "127.0.0.1"), env("NATS_PORT", "4222"))
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(-1), nats.ReconnectWait(2*time.Second))
	if err != nil {
		log.Fatal("nats connect", zap.Error(err))
	}
	defer nc.Close()

	subject := env("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get")
	sub, err := nc.QueueSubscribe(subject, rpcQueueGroup, func(msg *nats.Msg) {
		var req statusRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil || req.BroadcasterID == "" {
			_ = msg.Respond([]byte(`{"error":"bad request"}`))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		reply, _ := json.Marshal(statusReply{
			BroadcasterID: req.BroadcasterID,
			Tier:          tierOf(ctx, db, req.BroadcasterID),
		})
		_ = msg.Respond(reply)
	})
	if err != nil {
		log.Fatal("nats subscribe", zap.Error(err))
	}

	// Operator verbs (admin tool only; reachable solely over the tailnet).
	invalidationSubject := env("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.broadcaster")
	admin := &adminRPC{
		db:                  db,
		nc:                  nc,
		invalidationSubject: invalidationSubject,
		log:                 log,
	}
	adminPrefix := env("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user")
	if err := admin.subscribe(adminPrefix); err != nil {
		log.Fatal("admin subscribe", zap.Error(err))
	}

	// Dashboard data verbs (user upsert, grant save/check).
	dash := &dashboardRPC{
		db:                  db,
		nc:                  nc,
		invalidationSubject: invalidationSubject,
		log:                 log,
	}
	dashPrefix := env("NATS_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.dashboard")
	if err := dash.subscribe(dashPrefix); err != nil {
		log.Fatal("dashboard subscribe", zap.Error(err))
	}

	log.Info("broadcaster-data serving",
		zap.String("subject", subject),
		zap.String("admin_prefix", adminPrefix),
		zap.String("dashboard_prefix", dashPrefix),
		zap.String("queue", rpcQueueGroup))

	httpSrv := &http.Server{Addr: env("LISTEN_ADDR", ":8080"), ReadHeaderTimeout: 5 * time.Second}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !nc.IsConnected() {
			http.Error(w, "nats disconnected", http.StatusServiceUnavailable)
			return
		}
		if _, err := db.User.Query().Limit(1).Exist(r.Context()); err != nil && !ent.IsNotFound(err) {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("http", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	// Drain lets in-flight RPCs finish before the subscription closes.
	_ = sub.Drain()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
