// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
)

// Pure raffle mechanics: key building, open-request clamping, the random
// pick, receipt digests and announcement text shaping. No I/O lives here —
// everything takes and returns plain values so tests pin them directly.

func raffleKey(prefix string, id uint64) string { return prefix + strconv.FormatUint(id, 10) }

// clampRaffleOpen applies the store's floors and ceilings to one open request.// pickWinners draws min(n, len(members)) distinct members uniformly at random;// rngPick returns n distinct indices uniform over [0,total) via partial// DigestPool is the receipt's tamper-evidence: SHA-256 over the version tag// marshalJSON is json.Marshal ignoring the error for the one shape here (a// mentionList renders winner ids as chat mentions: "@a, @b". Winners are// expandTokens substitutes {token} placeholders with values; unknown tokens
