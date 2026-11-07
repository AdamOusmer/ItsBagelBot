// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const CreatorCodeMaxLen = 64

func normalizeCreatorCode(raw string) (*string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(code) > CreatorCodeMaxLen {
		return nil, fmt.Errorf("creator_code must be %d characters or fewer", CreatorCodeMaxLen)
	}
	for _, r := range code {
		if r < 0x20 || r == 0x7f {
			return nil, fmt.Errorf("creator_code cannot contain control characters")
		}
	}
	return &code, nil
}

func (v UserView) CreatorCodeText() string {
	if v.CreatorCode == nil {
		return ""
	}
	return *v.CreatorCode
}

func (r *Users) SetCreatorCode(ctx context.Context, id uint64, raw string) error {
	code, err := normalizeCreatorCode(raw)
	if err != nil {
		return err
	}
	r.queuePref(id, prefCreatorCode, prefWrite{code: code})
	return nil
}
