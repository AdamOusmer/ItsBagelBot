// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package consumers holds the event-plane handlers every data service repeats
// verbatim: the account-deletion sweep and the cache invalidation that follows
// a change event.
//
// It lives beside pkg/bus rather than inside it because it knows the domain
// event DTOs (internal/domain/event/data); pkg/bus itself stays transport-only
// and must keep compiling for callers that carry no domain types.
package consumers

import (
	"context"

	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
)

// Service is the log prefix a data service stamps on its event-plane lines
// ("commands: bad user_deleted payload"). It is a named type, not a plain
// string, so it cannot be swapped with a subject at a call site -- the prefix
// is the only thing that differed between the five hand-written copies of the
// handler below, and it is worth keeping.
type Service string

// OnUserDeleted builds the data.users.deleted handler. sweep removes every row
// the service owns for that account; a service that owns two tables sweeps
// both inside the one closure.
//
// Malformed payloads and invalid ids are logged and dropped rather than
// returned: a redelivery cannot make them parse, so returning an error would
// only spin the subject until the stream ages the message out. A sweep failure
// IS returned, because that one is worth retrying.
func OnUserDeleted(svc Service, log *zap.Logger, sweep func(context.Context, uint64) error) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)

		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn(string(svc)+": bad user_deleted payload", zap.Error(err))
			return nil
		}
		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn(string(svc)+": invalid user_id in user_deleted", zap.Error(err))
			return nil
		}
		if err := sweep(msg.Context(), dto.UserID); err != nil {
			return err
		}

		log.Info(string(svc)+": deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}
}

// OnChangeInvalidate builds the broadcast-side handler that drops the cached
// view of whichever user a change event named. userID reads the id off the
// DTO: Go has no field access on type parameters, so the accessor is the price
// of not writing this closure once per DTO type.
//
// A malformed payload is returned as an error here (unlike OnUserDeleted): a
// missed invalidation leaves a stale view served to a real user, so the
// redelivery is worth having even though it will most likely fail again.
func OnChangeInvalidate[DTO any](userID func(DTO) uint64, invalidate func(uint64)) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto DTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		invalidate(userID(dto))
		return nil
	}
}
