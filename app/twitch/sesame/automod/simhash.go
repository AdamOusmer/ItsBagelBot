// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

func simHash(skel []byte) uint64 {
	const (
		fnvOffset = 14695981039346656037
		fnvPrime  = 1099511628211
	)
	var votes [64]int16
	var h uint64 = fnvOffset
	inTok := false
	tokens := 0
	vote := func() {
		for bit := 0; bit < 64; bit++ {
			if h&(1<<uint(bit)) != 0 {
				votes[bit]++
			} else {
				votes[bit]--
			}
		}
		tokens++
		h = fnvOffset
	}
	for _, b := range skel {
		if b == ' ' {
			if inTok {
				vote()
				inTok = false
			}
			continue
		}
		inTok = true
		h ^= uint64(b)
		h *= fnvPrime
	}
	if inTok {
		vote()
	}
	if tokens == 0 {
		return 0
	}

	var out uint64
	for bit := 0; bit < 64; bit++ {
		if votes[bit] > 0 {
			out |= 1 << uint(bit)
		}
	}
	if out == 0 {
		out = 1
	}
	return out
}

func simBands(h uint64) (uint64, uint64) {
	return h >> 32, h & 0xffffffff
}
