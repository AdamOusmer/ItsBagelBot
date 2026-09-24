// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsmigrate

import "time"

const (
	DefaultChunkSize = 128 * 1024
	DefaultTimeout   = 10 * time.Second
)

func apiSubject(domain, suffix string) string {
	if domain == "" {
		return "$JS.API." + suffix
	}
	return "$JS." + domain + ".API." + suffix
}

func withDefaultDuration(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

func withDefaultInt(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}
