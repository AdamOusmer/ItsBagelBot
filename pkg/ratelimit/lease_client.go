// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

type LeaseClient struct {
	client valkey.Client
}

func NewLeaseClient(client valkey.Client) *LeaseClient {
	return &LeaseClient{client: client}
}

const (
	memberSetKey    = "outgress:members:v2"
	memberSeparator = "|"
)

func encodeMember(m Member) string { return m.PodID + memberSeparator + m.Region }

func decodeMember(entry string) (Member, bool) {
	podID, region, ok := strings.Cut(entry, memberSeparator)
	if !ok {
		return Member{}, false
	}
	if podID == "" || region == "" {
		return Member{}, false
	}
	return Member{PodID: podID, Region: region}, true
}

func (c *LeaseClient) Heartbeat(ctx context.Context, self Member, now time.Time, ttl time.Duration) error {
	score := float64(now.Add(ttl).UnixMilli())
	return c.client.Do(ctx, c.client.B().Zadd().Key(memberSetKey).ScoreMember().ScoreMember(score, encodeMember(self)).Build()).Error()
}

func (c *LeaseClient) ListMembers(ctx context.Context, now time.Time) ([]Member, error) {
	nowMS := strconv.FormatInt(now.UnixMilli(), 10)
	_ = c.client.Do(ctx, c.client.B().Zremrangebyscore().Key(memberSetKey).Min("-inf").Max("("+nowMS).Build()).Error()
	entries, err := c.client.Do(ctx, c.client.B().Zrangebyscore().Key(memberSetKey).Min(nowMS).Max("+inf").Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}
	members := make([]Member, 0, len(entries))
	for _, entry := range entries {
		if member, ok := decodeMember(entry); ok {
			members = append(members, member)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].PodID < members[j].PodID })
	return members, nil
}

func (c *LeaseClient) RemoveMember(ctx context.Context, self Member) error {
	return c.client.Do(ctx, c.client.B().Zrem().Key(memberSetKey).Member(encodeMember(self)).Build()).Error()
}

func (c *LeaseClient) ProposePlan(ctx context.Context, plan *Plan, replicas int, timeout time.Duration) (bool, error) {
	if err := plan.Validate(); err != nil {
		return false, err
	}
	key := fmt.Sprintf("outgress:plan:v2:%d", plan.Epoch)
	commitKey := key + ":committed"

	data, err := codec.Marshal(plan)
	if err != nil {
		return false, err
	}

	// WATCH, MULTI and WAIT must run on one dedicated connection.
	conn, cancel := c.client.Dedicate()
	defer cancel()

	if err := conn.Do(ctx, conn.B().Watch().Key(key).Build()).Error(); err != nil {
		return false, err
	}

	existingData, err := conn.Do(ctx, conn.B().Get().Key(key).Build()).AsBytes()
	if err == nil {
		_ = conn.Do(ctx, conn.B().Unwatch().Build()).Error()
		var existing Plan
		if err := codec.Unmarshal(existingData, &existing); err != nil {
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

	if err := conn.Do(ctx, conn.B().Multi().Build()).Error(); err != nil {
		return false, err
	}

	retention := planRetention(*plan)
	if err := conn.Do(ctx, conn.B().Set().Key(key).Value(string(data)).Px(retention).Build()).Error(); err != nil {
		return false, err
	}

	execRes := conn.Do(ctx, conn.B().Exec().Build())
	if err := execRes.Error(); err != nil {
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
	if err := codec.Unmarshal(data, &plan); err != nil {
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
