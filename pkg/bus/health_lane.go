// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"

	"ItsBagelBot/pkg/health"
)

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
