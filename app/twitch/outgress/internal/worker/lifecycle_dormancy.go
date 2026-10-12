package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/projection"
	"context"
	"strconv"
	"time"
)

const dormancyLiveFreshness = 15 * time.Minute
const dormancyUnavailableLimit = 24 * time.Hour

type dormancyLiveReader interface {
	GetStreamLive(context.Context, uint64) (projection.StreamLive, error)
}

// A newer offline version wins over an old online projection. A stale positive
// merits one durable probe per outage window, never one check per sweep/replica.
func dormancyDecision(s lifecycle.State, live projection.StreamLive, unavailable bool, now time.Time) string {
	if dormancyOffline(live, unavailable) {
		return "offline"
	}
	if dormancyFreshLive(live, unavailable, now) {
		return "live"
	}
	if dormancyOutageExpired(s, now) {
		return "unavailable"
	}
	if dormancyProbeDue(s, live, unavailable, now) {
		return "probe"
	}
	return "defer"
}

func (w *Worker) dormancyRequest(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	reply, err := w.lifecycleRequest(ctx, req)
	if err != nil {
		return reply, err
	}
	if reply.AccountCreatedAt == 0 {
		return reply, nil
	}
	return reply, w.applyLifecycle(ctx, req.UserID, reply)
}

func (w *Worker) reconcileDormancy(parent context.Context, item usersrpc.LifecycleWork) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req := workRequest(item, "dormancy_claim")
	claimed, err := w.dormancyRequest(ctx, req)
	if err != nil || !claimed.Applied {
		return
	}
	reader, ok := w.runtime.(dormancyLiveReader)
	live, unavailable := readDormancyLive(ctx, reader, item.UserID)
	if !ok {
		unavailable = true
	}
	decision := dormancyDecision(claimed.State, live, unavailable, time.Now().UTC())
	if decision == "probe" {
		decision = w.probeDormancy(ctx, item, reader, live)
		if decision == "stopped" {
			return
		}
	}
	req = dormancyCompletion(req, decision)
	_, _ = w.dormancyRequest(ctx, req)
}

func dormancyOffline(live projection.StreamLive, unavailable bool) bool {
	if unavailable {
		return false
	}
	if !live.Known {
		return false
	}
	return !live.Live
}
func dormancyFreshLive(live projection.StreamLive, unavailable bool, now time.Time) bool {
	if unavailable {
		return false
	}
	if !live.Known || !live.Live {
		return false
	}
	if live.Version <= 0 {
		return false
	}
	return now.Sub(time.UnixMilli(live.Version)) <= dormancyLiveFreshness
}
func dormancyOutageExpired(s lifecycle.State, now time.Time) bool {
	if s.DormancyUnknownSince.IsZero() {
		return false
	}
	return !now.Before(s.DormancyUnknownSince.Add(dormancyUnavailableLimit))
}
func dormancyProbeDue(s lifecycle.State, live projection.StreamLive, unavailable bool, now time.Time) bool {
	if unavailable {
		return false
	}
	if !live.Known || !live.Live {
		return false
	}
	if s.DormancyProbeAt.IsZero() {
		return true
	}
	return now.Sub(s.DormancyProbeAt) >= dormancyUnavailableLimit
}
func readDormancyLive(ctx context.Context, reader dormancyLiveReader, uid uint64) (projection.StreamLive, bool) {
	if reader == nil {
		return projection.StreamLive{}, true
	}
	live, err := reader.GetStreamLive(ctx, uid)
	return live, err != nil
}
func dormancyCompletion(req usersrpc.LifecycleRequest, decision string) usersrpc.LifecycleRequest {
	switch decision {
	case "offline", "unavailable":
		req.Action = "dormancy"
		req.Reason = decision
	case "live":
		req.Action = "dormancy_live"
	default:
		req.Action = "dormancy_defer"
	}
	return req
}
func (w *Worker) probeDormancy(ctx context.Context, item usersrpc.LifecycleWork, reader dormancyLiveReader, live projection.StreamLive) string {
	probe, err := w.dormancyRequest(ctx, workRequest(item, "dormancy_probe"))
	if err != nil || !probe.Applied {
		return "stopped"
	}
	e := enrollment{broadcasterID: strconv.FormatUint(item.UserID, 10), incarnation: item.AccountCreatedAt, generation: item.State.AdmissionGeneration}
	if !w.enrollmentAllowed(ctx, e) {
		return "stopped"
	}
	if w.takeSystemHelix(ctx) != nil {
		return "defer"
	}
	decision := w.probeStreamDecision(ctx, e.broadcasterID)
	// New stream observations supersede the provider reply; never restamp it globally.
	latest, readErr := reader.GetStreamLive(ctx, item.UserID)
	if readErr != nil {
		return "defer"
	}
	if latest.Version > live.Version {
		return dormancyDecision(probe.State, latest, false, time.Now().UTC())
	}
	return decision
}
func (w *Worker) probeStreamDecision(ctx context.Context, id string) string {
	live, err := w.twitch.IsStreamLive(ctx, id)
	if err != nil {
		return "defer"
	}
	if live {
		return "live"
	}
	return "offline"
}
