// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import "time"

const (
	tokensBudget   = 2 * time.Second
	emailBudget    = 3 * time.Second
	countsBudget   = 3 * time.Second
	adminBudget    = 3 * time.Second
	billingBudget  = 5 * time.Second
	giveawayBudget = 5 * time.Second
)
