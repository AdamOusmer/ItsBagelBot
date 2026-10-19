// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/event/lane"
)

// Zero means the message carried no @mention for the login.
func mentionedViewerID(mentions []lane.Mention, login string) uint64 {
	for _, m := range mentions {
		if strings.EqualFold(m.UserLogin, login) {
			id, _ := strconv.ParseUint(m.UserID, 10, 64)
			return id
		}
	}
	return 0
}
