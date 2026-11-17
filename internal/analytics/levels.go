// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	catalog "ItsBagelBot/internal/domain/analytics"
)

type Tally map[string]int64

func (t Tally) Add(m catalog.Metric, dims ...string) { t.AddN(1, m, dims...) }

func (t Tally) AddN(n int64, m catalog.Metric, dims ...string) {
	if n > 0 && catalog.Valid(m, catalog.Level, dims, nil) {
		t[catalog.Field(m, dims...)] += n
	}
}
