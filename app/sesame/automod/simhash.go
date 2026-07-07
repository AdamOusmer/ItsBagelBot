package automod

// simHash fingerprints a message skeleton as a 64-bit SimHash over its
// whitespace tokens (bag of words: token order does not change the hash). Near
// duplicates - the same spam template with a swapped link, name or emoji -
// land within a small Hamming distance, so the campaign juror can group a
// reworded flood the exact-match ingress squash cannot fold. Zero is reserved
// as "no hash". FNV-1a is inlined (no hash.Hash interface) so the whole
// fingerprint is allocation-free.
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

// simBands splits a SimHash into two 32-bit bands. Two near-duplicate hashes
// (small Hamming distance) very likely agree on at least one band, so the
// campaign juror counts distinct senders per band and takes the larger count -
// cheap locality-sensitive grouping without a Hamming search.
func simBands(h uint64) (uint64, uint64) {
	return h >> 32, h & 0xffffffff
}
