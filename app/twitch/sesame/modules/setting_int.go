// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"
)

// settingInt is for dashboard number settings, which are saved as strings: a
// plain int64 field fails to decode "500" and silently keeps the default.
type settingInt int64

func (s *settingInt) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(strings.Trim(string(b), `"`))
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		n = 0
	}
	*s = settingInt(n)
	return nil
}
