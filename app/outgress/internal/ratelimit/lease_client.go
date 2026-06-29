package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

type LeaseClient struct {
	client valkey.Client
}

func NewLeaseClient(client valkey.Client) *LeaseClient {
	return &LeaseClient{client: client}
}

// ProposePlan attempts to publish a new plan for the epoch.
func (c *LeaseClient) ProposePlan(ctx context.Context, plan *Plan, replicas int, timeout time.Duration) (bool, error) {
	if err := plan.Validate(); err != nil {
		return false, err
	}
	key := fmt.Sprintf("outgress:plan:v2:%d", plan.Epoch)
	commitKey := key + ":committed"

	data, err := json.Marshal(plan)
	if err != nil {
		return false, err
	}

	// We need a dedicated connection to run WATCH, MULTI, and WAIT in sequence.
	conn, cancel := c.client.Dedicate()
	defer cancel()

	// WATCH the plan key
	if err := conn.Do(ctx, conn.B().Watch().Key(key).Build()).Error(); err != nil {
		return false, err
	}

	// Ensure it's absent. Note: Get returns Nil error if missing, we just want to ensure it doesn't already have data.
	existingData, err := conn.Do(ctx, conn.B().Get().Key(key).Build()).AsBytes()
	if err == nil {
		// Recover a winner that may have crashed between the plan replication
		// barrier and publishing its commit marker.
		_ = conn.Do(ctx, conn.B().Unwatch().Build()).Error()
		var existing Plan
		if err := json.Unmarshal(existingData, &existing); err != nil {
			return false, err
		}
		if err := existing.Validate(); err != nil {
			return false, err
		}
		retention := planRetention(existing)
		if err := conn.Do(ctx, conn.B().Pexpire().Key(key).Milliseconds(retention.Milliseconds()).Build()).Error(); err != nil {
			return false, err
		}
		if err := waitReplicas(ctx, conn, replicas, timeout); err != nil {
			return false, err
		}
		if err := conn.Do(ctx, conn.B().Set().Key(commitKey).Value(existing.Digest).Px(retention).Build()).Error(); err != nil {
			return false, err
		}
		return false, waitReplicas(ctx, conn, replicas, timeout)
	} else if !valkey.IsValkeyNil(err) {
		return false, err
	}

	// MULTI
	if err := conn.Do(ctx, conn.B().Multi().Build()).Error(); err != nil {
		return false, err
	}

	retention := planRetention(*plan)
	// Queue the immutable plan with bounded retention. Command queueing errors
	// are surfaced by EXEC, which is read below.
	if err := conn.Do(ctx, conn.B().Set().Key(key).Value(string(data)).Px(retention).Build()).Error(); err != nil {
		return false, err
	}

	// EXEC
	execRes := conn.Do(ctx, conn.B().Exec().Build())
	if err := execRes.Error(); err != nil {
		// Transaction aborted or failed
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}

	if err := waitReplicas(ctx, conn, replicas, timeout); err != nil {
		return false, err
	}
	if err := conn.Do(ctx, conn.B().Set().Key(commitKey).Value(plan.Digest).Px(retention).Build()).Error(); err != nil {
		return false, err
	}
	if err := waitReplicas(ctx, conn, replicas, timeout); err != nil {
		return false, err
	}

	return true, nil
}

// LoadPlan reads the plan for an epoch.
func (c *LeaseClient) LoadPlan(ctx context.Context, epoch uint64) (*Plan, error) {
	key := fmt.Sprintf("outgress:plan:v2:%d", epoch)
	results := c.client.DoMulti(ctx,
		c.client.B().Get().Key(key).Build(),
		c.client.B().Get().Key(key+":committed").Build(),
	)
	data, err := results[0].AsBytes()
	if err != nil {
		return nil, err
	}
	committed, err := results[1].ToString()
	if err != nil {
		return nil, err
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}

	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}
	if committed != plan.Digest {
		return nil, errors.New("ratelimit: lease plan is not replication-committed")
	}

	return &plan, nil
}

func planRetention(plan Plan) time.Duration {
	retention := 3 * time.Duration(plan.ValidUntilMS-plan.ValidFromMS) * time.Millisecond
	if retention < time.Minute {
		return time.Minute
	}
	return retention
}

func waitReplicas(ctx context.Context, conn valkey.DedicatedClient, replicas int, timeout time.Duration) error {
	if replicas <= 0 {
		return nil
	}
	acknowledged, err := conn.Do(ctx, conn.B().Wait().Numreplicas(int64(replicas)).Timeout(timeout.Milliseconds()).Build()).AsInt64()
	if err != nil {
		return err
	}
	if acknowledged < int64(replicas) {
		return fmt.Errorf("replication barrier failed: %d/%d replicas acknowledged", acknowledged, replicas)
	}
	return nil
}

func (c *LeaseClient) ServerTime(ctx context.Context) (time.Time, error) {
	parts, err := c.client.Do(ctx, c.client.B().Time().Build()).AsStrSlice()
	if err != nil {
		return time.Time{}, err
	}
	if len(parts) != 2 {
		return time.Time{}, errors.New("ratelimit: invalid Valkey TIME response")
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	micros, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, micros*int64(time.Microsecond)), nil
}
