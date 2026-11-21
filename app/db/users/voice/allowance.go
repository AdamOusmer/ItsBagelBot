// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice

import usersrpc "ItsBagelBot/internal/domain/rpc/users"

type Allowance struct {
	usersrpc.VoiceAllowance
	Reason string `json:"reason,omitempty"`
}
