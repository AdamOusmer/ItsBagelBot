// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "math/rand/v2"

func pickIndex(pick func(n int) int, n int) int {
	if pick != nil {
		return pick(n)
	}
	return rand.IntN(n)
}

type drawCursor struct {
	items []string
	used  int
}

func (d *drawCursor) next() (string, bool) {
	if len(d.items) == 0 {
		return "", false
	}
	item := d.items[min(d.used, len(d.items)-1)]
	d.used++
	return item, true
}
