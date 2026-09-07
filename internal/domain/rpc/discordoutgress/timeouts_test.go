// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import "testing"

// The invariant the table exists for: the caller always outlives the handler.
// When it does not, the engine abandons a request outgress is still serving --
// the channel gets created, the card gets posted, and no ticket row is ever
// written for either.
func TestTicketClientDeadlineOutlivesTheServerDeadline(t *testing.T) {
	if len(TicketTimeouts) == 0 {
		t.Fatal("the timeout table is empty")
	}
	for _, pair := range TicketTimeouts {
		if pair.Server <= 0 || pair.Client <= 0 {
			t.Fatalf("%s: zero deadline (server %s, client %s)", pair.Subject, pair.Server, pair.Client)
		}
		if pair.Client <= pair.Server {
			t.Fatalf("%s: client %s must outlive server %s", pair.Subject, pair.Client, pair.Server)
		}
	}
}

// Every ticket RPC subject appears exactly once, so a new one cannot be added
// to the wire without being added to the table that pairs its deadlines.
func TestTicketTimeoutsCoverEverySubjectOnce(t *testing.T) {
	want := map[string]bool{
		"ticket.open": false, "ticket.claim": false, "ticket.add": false,
		"ticket.panel": false, "ticket.close": false,
	}
	markTimeoutSubjects(t, want)
	wantEverySubjectPaired(t, want)
}

// markTimeoutSubjects walks the table and ticks off each subject, failing on
// one the list above does not know or that the table pairs twice.
func markTimeoutSubjects(t *testing.T, want map[string]bool) {
	t.Helper()
	for _, pair := range TicketTimeouts {
		seen, known := want[pair.Subject]
		if !known {
			t.Fatalf("%s is in the table but not in this test's list", pair.Subject)
		}
		if seen {
			t.Fatalf("%s appears twice", pair.Subject)
		}
		want[pair.Subject] = true
	}
}

// wantEverySubjectPaired is the other direction: a subject on the wire with no
// row in the table would ship with no deadline pairing at all.
func wantEverySubjectPaired(t *testing.T, want map[string]bool) {
	t.Helper()
	for subject, seen := range want {
		if !seen {
			t.Fatalf("%s has no deadline pairing", subject)
		}
	}
}
