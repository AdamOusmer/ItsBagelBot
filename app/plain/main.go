// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"time"

	"ItsBagelBot/app/plain/internal/config"
	"ItsBagelBot/pkg/svcboot"
)

const serviceName = "outgress"

const (
	nakDelay        = 1 * time.Second
	maxRedeliveries = 2
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()

	// Loaded but unused: this binary is still a stub, and the load is kept so
	// the config package stays wired for whatever finishes it. Valkey's own
	// endpoints now come from svcboot.Infra, which reads the same two vars.
	_ = config.Load()

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()
}
