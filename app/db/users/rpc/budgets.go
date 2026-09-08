// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import "time"

// The handler budgets the users service's verb groups run on. They were
// positional arguments to a seven-argument QueueSubscribeJSON call in six
// different files, which is exactly the kind of value a refactor flattens onto
// one default without anyone noticing until a verb starts timing out.
// budgets_test.go pins them.
const (
	// tokensBudget covers one indexed row read plus an unseal, or one upsert.
	tokensBudget = 2 * time.Second
	// emailBudget and countsBudget: one indexed lookup, one aggregate query.
	emailBudget  = 3 * time.Second
	countsBudget = 3 * time.Second
	// adminBudget covers the console's admin verbs and the staff auth/audit
	// surface: a lookup or one short transaction, plus an audit insert.
	adminBudget = 3 * time.Second
	// billingBudget is the widest here: applying a Tebex grant is a
	// transaction plus the cache invalidation publish that follows it.
	billingBudget = 5 * time.Second
)
