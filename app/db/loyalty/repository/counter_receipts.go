// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/internal/domain/event/data"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"strconv"
	"time"
)

const counterDoneMarker = "done"

type counterDelivery struct {
	request     *counterRequest
	key         string
	aliases     []*counterRequest
	err         error
	claimed     bool
	cacheFailed bool
}

func counterReceiptKey(dto data.CounterBumpedDTO) string {
	if dto.BatchID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(dto.BatchID))
	return "loyalty:counter:receipt:" + strconv.FormatUint(dto.UserID, 10) + ":" + hex.EncodeToString(hash[:])
}

func (p *CounterProcessor) claimCounterDeliveries(ctx context.Context, deliveries []*counterDelivery, owner string) []*counterDelivery {
	claims, tracked := p.counterClaimCommands(deliveries, owner)
	if len(claims) == 0 {
		return tracked
	}
	claimCtx, cancel := context.WithTimeout(ctx, counterCacheTimeout)
	defer cancel()
	results := pkg_valkey.RunScripts(claimCtx, p.client, claims)
	for i, delivery := range tracked {
		p.classifyCounterClaim(delivery, results, i)
	}
	return tracked
}

func (p *CounterProcessor) counterClaimCommands(deliveries []*counterDelivery, owner string) ([]pkg_valkey.ScriptCall, []*counterDelivery) {
	commands := make([]pkg_valkey.ScriptCall, 0, len(deliveries))
	tracked := make([]*counterDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.key == "" {
			continue
		}
		commands = append(commands, counterLease(delivery).ClaimCall(owner, counterPendingTTL))
		tracked = append(tracked, delivery)
	}
	return commands, tracked
}

func counterLease(delivery *counterDelivery) pkg_valkey.MarkedLease {
	return pkg_valkey.MarkedLease{Key: delivery.key, Done: counterDoneMarker}
}

func counterClaimStatus(results []valkey_go.ValkeyResult, index int) (int64, error) {
	if index >= len(results) {
		return 0, errors.New("counter claim response missing")
	}
	status, err := results[index].ToInt64()
	if err != nil {
		return 0, fmt.Errorf("counter claim: %w", err)
	}
	return status, nil
}

func (p *CounterProcessor) classifyCounterClaim(delivery *counterDelivery, results []valkey_go.ValkeyResult, index int) {
	status, err := counterClaimStatus(results, index)
	if err != nil {
		delivery.cacheFailed = true
		p.warnCounterCache(err)
		return
	}
	switch status {
	case 1:
		delivery.claimed = true
	case 2: // Completed receipts exclude a delivery from SQL.
	case 0:
		delivery.err = ErrCounterPending
	default:
		delivery.err = fmt.Errorf("unexpected counter claim status %d", status)
	}
}

func (d *counterDelivery) persistable() bool {
	if d.err != nil {
		return false
	}
	if d.key == "" {
		return true
	}
	return d.hasOwnerClaim()
}

func (d *counterDelivery) hasOwnerClaim() bool { return d.claimed || d.cacheFailed }

func activeCounterDeliveries(deliveries []*counterDelivery) []*counterDelivery {
	active := make([]*counterDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.persistable() {
			active = append(active, delivery)
		}
	}
	return active
}

func (p *CounterProcessor) finalizeCounterClaims(tracked []*counterDelivery, owner string) {
	commands, finalized := p.counterFinalizationCommands(tracked, owner)
	if len(commands) == 0 {
		return
	}
	// Cleanup retains its short bound when SQL exhausted its deadline. Owner
	// checks prevent deleting a replacement claim after a lease expires.
	ctx, cancel := context.WithTimeout(context.Background(), counterCacheTimeout)
	defer cancel()
	results := pkg_valkey.RunScripts(ctx, p.client, commands)
	for i, delivery := range finalized {
		if delivery.err != nil {
			continue
		} // Preserve genuine SQL/claim failures.
		if err := counterCompletionError(results, i); err != nil {
			p.warnCounterCache(err)
		}
	}
}

func (p *CounterProcessor) counterFinalizationCommands(tracked []*counterDelivery, owner string) ([]pkg_valkey.ScriptCall, []*counterDelivery) {
	commands := make([]pkg_valkey.ScriptCall, 0, len(tracked))
	finalized := make([]*counterDelivery, 0, len(tracked))
	for _, delivery := range tracked {
		command, needed := p.counterFinalizationCommand(delivery, owner)
		if !needed {
			continue
		}
		commands = append(commands, command)
		finalized = append(finalized, delivery)
	}
	return commands, finalized
}

func (p *CounterProcessor) counterFinalizationCommand(delivery *counterDelivery, owner string) (pkg_valkey.ScriptCall, bool) {
	if delivery.shouldComplete() {
		return counterLease(delivery).FinishCall(owner, counterCompletedTTL), true
	}
	if delivery.shouldRelease() {
		// Also release unknown claims: a failed response may follow a valid SET.
		return pkg_valkey.NewOwnerLock(p.client, delivery.key, owner).ReleaseCall(), true
	}
	return pkg_valkey.ScriptCall{}, false
}

func (d *counterDelivery) shouldComplete() bool {
	if d.err != nil {
		return false
	}
	return d.hasOwnerClaim()
}

func (d *counterDelivery) shouldRelease() bool {
	if d.err == nil {
		return false
	}
	return !errors.Is(d.err, ErrCounterPending)
}

func counterCompletionError(results []valkey_go.ValkeyResult, index int) error {
	if index >= len(results) {
		return errors.New("counter completion response missing")
	}
	status, err := results[index].ToInt64()
	if err != nil {
		return fmt.Errorf("counter completion: %w", err)
	}
	if status != 1 {
		return errors.New("counter completion lease lost")
	}
	return nil
}

// Dedup is best effort for these loss-tolerant counters. A Valkey outage must
// not discard a counter window whose SQL write can succeed; warnings are
// throttled so an outage cannot flood logs. Explicit live-owner collisions
// remain retryable because another delivery has not yet proved persistence.
func (p *CounterProcessor) warnCounterCache(err error) {
	now := time.Now().Unix()
	previous := p.cacheWarning.Load()
	if now-previous < 60 || !p.cacheWarning.CompareAndSwap(previous, now) {
		return
	}
	if p.repo.log != nil {
		p.repo.log.Warn("loyalty: counter dedup unavailable; recount is possible", zap.Error(err))
	}
}
