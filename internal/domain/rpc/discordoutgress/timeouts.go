// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import "time"

// The ticket desk's request deadlines, as ONE table both sides read.
//
// The pairing is the point. Every one of these RPCs has two deadlines: the one
// outgress's handler runs under (bus.QueueSubscribeJSON's timeout) and the one
// engine's client waits for a reply under. The client's MUST be the longer of
// the two, and the two used to be written in two files with no reference to
// each other -- app/discord/outgress/internal/rpc/ticket_rpc.go said 15s for
// the open handler while app/discord/engine/internal/rpcclient said 8s for the
// call, so the engine gave up seven seconds BEFORE outgress stopped working.
// The visible symptom is the worst one this feature has: outgress creates the
// channel and posts the card, the engine has already abandoned the request, no
// ticket row is written, and the guild gets a private channel nothing can
// close.
//
// The margin is 5 seconds, not a percentage. It covers the NATS round trip
// plus the handler's own bookkeeping either side of the REST work, both of
// which are milliseconds in practice; the number is round rather than tuned
// because there is nothing here to tune against -- it only has to be more than
// zero and less than a human's patience.
//
// TicketTimeouts below is the same data as a slice so a test can assert the
// invariant over every row instead of over the rows someone remembered.
const (
	// TicketOpenServerTimeout bounds a create-channel plus a post: two REST
	// calls. Claim (an edit plus a note) and add (one overwrite PUT) are the
	// same shape and share it.
	TicketOpenServerTimeout = 15 * time.Second
	TicketOpenClientTimeout = 20 * time.Second

	// TicketCloseServerTimeout bounds the whole close sequence. A transcript
	// pages the channel up to TranscriptMessageCap/MessagePageMax = 20 times,
	// then moves or deletes the channel, then uploads a file and posts a
	// summary: 23 REST calls worst case. At Discord's shared ~50 req/s budget
	// that is well under a second of call time, but each call can sit behind a
	// Retry-After from the same bucket every other guild is drawing on, so the
	// ceiling is set by waiting, not by working.
	TicketCloseServerTimeout = 60 * time.Second
	TicketCloseClientTimeout = 70 * time.Second
)

// TicketTimeout is one RPC's pair of deadlines.
type TicketTimeout struct {
	// Subject is the leaf under the outgress RPC prefix.
	Subject string
	// Server is the handler deadline outgress registers the subject with.
	Server time.Duration
	// Client is the deadline engine's caller waits under. Always > Server.
	Client time.Duration
}

// TicketTimeouts is every ticket RPC's pairing, in one place.
var TicketTimeouts = []TicketTimeout{
	{Subject: "ticket.open", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.claim", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.add", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.panel", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.close", Server: TicketCloseServerTimeout, Client: TicketCloseClientTimeout},
}
