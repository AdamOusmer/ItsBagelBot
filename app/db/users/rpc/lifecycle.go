package rpc

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
)

func SubscribeLifecycle(w Wiring) error {
	return bus.Serve(w.Within(tokensBudget), usersrpc.LifecycleSubject, func(ctx context.Context, req usersrpc.LifecycleRequest) usersrpc.LifecycleReply {
		rep, err := w.Repo.Lifecycle(ctx, req)
		if err != nil {
			rep.Refusal = refusal(err)
		}
		return rep
	})
}
