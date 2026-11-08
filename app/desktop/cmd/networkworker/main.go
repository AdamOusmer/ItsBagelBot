// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/app/desktop/internal/network"
	"os"
)

func main() {
	if network.RunWorker() != nil {
		os.Exit(1)
	}
}
