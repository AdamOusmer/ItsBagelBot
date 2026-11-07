// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package watchtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	pkgvalkey "ItsBagelBot/pkg/valkey"
)

const providerCooldownPrefix = "watchtime:provider-reset:"
const providerResetMaximum = 24 * time.Hour

// providerCooldownKey is scoped to the actual provider identity, shared by
// every tenant and worker using that token. It supplements the token budget;
// it never creates another competing rate limiter.
func providerCooldownKey(identity string) (string, error) {
	if identity == "helix:app" {
		return providerCooldownPrefix + identity, nil
	}
	id, ok := strings.CutPrefix(identity, "helix:bot:")
	if !ok {
		return "", errors.New("invalid watch provider identity")
	}
	if id == "" || len(id) > 20 {
		return "", errors.New("invalid watch provider identity")
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return "", errors.New("invalid watch provider identity")
	}
	if n == 0 || strconv.FormatUint(n, 10) != id {
		return "", errors.New("invalid watch provider identity")
	}
	return providerCooldownPrefix + identity, nil
}

// ObserveProviderReset publishes a provider 429 reset fleet-wide. Shorter or
// expired observations cannot release a cooldown another replica established.
// The server clock bounds resets to one day. Keys deliberately have no TTL:
// volatile-lru must never evict a provider reset early. There are only the
// actual bot/app identities; reads lazily retire elapsed resets. Authority
// errors propagate and must fail closed.
func (s *Store) ObserveProviderReset(ctx context.Context, identity string, reset time.Time) error {
	key, err := providerCooldownKey(identity)
	if err != nil {
		return err
	}
	if reset.IsZero() {
		return nil
	}
	return pkgvalkey.RunCooldownDeadlineObserve(ctx, s.client, []string{key}, []string{strconv.FormatInt(reset.UnixMilli(), 10), strconv.FormatInt(providerResetMaximum.Milliseconds(), 10)}).Error()
}

// ProviderRetryAt returns zero when no live provider cooldown exists. Read
// errors are not interpreted as absence: callers must stop the HTTP attempt.
func (s *Store) ProviderRetryAt(ctx context.Context, identity string) (time.Time, error) {
	key, err := providerCooldownKey(identity)
	if err != nil {
		return time.Time{}, err
	}
	n, err := pkgvalkey.RunCooldownDeadlineRead(ctx, s.client, []string{key}, nil).AsInt64()
	if err != nil {
		return time.Time{}, err
	}
	if n == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(n), nil
}
