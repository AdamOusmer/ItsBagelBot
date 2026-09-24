// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package utils

func BoolField(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
