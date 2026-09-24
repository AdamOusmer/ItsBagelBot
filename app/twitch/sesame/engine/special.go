// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "strings"

type SpecialSet struct {
	ids map[string]struct{}
}

func NewSpecialSet(csv string) *SpecialSet {
	s := &SpecialSet{ids: make(map[string]struct{})}
	for _, raw := range strings.Split(csv, ",") {
		id := strings.TrimSpace(raw)
		if id != "" {
			s.ids[id] = struct{}{}
		}
	}
	return s
}

func (s *SpecialSet) Has(id string) bool {
	if s == nil || id == "" {
		return false
	}
	_, ok := s.ids[id]
	return ok
}

func (s *SpecialSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.ids)
}
