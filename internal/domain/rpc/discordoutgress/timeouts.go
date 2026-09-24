// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import "time"

const (
	TicketOpenServerTimeout = 15 * time.Second
	TicketOpenClientTimeout = 20 * time.Second

	TicketCloseServerTimeout = 60 * time.Second
	TicketCloseClientTimeout = 70 * time.Second
)

type TicketTimeout struct {
	Subject string
	Server  time.Duration
	Client  time.Duration
}

var TicketTimeouts = []TicketTimeout{
	{Subject: "ticket.open", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.claim", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.add", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.panel", Server: TicketOpenServerTimeout, Client: TicketOpenClientTimeout},
	{Subject: "ticket.close", Server: TicketCloseServerTimeout, Client: TicketCloseClientTimeout},
}
