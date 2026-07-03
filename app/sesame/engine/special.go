package engine

import "strings"

// SpecialSet is the set of special Twitch user ids (the bagel crew), parsed once
// from the TWITCH_SPECIAL_USER_IDS Doppler secret (the same secret ingress uses
// to lane them premium). Lookups are by the raw string id as it arrives on the
// chat envelope.
type SpecialSet struct {
	ids map[string]struct{}
}

// NewSpecialSet parses a comma-separated list of user ids into a set. Blank
// entries and surrounding whitespace are ignored.
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

// Has reports whether id is a special user.
func (s *SpecialSet) Has(id string) bool {
	if s == nil || id == "" {
		return false
	}
	_, ok := s.ids[id]
	return ok
}

// Len returns how many special ids are configured.
func (s *SpecialSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.ids)
}
