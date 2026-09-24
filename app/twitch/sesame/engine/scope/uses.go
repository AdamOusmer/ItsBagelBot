// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
)

const usesName = "uses"

type Uses struct {
	Count int64
}

func (Uses) Owns(v Var) bool {
	return v.Name == usesName || (v.Name == countName && !v.HasPayload)
}

func (u Uses) Plan(context.Context, []Var) (Values, error) { return u, nil }

func (u Uses) Get(v Var) (string, bool) {
	owned := v.Name == usesName || v.Name == countName
	if !owned || v.HasPayload {
		return "", false
	}
	return strconv.FormatInt(u.Count, 10), true
}
