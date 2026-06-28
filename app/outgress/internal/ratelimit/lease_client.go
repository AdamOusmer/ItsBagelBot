package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
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
	key := fmt.Sprintf("outgress:plan:%d", plan.Epoch)

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
	_, err = conn.Do(ctx, conn.B().Get().Key(key).Build()).AsBytes()
	if err == nil {
		// Already exists, we lost the race
		conn.Do(ctx, conn.B().Unwatch().Build())
		return false, nil
	} else if !valkey.IsValkeyNil(err) {
		return false, err
	}

	// MULTI
	if err := conn.Do(ctx, conn.B().Multi().Build()).Error(); err != nil {
		return false, err
	}

	// Write plan
	conn.Do(ctx, conn.B().Set().Key(key).Value(string(data)).Build())

	// EXEC
	execRes := conn.Do(ctx, conn.B().Exec().Build())
	if execRes.Error() != nil {
		// Transaction aborted or failed
		return false, nil // we didn't win
	}

	// WAIT
	waitRes, err := conn.Do(ctx, conn.B().Wait().Numreplicas(int64(replicas)).Timeout(int64(timeout.Milliseconds())).Build()).AsInt64()
	if err != nil {
		return false, err
	}

	if waitRes < int64(replicas) {
		return false, fmt.Errorf("replication barrier failed: %d/%d replicas acknowledged", waitRes, replicas)
	}

	return true, nil
}

// LoadPlan reads the plan for an epoch.
func (c *LeaseClient) LoadPlan(ctx context.Context, epoch uint64) (*Plan, error) {
	key := fmt.Sprintf("outgress:plan:%d", epoch)
	
	data, err := c.client.Do(ctx, c.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return nil, err
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}

	if !plan.IsValid() {
		return nil, fmt.Errorf("invalid plan digest")
	}

	return &plan, nil
}
