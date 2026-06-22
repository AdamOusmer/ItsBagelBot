package rpc

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// liveRPC answers the worker's cold-key live query. It serves the projector's
// own projected live signal when it has one; otherwise it escalates to Twitch by
// publishing a stream_status job on the outgress system lane and replies unknown,
// so the worker treats the broadcaster as offline until the escalation refreshes
// the live key.
type liveRPC struct {
	store         *projection.Store
	pub           message.Publisher
	systemSubject string
	log           *zap.Logger
}

// SubscribeLive registers the projector live verb on subject. systemSubject is
// the outgress system lane the escalation job rides; pub is a JetStream publisher.
func SubscribeLive(nc *nats.Conn, store *projection.Store, pub message.Publisher, subject, systemSubject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	l := &liveRPC{store: store, pub: pub, systemSubject: systemSubject, log: log}
	return bus.QueueSubscribeJSON[projectorrpc.LiveRequest, projectorrpc.LiveReply](nc, subject, queueGroup, 1500*time.Millisecond, app, log, l.handleGet)
}

func (l *liveRPC) handleGet(ctx context.Context, req projectorrpc.LiveRequest) projectorrpc.LiveReply {
	if req.BroadcasterID == "" {
		return projectorrpc.LiveReply{Error: "bad request"}
	}
	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil {
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Error: "bad request"}
	}

	live, known, err := l.store.GetStreamLive(ctx, id)
	if err != nil {
		// Valkey error: do not escalate, just answer offline/unknown.
		l.log.Warn("live rpc: store read failed", zap.Uint64("broadcaster_id", id), zap.Error(err))
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: false, Known: false}
	}
	if known {
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: live, Known: true}
	}

	// Cold: escalate to Twitch via outgress; the re-check writes the live key for
	// the next read. Reply unknown (offline) now.
	l.escalate(ctx, req.BroadcasterID)
	return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: false, Known: false}
}

func (l *liveRPC) escalate(ctx context.Context, broadcasterID string) {
	if l.pub == nil || l.systemSubject == "" {
		return
	}
	body, err := json.Marshal(outgress.StreamStatusJob{BroadcasterID: broadcasterID})
	if err != nil {
		return
	}
	if err := bus.PublishJSON(ctx, l.pub, l.systemSubject, outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: broadcasterID,
		Payload:       body,
	}); err != nil {
		l.log.Warn("live rpc: failed to escalate to outgress", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
