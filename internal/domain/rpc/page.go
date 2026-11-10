// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

type Page struct {
	Number   int
	Size     int
	MaxPages int
}

func ClampPage(number, size, maxSize, maxPages int) Page {
	return Page{
		Number:   min(max(number, 1), maxPages),
		Size:     min(max(size, 1), maxSize),
		MaxPages: maxPages,
	}
}

func (p Page) Offset() int { return (p.Number - 1) * p.Size }

func (p Page) FetchLimit() int {
	if p.Number < p.MaxPages {
		return p.Size + 1
	}
	return p.Size
}

func PageRows[T any](p Page, rows []T) ([]T, bool) {
	if p.Number < p.MaxPages && len(rows) > p.Size {
		return rows[:p.Size], true
	}
	return rows, false
}
