// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
)

func CountHelixFailures(rec *analytics.Recorder) func(status int, err error) {
	return func(status int, err error) {
		if class := failureClass(status, err); class != "" {
			rec.Count(catalog.OutgressFailures, class)
		}
	}
}

func failureClass(status int, err error) string {
	switch {
	case err != nil:
		return "network"
	case status == http.StatusTooManyRequests:
		return "ratelimited"
	case status == http.StatusUnauthorized:
		return "unauthorized"
	case status == http.StatusForbidden:
		return "banned"
	case status >= 500:
		return "server"
	case status >= 400:
		return "client"
	}
	return ""
}
