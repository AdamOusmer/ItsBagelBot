// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"testing"
)

func TestTicketsDurableIsFalseOnTheValkeyFallback(t *testing.T) {
	if (valkeyStore{}).TicketsDurable(context.Background()) {
		t.Fatal("the pure-Valkey fallback must not claim durable tickets")
	}
	if !NewMem().TicketsDurable(context.Background()) {
		t.Fatal("the memory double stands in for the durable path")
	}
}
