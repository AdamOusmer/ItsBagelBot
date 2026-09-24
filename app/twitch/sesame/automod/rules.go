// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import "ItsBagelBot/internal/moderation"

type category struct {
	name    string
	action  Action
	seconds uint32
}

func defaultCategories() []category {
	cats := make([]category, moderation.FloorScam+1)
	cats[moderation.FloorIPLogger] = category{name: "ip_logger", action: ActionTimeout, seconds: 600}
	cats[moderation.FloorScam] = category{name: "scam", action: ActionTimeout, seconds: 600}
	return cats
}
