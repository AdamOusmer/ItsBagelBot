// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/app/plain/internal/config"
	"ItsBagelBot/pkg/svcboot"
)

const serviceName = "plain"

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()

	_ = config.Load()

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()
}
