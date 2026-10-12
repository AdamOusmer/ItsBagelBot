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
	if !unavailable && live.Known && !live.Live {
		return "offline"
	}
	if !unavailable && live.Known && live.Live && live.Version > 0 && now.Sub(time.UnixMilli(live.Version)) <= dormancyLiveFreshness {
		return "live"
	}
	if !s.DormancyUnknownSince.IsZero() && !now.Before(s.DormancyUnknownSince.Add(dormancyUnavailableLimit)) {
		return "unavailable"
	}
	if !unavailable && live.Known && live.Live && (s.DormancyProbeAt.IsZero() || now.Sub(s.DormancyProbeAt) >= dormancyUnavailableLimit) {
		return "probe"
	}
	return "defer"
}

func (w *Worker) dormancyRequest(ctx context.Context, r usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	reply, err := w.lifecycleRequest(ctx, r)
	if err == nil && reply.AccountCreatedAt != 0 && w.runtime != nil {
		err = w.runtime.Apply(ctx, r.UserID, reply)
	}
	return reply, err
}

func (w *Worker) reconcileDormancy(parent context.Context, item usersrpc.LifecycleWork) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	r := usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, AdmissionGeneration: item.State.AdmissionGeneration, Action: "dormancy_claim"}
	claimed, err := w.dormancyRequest(ctx, r)
	if err != nil || !claimed.Applied {
		return
	}
	reader, ok := w.runtime.(dormancyLiveReader)
	live := projection.StreamLive{}
	unavailable := !ok
	if ok {
		live, err = reader.GetStreamLive(ctx, item.UserID)
		unavailable = err != nil
	}
	decision := dormancyDecision(claimed.State, live, unavailable, time.Now().UTC())
	if decision == "probe" {
		r.Action = "dormancy_probe"
		probe, err := w.dormancyRequest(ctx, r)
		if err != nil || !probe.Applied {
			return
		}
		current, err := w.runtime.Get(ctx, item.UserID)
		if err != nil || !current.AllowsBackground() || current.AccountCreatedAt != item.AccountCreatedAt || current.State.AdmissionGeneration != item.State.AdmissionGeneration {
			return
		}
		if w.takeSystemHelix(ctx) != nil {
			decision = "defer"
		} else {
			isLive, checkErr := w.twitch.IsStreamLive(ctx, strconv.FormatUint(item.UserID, 10))
			if checkErr != nil {
				decision = "defer"
			} else if isLive {
				decision = "live"
			} else {
				decision = "offline"
			}
			// A control/stream observation accepted during the call supersedes its
			// reply. No old result is restamped into the global live projection.
			latest, readErr := reader.GetStreamLive(ctx, item.UserID)
			if readErr != nil {
				decision = "defer"
			} else if latest.Version > live.Version {
				decision = dormancyDecision(probe.State, latest, false, time.Now().UTC())
			}
		}
	}
	switch decision {
	case "offline", "unavailable":
		r.Action = "dormancy"
		r.Reason = decision
	case "live":
		r.Action = "dormancy_live"
	default:
		r.Action = "dormancy_defer"
	}
	_, _ = w.dormancyRequest(ctx, r)
}
