package ratelimit

import (
	"context"
	"strings"
	"time"

	"github.com/Yiling-J/theine-go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

type LeaseManager struct {
	central *Limiter
	local   *theine.Cache[string, *LocalBucket]
	permit  *PermitService
	mode    string
}

func NewLeaseManager(central *Limiter, local *theine.Cache[string, *LocalBucket], permit *PermitService, mode string) *LeaseManager {
	return &LeaseManager{
		central: central,
		local:   local,
		permit:  permit,
		mode:    mode,
	}
}

func (m *LeaseManager) Allow(ctx context.Context, req Request) (bool, error) {
	if m.mode == "central" {
		return m.central.Allow(ctx, req)
	}

	// Local check
	bucketID := extractBucketID(req.Key)
	now := time.Now()
	
	localAllowed := false
	if b, ok := m.local.Get(bucketID); ok {
		localAllowed = b.TryPremium(now)
	}

	if m.mode == "shadow" {
		centralAllowed, err := m.central.Allow(ctx, req)
		if err == nil && centralAllowed != localAllowed {
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.Application().RecordCustomEvent("outgress_shadow_disagreement", map[string]interface{}{
					"central": centralAllowed,
					"local":   localAllowed,
					"bucket":  bucketID,
				})
			}
		}
		return centralAllowed, err
	}

	// Leased mode: if local allowed, we're good
	if localAllowed {
		return true, nil
	}

	// Fallback to central emergency if borrowing isn't fully implemented or failed
	// Here we just use central with the original spec for simplicity as a placeholder
	// In the real system, it would use an emergency spec.
	if m.central != nil {
		return m.central.Allow(ctx, req)
	}
	return false, nil
}

func (m *LeaseManager) AllowOrdered(ctx context.Context, first, second Request) (uint8, error) {
	if m.mode == "central" {
		if m.central != nil {
			return m.central.AllowOrdered(ctx, first, second)
		}
		return 2, nil
	}

	bucketID := extractBucketID(second.Key) // the shared bucket represents the full identity
	now := time.Now()
	
	localFirst, localShared := false, false
	if b, ok := m.local.Get(bucketID); ok {
		localFirst, localShared = b.TryStandard(now)
	}

	if m.mode == "shadow" {
		centralDenied := uint8(2)
		var err error
		if m.central != nil {
			centralDenied, err = m.central.AllowOrdered(ctx, first, second)
		}
		
		shadowDenied := uint8(0)
		if !localFirst {
			shadowDenied = 1
		} else if !localShared {
			shadowDenied = 2
		}

		if err == nil && centralDenied != shadowDenied {
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.Application().RecordCustomEvent("outgress_shadow_disagreement", map[string]interface{}{
					"central": centralDenied,
					"local":   shadowDenied,
					"bucket":  bucketID,
				})
			}
		}
		return centralDenied, err
	}

	// Leased mode
	if localFirst && localShared {
		return 0, nil
	}

	// Fallback to central emergency if borrowing isn't fully implemented or failed
	if m.central != nil {
		return m.central.AllowOrdered(ctx, first, second)
	}

	if !localFirst {
		return 1, nil
	} else {
		return 2, nil
	}
}

// extractBucketID simplifies "ratelimit:chat:broadcaster_id" to "chat:broadcaster_id"
func extractBucketID(key string) string {
	return strings.TrimPrefix(key, "ratelimit:")
}
