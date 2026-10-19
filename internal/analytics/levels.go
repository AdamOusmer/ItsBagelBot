// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	catalog "ItsBagelBot/internal/domain/analytics"
)

// Tally counts catalog fields for one side of a write: the rows a change touches,
// read before and after it.
type Tally map[string]int64

func (t Tally) Add(m catalog.Metric, dims ...string) { t.AddN(1, m, dims...) }

func (t Tally) AddN(n int64, m catalog.Metric, dims ...string) {
	if n > 0 && catalog.Valid(m, catalog.Level, dims, nil) {
		t[catalog.Field(m, dims...)] += n
	}
}

// Shift moves the running totals by after minus before.
func (r *Recorder) Shift(before, after Tally) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for field, n := range after {
		r.levels[field] += n - before[field]
	}
	for field, n := range before {
		if _, seen := after[field]; !seen {
			r.levels[field] -= n
		}
	}
}
