// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"

	"ItsBagelBot/pkg/health"
)

// LaneCheck reports on one of a service's own JetStream lanes, as a check that
// its /status body and its health RPC reply both carry.
//
// It reads SubscriberHealthy, a local fetch-error clock rather than a
// ConsumerInfo call against the broker: the lane goes unhealthy once this
// consumer has been failing to fetch for longer than laneUnhealthyAfter. That
// is the signal worth reporting, because the failure it was written for is a
// consumer that is bound and erroring while the connection stays up — sesame
// ran seven hours silent that way on 2026-08-16 with every other check green.
//
// The verdict is hard, not degrading: a lane consumer that cannot fetch is not
// doing the thing the pod exists to do, so the pod should leave rotation rather
// than sit in it reporting an impairment.
//
// name is the lane, not the service. A service with several lanes registers one
// check each ("lane_premium", "lane_standard") so the report names the lane
// that failed instead of rolling them into one boolean.
func LaneCheck(name string, sub Subscriber) health.Check {
	return health.Check{
		Name: "lane_" + name,
		Probe: func(context.Context) error {
			if !SubscriberHealthy(sub) {
				return errors.New("consumer has been failing to fetch")
			}
			return nil
		},
	}
}
