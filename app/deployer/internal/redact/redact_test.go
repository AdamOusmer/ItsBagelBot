// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package redact

import "testing"

func TestLine(t *testing.T) {
	cases := map[string]string{
		"dial mysql://svc:hunter2@10.0.0.4:3306/users: refused": "dial mysql://***@10.0.0.4:3306/users: refused",
		"nats: nats://tok@nats.messaging:4222 timeout":          "nats: nats://***@nats.messaging:4222 timeout",
		"Authorization: Bearer abc.def.ghi":                     "Authorization: ***",
		"sent bearer abc123 upstream":                           "sent bearer *** upstream",
		"DB_PASSWORD=hunter2 host=db":                           "DB_PASSWORD=*** host=db",
		`config {"clientSecret": "s3cr3t", "port": 1}`:          `config {"clientSecret": ***, "port": 1}`,
		"api_key: 'k-123' region=eu":                            "api_key: *** region=eu",
		"dsn=user:pw@tcp(db:3306)/x":                            "dsn=***",
		"token=abc":                                             "token=***",
		"listening on :8080, 3 workers":                         "listening on :8080, 3 workers",
		"panic: runtime error: index out of range":              "panic: runtime error: index out of range",
	}
	for in, want := range cases {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}
